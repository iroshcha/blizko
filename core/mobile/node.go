// Package mobile is the same embedded iroh/chat engine on Android and iOS.
// Its gomobile API only exposes strings, byte arrays and errors.
package mobile

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/nacl/box"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type message struct {
	ID        string    `json:"id"`
	Peer      string    `json:"peer"`
	Text      string    `json:"text"`
	Out       bool      `json:"out"`
	Delivered bool      `json:"delivered"`
	Time      int64     `json:"time"`
	Packet    *envelope `json:"packet,omitempty"`
}
type diskState struct {
	Version   int       `json:"v"`
	Public    [32]byte  `json:"public"`
	Secret    [32]byte  `json:"secret"`
	Contacts  []contact `json:"contacts"`
	Messages  []message `json:"messages"`
	Address   string    `json:"address"`
	IrohSeed  [32]byte  `json:"irohSeed"`
	RelayOnly bool      `json:"relayOnly"`
}
type Node struct {
	mu             sync.Mutex
	life           sync.Mutex
	state          diskState
	storage        *vault
	dir            string
	link           *irohLink
	cancel         context.CancelFunc
	wake           chan struct{}
	status         string
	enabled        bool
	online         bool
	generation     int
	deliveryIssues map[string]string // Local runtime diagnostics, never message contents or server storage.
	relay          string
}

