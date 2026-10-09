// Package mobile is the same embedded Tailscale/chat engine on Android and iOS.
// Its gomobile API only exposes strings, byte arrays and errors.
package mobile

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"tailscale.com/tsnet"
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
	Version  int       `json:"v"`
	Public   [32]byte  `json:"public"`
	Secret   [32]byte  `json:"secret"`
	Contacts []contact `json:"contacts"`
	Messages []message `json:"messages"`
	Address  string    `json:"address"`
	DNSName  string    `json:"dns,omitempty"`
	Invite   string    `json:"invite,omitempty"`
}
type Node struct {
	mu             sync.Mutex
	life           sync.Mutex
	state          diskState
	storage        *vault
	dir            string
	ts             *tsnet.Server
	cancel         context.CancelFunc
	wake           chan struct{}
	status         string
	authURL        string
	enabled        bool
	online         bool
	generation     int
	deliveryIssues map[string]string // Local runtime diagnostics, never message contents or server storage.
	tailnet        string
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
		n.state = diskState{Version: 2, Public: *pub, Secret: *priv, Contacts: []contact{}, Messages: []message{}}
		if e = n.commit(n.state); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, errors.New("Не удалось расшифровать хранилище. Данные не удалены.")
	} else if e = json.Unmarshal(b, &n.state); e != nil || n.state.Version != 2 {
		return nil, errors.New("Неизвестный формат хранилища")
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
	return contact{ID: keyID(n.state.Public), Key: n.state.Public, Address: n.state.Address, DNSName: n.state.DNSName, Invite: n.state.Invite}
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
	s := map[string]any{"status": n.status, "enabled": n.enabled, "online": n.online, "authURL": n.authURL, "address": n.state.Address, "id": keyID(n.state.Public), "contacts": n.state.Contacts, "messages": msgs, "incoming": incoming}
	s["deliveryIssues"] = n.deliveryIssues
	s["tailnet"] = n.tailnet
	b, _ := json.Marshal(s)
	return string(b)
}
func (n *Node) MyCode() (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !validAddress(n.state.Address) {
		return "", errors.New("Сначала войдите в Tailscale")
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
	c.Invite = "" // Do not retain the recipient's invitation capability after import.
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
func (n *Node) Start() error {
	n.life.Lock()
	defer n.life.Unlock()
	return n.startLocked()
}
func (n *Node) startLocked() error {
	n.mu.Lock()
	if n.enabled {
		n.mu.Unlock()
		return nil
	}
	n.mu.Unlock()
	networkDir, err := prepareNetworkDirectory(n.dir)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.enabled = true
	n.online = false
	n.status = "Подключение к Tailscale…"
	n.authURL = ""
	n.generation++
	gen := n.generation
	hostname := "blizko-" + keyID(n.state.Public)[:10]
	n.mu.Unlock()
	ts := &tsnet.Server{Dir: networkDir, Store: n.storage, Hostname: hostname, Logf: func(string, ...any) {}, UserLogf: func(string, ...any) {}}
	ctx, cancel := context.WithCancel(context.Background())
	n.ts = ts
	n.cancel = cancel
	if e := ts.Start(); e != nil {
		cancel()
		_ = ts.Close()
		n.ts = nil
		n.mu.Lock()
		n.enabled = false
		n.status = "Не удалось запустить Tailscale"
		n.mu.Unlock()
		return e
	}
	go n.run(ctx, ts, gen)
	return nil
}

// Android has no writable HOME, /var/lib or /tmp for application UIDs.
// tsnet's Dir does not configure ipnlocal's separate sockstat logger: it calls
// logpolicy.LogsDir even when log upload is disabled. Give it an existing
// app-private directory before tsnet starts, otherwise that lookup panics.
// There is one embedded node per app process; these settings are process-wide.
func prepareNetworkDirectory(dir string) (string, error) {
	networkDir := filepath.Join(dir, "network")
	if err := os.MkdirAll(networkDir, 0700); err != nil {
		return "", errors.New("Не удалось создать служебную папку подключения")
	}
	if err := os.Setenv("TS_LOGS_DIR", networkDir); err != nil {
		return "", err
	}
	if err := os.Setenv("TS_NO_LOGS_NO_SUPPORT", "true"); err != nil {
		return "", err
	}
	return networkDir, nil
}

func (n *Node) Stop() {
	n.life.Lock()
	defer n.life.Unlock()
	n.stopLocked()
}
func (n *Node) stopLocked() {
	if n.cancel != nil {
		n.cancel()
	}
	if n.ts != nil {
		_ = n.ts.Close()
		n.ts = nil
	}
	n.mu.Lock()
	n.generation++
	n.enabled = false
	n.online = false
	n.authURL = ""
	n.status = "Приём выключен"
	n.tailnet = ""
	n.mu.Unlock()
}
func (n *Node) setStatus(gen int, status, url string, online bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.generation == gen {
		n.status = status
		n.authURL = url
		n.online = online
	}
}
func (n *Node) run(ctx context.Context, ts *tsnet.Server, gen int) {
	lc, e := ts.LocalClient()
	if e != nil {
		n.setStatus(gen, "Ошибка сетевого модуля", "", false)
		return
	}
	// tsnet starts the interactive login; LocalClient exposes the actual AuthURL.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var listener net.Listener
	var nextLoginRequest time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		st, e := lc.Status(ctx)
		if e != nil {
			continue
		}
		if st.BackendState != "Running" || len(st.TailscaleIPs) == 0 {
			// tsnet checks NeedsLogin only once during Start. The backend can
			// reach that state later, especially when restarting a saved node.
			if st.BackendState == "NeedsLogin" && st.AuthURL == "" && time.Now().After(nextLoginRequest) {
				loginCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				_ = lc.StartLoginInteractive(loginCtx)
				cancel()
				nextLoginRequest = time.Now().Add(20 * time.Second)
			}
			n.setStatus(gen, "Войдите в Tailscale", st.AuthURL, false)
			continue
		}
		address := st.TailscaleIPs[0].String()
		n.mu.Lock()
		if n.generation != gen || ctx.Err() != nil {
			n.mu.Unlock()
			return
		}
		s := n.clone()
		if s.Address != address {
			s.Invite = ""
		}
		s.Address = address
		if st.Self != nil {
			s.DNSName = strings.TrimSuffix(st.Self.DNSName, ".")
		}
		if st.CurrentTailnet != nil {
			n.tailnet = st.CurrentTailnet.Name
		}
		e = n.commit(s)
		n.mu.Unlock()
		if e != nil {
			n.setStatus(gen, "Не удалось сохранить сетевой адрес", "", false)
			return
		}
		listener, e = ts.Listen("tcp", ":47831")
		if e != nil {
			n.setStatus(gen, "Не удалось открыть приём сообщений", "", false)
			return
		}
		break
	}
	n.setStatus(gen, "Подключено · Tailscale внутри приложения", "", true)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 4096, Handler: n.messageHandler()}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	tr := &http.Transport{DialContext: ts.Dial, MaxConnsPerHost: 2, ResponseHeaderTimeout: 10 * time.Second}
	client := &http.Client{Transport: tr, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }}
	defer tr.CloseIdleConnections()
	retry := time.NewTicker(15 * time.Second)
	defer retry.Stop()
	for {
		st, err := lc.Status(ctx)
		var routes map[string]string
		if err == nil {
			routes = sharedRoutes(st)
		}
		n.flushRoutes(ctx, client, routes)
		select {
		case <-ctx.Done():
			return
		case <-retry.C:
		case <-n.wake:
		}
		st, e := lc.Status(ctx)
		if e == nil && st.BackendState == "Running" {
			n.setStatus(gen, "Подключено · Tailscale внутри приложения", "", true)
		} else {
			n.setStatus(gen, "Ожидание сети", "", false)
		}
	}
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
	n.flushRoutes(ctx, client, nil)
}
func (n *Node) flushRoutes(ctx context.Context, client *http.Client, routes map[string]string) {
	n.mu.Lock()
	var pending []message
	peers := map[string]contact{}
	for _, m := range n.state.Messages {
		if m.Out && !m.Delivered && m.Packet != nil {
			pending = append(pending, m)
			if c, ok := n.peer(m.Peer); ok {
				if address := routes[c.DNSName]; address != "" {
					c.Address = address
				}
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
			issue := "Телефон собеседника недоступен. Откройте «Близко» и включите приём на обоих телефонах. Для разных аккаунтов подтвердите взаимный доступ по QR с приглашением."
			defer func() {
				if ctx.Err() == nil {
					n.recordDeliveryIssue(c.ID, issue)
				}
			}()
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
