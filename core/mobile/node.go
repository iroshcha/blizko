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
	Order     int64     `json:"order"`
	Archived  bool      `json:"archived,omitempty"`
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
	revision       uint64
	nextOrder      int64
	incoming       int
	messageIndexes map[string]int
	retry          map[string]retryState
	flushMu        sync.Mutex
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
	} else if e = json.Unmarshal(b, &n.state); e != nil || (n.state.Version != 2 && n.state.Version != 3 && n.state.Version != 4) {
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
	if n.state.Version == 4 {
		messages, err := v.readMessages()
		if err != nil {
			return nil, errors.New("Не удалось расшифровать записи истории. Данные не удалены.")
		}
		n.state.Messages = messages
	} else {
		state := n.clone()
		state.Version = 4
		if e = n.commit(state); e != nil {
			return nil, e
		}
	}
	n.rebuildIndexes()
	return n, nil
}

// Message packets are immutable after creation; cloning slices is sufficient.
func (n *Node) clone() diskState {
	state := n.state
	state.Contacts = append([]contact{}, n.state.Contacts...)
	state.Messages = append([]message{}, n.state.Messages...)
	return state
}
func (n *Node) commit(state diskState) error {
	metadata := state
	if state.Version == 4 {
		order := n.nextOrder
		for i, m := range state.Messages {
			if m.Order == 0 {
				order++
				m.Order = order
				state.Messages[i] = m
			}
			index, exists := n.messageIndexes[recordID(m)]
			if n.state.Version != 4 || !exists || n.state.Messages[index] != m {
				if err := n.storage.writeMessage(m); err != nil {
					return err
				}
			}
		}
		metadata = state
		metadata.Messages = []message{}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	if err = n.storage.write("chat-v2", raw); err != nil {
		return err
	}
	n.state = state
	n.rebuildIndexes()
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
func (n *Node) statusLocked() map[string]any {
	return map[string]any{"status": n.status, "enabled": n.enabled, "online": n.online, "address": n.state.Address, "id": keyID(n.state.Public), "incoming": n.incoming, "revision": n.revision, "relay": n.relay, "relayOnly": n.state.RelayOnly, "transport": "iroh"}
}

// Status is bounded and does not serialize history or wait for network I/O.
func (n *Node) Status() string {
	n.mu.Lock()
	status := n.statusLocked()
	n.mu.Unlock()
	raw, _ := json.Marshal(status)
	return string(raw)
}
func (n *Node) Snapshot() string { return n.snapshot("", 0, 0) }

// before is an exclusive local order cursor. A zero cursor selects recent rows.
func (n *Node) SnapshotPage(peer string, before int64, limit int) string {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return n.snapshot(peer, before, limit)
}
func (n *Node) snapshot(peer string, before int64, limit int) string {
	n.mu.Lock()
	status := n.statusLocked()
	messages := []message{}
	hasMore := false
	for i := len(n.state.Messages) - 1; i >= 0; i-- {
		m := n.state.Messages[i]
		if m.Archived || (peer != "" && m.Peer != peer) || (before > 0 && m.Order >= before) {
			continue
		}
		if limit > 0 && len(messages) >= limit {
			hasMore = true
			break
		}
		m.Packet = nil
		messages = append(messages, m)
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	status["contacts"] = append([]contact{}, n.state.Contacts...)
	issues := map[string]string{}
	for id, value := range n.deliveryIssues {
		issues[id] = value
	}
	previews := map[string]string{}
	for i := len(n.state.Messages) - 1; i >= 0; i-- {
		m := n.state.Messages[i]
		if !m.Archived {
			if _, ok := previews[m.Peer]; !ok {
				previews[m.Peer] = m.Text
			}
		}
		if len(previews) == len(n.state.Contacts) {
			break
		}
	}
	status["messages"] = messages
	status["hasMore"] = hasMore
	status["deliveryIssues"] = issues
	status["previews"] = previews
	n.mu.Unlock()
	raw, _ := json.Marshal(status)
	return string(raw)
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
			return n.commitContacts(s)
		}
	}
	if len(s.Contacts) >= 100 {
		return errors.New("Лимит прототипа: 100 контактов")
	}
	s.Contacts = append(s.Contacts, c)
	return n.commitContacts(s)
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
	if pending >= 1000 {
		return errors.New("Очередь содержит 1000 ожидающих сообщений. Дождитесь доставки.")
	}
	id := randomID()
	p := payload{Version: 2, ID: id, From: keyID(n.state.Public), To: peerID, Kind: "text", Text: text}
	env, e := seal(p, c, n.state.Secret)
	if e != nil {
		return e
	}
	if e = n.storeMessage(message{ID: id, Peer: peerID, Text: text, Out: true, Time: time.Now().UnixMilli(), Packet: &env}, -1); e != nil {
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
	_, duplicate := n.messageIndexes[recordID(message{Peer: p.From, ID: p.ID})]
	if !duplicate {
		if e = n.storeMessage(message{ID: p.ID, Peer: p.From, Text: p.Text, Delivered: true, Time: time.Now().UnixMilli()}, -1); e != nil {
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
	index, exists := n.messageIndexes[recordID(message{Peer: peerID, ID: messageID, Out: true})]
	if exists && !n.state.Messages[index].Delivered {
		m := n.state.Messages[index]
		m.Delivered = true
		m.Packet = nil
		return n.storeMessage(m, index)
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
	n.flushMu.Lock()
	defer n.flushMu.Unlock()
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
	// One sequential worker per contact keeps order. A failed contact consumes
	// at most one attempt per pass and cannot monopolize the global queue.
	groups := map[string][]message{}
	order := []string{}
	for _, m := range pending {
		if _, ok := groups[m.Peer]; !ok {
			order = append(order, m.Peer)
		}
		groups[m.Peer] = append(groups[m.Peer], m)
	}
	sem := make(chan struct{}, 4)
	var workers sync.WaitGroup
	for _, peer := range order {
		c, ok := peers[peer]
		if !ok {
			continue
		}
		n.mu.Lock()
		retry := n.retry[peer]
		n.mu.Unlock()
		if time.Now().Before(retry.next) {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			workers.Wait()
			return
		}
		workers.Add(1)
		go func(c contact, queue []message) {
			defer workers.Done()
			defer func() { <-sem }()
			for _, m := range queue {
				if ctx.Err() != nil {
					return
				}
				issue := n.deliverOne(ctx, client, m, c)
				if ctx.Err() != nil {
					return
				}
				n.recordDeliveryIssue(c.ID, issue)
				n.mu.Lock()
				if n.retry == nil {
					n.retry = map[string]retryState{}
				}
				if issue != "" {
					r := n.retry[c.ID]
					r.failures++
					delay := time.Duration(1<<min(r.failures, 5)) * time.Second
					n.retry[c.ID] = retryState{failures: r.failures, next: time.Now().Add(delay + time.Duration(time.Now().UnixNano()%1000)*time.Millisecond)}
				} else {
					delete(n.retry, c.ID)
				}
				n.mu.Unlock()
				if issue != "" {
					return
				}
			}
		}(c, groups[peer])
	}
	workers.Wait()
}

type retryState struct {
	failures int
	next     time.Time
}

func (n *Node) deliverOne(ctx context.Context, client *http.Client, m message, c contact) string {
	if !validAddress(c.Address) {
		return "Обменяйтесь новыми QR после обновления обоих телефонов. История сохранена."
	}
	raw, _ := json.Marshal(m.Packet)
	request, err := http.NewRequestWithContext(ctx, "POST", "http://"+net.JoinHostPort(c.Address, "47831")+"/message", bytes.NewReader(raw))
	if err != nil {
		return "Не удалось подготовить сообщение. Оно осталось в очереди."
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "Собеседник недоступен. Сообщение осталось в очереди; отправка будет повторена."
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return deliveryResponseIssue(response)
	}
	var ack envelope
	if err = json.NewDecoder(io.LimitReader(response.Body, 20000)).Decode(&ack); err != nil || n.applyAck(ack, m.Peer, m.ID) != nil {
		return "Не удалось проверить подтверждение доставки. Сообщение осталось в очереди."
	}
	return ""
}