func NewNode(dir string, storageKey []byte) (*Node, error) {
	v, e := newVault(filepath.Join(dir, "vault"), storageKey)
	if e != nil {
		return nil, e
	}
	n := &Node{storage: v, dir: dir, wake: make(chan struct{}, 1), status: "Приём выключен"}
	b, e := v.read("chat-v2")
	if os.IsNotExist(e) {
		pub, priv, e := box.GenerateKey(rand.Reader)
		if e != nil {
			return nil, e
		}
		n.state = diskState{Version: 3, Public: *pub, Secret: *priv, Contacts: []contact{}, Messages: []message{}}
		if e = n.commit(n.state); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, errors.New("Не удалось расшифровать хранилище. Данные не удалены.")
	} else if e = json.Unmarshal(b, &n.state); e != nil || (n.state.Version != 2 && n.state.Version != 3) {
		return nil, errors.New("Неизвестный формат хранилища")
	}

	if n.state.IrohSeed == ([32]byte{}) {
		state := n.clone()
		if _, e = rand.Read(state.IrohSeed[:]); e != nil {
			return nil, e
		}
		if state.Version == 2 {
			for i := range state.Contacts {
				state.Contacts[i].Address = ""
			}
		}
		state.Version = 3
		state.Address = hex.EncodeToString(ed25519.NewKeyFromSeed(state.IrohSeed[:]).Public().(ed25519.PublicKey))
		if e = n.commit(state); e != nil {
			return nil, e
		}
	}
	return n, nil
}
func (n *Node) clone() diskState {
	b, _ := json.Marshal(n.state)
	var s diskState
	_ = json.Unmarshal(b, &s)
	return s
}
func (n *Node) commit(s diskState) error {
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	if e = n.storage.write("chat-v2", b); e != nil {
		return e
	}
	n.state = s
	return nil
}
func (n *Node) self() contact {
	return contact{ID: keyID(n.state.Public), Key: n.state.Public, Address: n.state.Address}
}
func (n *Node) peer(id string) (contact, bool) {
	for _, c := range n.state.Contacts {
		if c.ID == id {
			return c, true
		}
	}
	return contact{}, false
}
func (n *Node) Snapshot() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	msgs := make([]message, len(n.state.Messages))
	copy(msgs, n.state.Messages)
	incoming := 0
	for i := range msgs {
		msgs[i].Packet = nil
		if !msgs[i].Out {
			incoming++
		}
	}
	s := map[string]any{"status": n.status, "enabled": n.enabled, "online": n.online, "address": n.state.Address, "id": keyID(n.state.Public), "contacts": n.state.Contacts, "messages": msgs, "incoming": incoming}
	s["deliveryIssues"] = n.deliveryIssues
	s["relay"] = n.relay
	s["relayOnly"] = n.state.RelayOnly
	s["transport"] = "iroh"
	b, _ := json.Marshal(s)
	return string(b)
}
func (n *Node) MyCode() (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !validAddress(n.state.Address) {
		return "", errors.New("Не удалось подготовить идентификатор iroh")
	}
	return encodeContact(n.self()), nil
}
func (n *Node) AddContact(name, code string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 {
		return errors.New("Введите имя до 60 символов")
	}
	c, e := decodeContact(code)
	if e != nil {
		return e
	}
	c.Name = name
	n.mu.Lock()
	defer n.mu.Unlock()
	if c.ID == keyID(n.state.Public) {
		return errors.New("Это ваш контакт")
	}
	s := n.clone()
	for i := range s.Contacts {
		if s.Contacts[i].ID == c.ID {
			s.Contacts[i] = c
			return n.commit(s)
		}
	}
	if len(s.Contacts) >= 100 {
		return errors.New("Лимит прототипа: 100 контактов")
	}
	s.Contacts = append(s.Contacts, c)
	return n.commit(s)
}
func (n *Node) Send(peerID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxText {
		return errors.New("Сообщение должно содержать от 1 до 4000 байт текста")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	c, ok := n.peer(peerID)
	if !ok {
		return errors.New("Контакт не найден")
	}
	pending := 0
	for _, m := range n.state.Messages {
		if m.Out && !m.Delivered {
			pending++
		}
	}
	if pending >= 100 || len(n.state.Messages) >= 2000 {
		return errors.New("Лимит прототипа: 100 ожидающих или 2000 сообщений всего")
	}
	id := randomID()
	p := payload{Version: 2, ID: id, From: keyID(n.state.Public), To: peerID, Kind: "text", Text: text}
	env, e := seal(p, c, n.state.Secret)
	if e != nil {
		return e
	}
	s := n.clone()
	s.Messages = append(s.Messages, message{ID: id, Peer: peerID, Text: text, Out: true, Time: time.Now().UnixMilli(), Packet: &env})
	if e = n.commit(s); e != nil {
		return e
	}
	select {
	case n.wake <- struct{}{}:
	default:
	}
	return nil
}
func (n *Node) accept(env envelope) (envelope, bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	c, ok := n.peer(env.From)
	if !ok {
		return envelope{}, false, errUnknownContact
	}
	p, e := openEnvelope(env, c, n.state.Secret, keyID(n.state.Public))
	if e != nil || p.Kind != "text" {
		return envelope{}, false, errInvalidMessage
	}
	duplicate := false
	for _, m := range n.state.Messages {
		if !m.Out && m.Peer == p.From && m.ID == p.ID {
			duplicate = true
			break
		}
	}
	if !duplicate {
		if len(n.state.Messages) >= 2000 {
			return envelope{}, false, errHistoryFull
		}
		s := n.clone()
		s.Messages = append(s.Messages, message{ID: p.ID, Peer: p.From, Text: p.Text, Delivered: true, Time: time.Now().UnixMilli()})
		if e = n.commit(s); e != nil {
			return envelope{}, false, e
		}
	}
	// Receipt is produced only after the authenticated message is durably stored.
	ack, e := seal(payload{Version: 2, ID: randomID(), From: keyID(n.state.Public), To: p.From, Kind: "ack", Ack: p.ID}, c, n.state.Secret)
	return ack, !duplicate, e
}
func (n *Node) applyAck(env envelope, peerID, messageID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	c, ok := n.peer(peerID)
	if !ok {
		return errors.New("unknown contact")
	}
	p, e := openEnvelope(env, c, n.state.Secret, keyID(n.state.Public))
	if e != nil {
		return e
	}
	if p.Kind != "ack" || p.Ack != messageID {
		return errors.New("invalid receipt")
	}
	for i, m := range n.state.Messages {
		if m.Out && m.ID == messageID && m.Peer == peerID && !m.Delivered {
			s := n.clone()
			s.Messages[i].Delivered = true
			s.Messages[i].Packet = nil
			return n.commit(s)
		}
	}
	return nil
}
func (n *Node) messageHandler() http.Handler {
	permits := make(chan struct{}, 8)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "POST" || r.URL.Path != "/message" {
			http.NotFound(w, r)
			return
		}
		select {
		case permits <- struct{}{}:
			defer func() { <-permits }()
		default:
			http.Error(w, "busy", 503)
			return
		}
		var env envelope
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 20000))
		decoder.DisallowUnknownFields()
		if e := decoder.Decode(&env); e != nil {
			http.Error(w, "invalid", 400)
			return
		}
		ack, _, e := n.accept(env)
		if e != nil {
			code, status := "storage_failed", http.StatusServiceUnavailable
			switch {
			case errors.Is(e, errUnknownContact):
				code, status = "unknown_contact", http.StatusForbidden
			case errors.Is(e, errInvalidMessage):
				code, status = "invalid_message", http.StatusForbidden
			case errors.Is(e, errHistoryFull):
				code, status = "history_full", http.StatusInsufficientStorage
			}
			w.Header().Set("X-Blizko-Error", code)
			http.Error(w, "not accepted", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ack)
	})
}
func (n *Node) flush(ctx context.Context, client *http.Client) {
	n.flushRoutes(ctx, client)
}
func (n *Node) flushRoutes(ctx context.Context, client *http.Client) {
	n.mu.Lock()
	var pending []message
	peers := map[string]contact{}
	for _, m := range n.state.Messages {
		if m.Out && !m.Delivered && m.Packet != nil {
			pending = append(pending, m)
			if c, ok := n.peer(m.Peer); ok {
				peers[m.Peer] = c
			}
		}
	}
	n.mu.Unlock()
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, m := range pending {
		if ctx.Err() != nil {
			break
		}
		c, ok := peers[m.Peer]
		if !ok {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(m message, c contact) {
			defer wg.Done()
			defer func() { <-sem }()
			issue := "Телефон собеседника недоступен. Откройте «Близко» и включите приём на обоих телефонах. Сообщение остаётся в очереди."
			defer func() {
				if ctx.Err() == nil {
					n.recordDeliveryIssue(c.ID, issue)
				}
			}()

			if !validAddress(c.Address) {
				issue = "Обменяйтесь новыми QR после обновления обоих телефонов. История сохранена."
				return
			}
			b, _ := json.Marshal(m.Packet)
			req, e := http.NewRequestWithContext(ctx, "POST", "http://"+net.JoinHostPort(c.Address, "47831")+"/message", bytes.NewReader(b))
			if e != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			res, e := client.Do(req)
			if e != nil {
				return
			}
			defer res.Body.Close()
			if res.StatusCode != 200 {
				issue = deliveryResponseIssue(res)
				return
			}
			var ack envelope
			issue = "Не удалось проверить подтверждение доставки. Сообщение осталось в очереди. Проверьте QR-контакты и версии приложения на обоих телефонах."
			if e = json.NewDecoder(io.LimitReader(res.Body, 20000)).Decode(&ack); e == nil && n.applyAck(ack, m.Peer, m.ID) == nil {
				issue = ""
			}
		}(m, c)
	}
	wg.Wait()
}
