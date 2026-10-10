package mobile

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type bridgeResult struct {
	Handle  uint64 `json:"handle"`
	Address string `json:"address"`
	Request uint64 `json:"request"`
	Peer    string `json:"peer"`
	Data    string `json:"data"`
	Online  bool   `json:"online"`
	Relay   string `json:"relay"`
	Error   string `json:"error"`
}

const homeRelayURL = "https://ample-raven-6363.ru.tuna.am/"

type irohLink struct {
	handle uint64
	once   sync.Once
}

func bridge(args map[string]any) (bridgeResult, error) {
	b, err := json.Marshal(args)
	if err != nil {
		return bridgeResult{}, err
	}
	var result bridgeResult
	if err = json.Unmarshal([]byte(nativeCall(string(b))), &result); err != nil {
		return result, err
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	return result, nil
}
func (l *irohLink) call(op string, args map[string]any) (bridgeResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	args["op"] = op
	args["handle"] = l.handle
	return bridge(args)
}
func (l *irohLink) close() { l.once.Do(func() { _, _ = l.call("close", nil) }) }

type wireResponse struct {
	Status int    `json:"status"`
	Code   string `json:"code,omitempty"`
	Data   string `json:"data,omitempty"`
}

var exchangeSequence atomic.Uint64

func (l *irohLink) exchange(peer, data string) (wireResponse, error) {
	return l.exchangeContext(context.Background(), peer, data)
}
func (l *irohLink) exchangeContext(ctx context.Context, peer, data string) (wireResponse, error) {
	if err := ctx.Err(); err != nil {
		return wireResponse{}, err
	}
	id := exchangeSequence.Add(1)
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
		case <-ctx.Done():
			_, _ = l.call("cancel", map[string]any{"request": id})
		}
	}()
	defer close(done)
	result, err := l.call("exchange", map[string]any{"peer": peer, "data": data, "request": id})
	if err != nil {
		return wireResponse{}, err
	}
	var response wireResponse
	err = json.Unmarshal([]byte(result.Data), &response)
	if err == nil && (response.Status < 200 || response.Status > 599) {
		err = errors.New("invalid_response")
	}
	return response, err
}
func (l *irohLink) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(req.Body, 20001))
	if err != nil || len(data) > 20000 {
		return nil, errors.New("invalid_request")
	}
	result, err := l.exchangeContext(req.Context(), req.URL.Hostname(), string(data))
	if err != nil {
		return nil, err
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("X-Blizko-Error", result.Code)
	return &http.Response{StatusCode: result.Status, Header: header, Body: io.NopCloser(bytes.NewBufferString(result.Data)), Request: req}, nil
}

func (n *Node) Start() error { n.life.Lock(); defer n.life.Unlock(); return n.startLocked() }
func (n *Node) startLocked() error {
	n.mu.Lock()
	if n.enabled {
		n.mu.Unlock()
		return nil
	}
	seed, relayOnly, address, relayURL := n.state.IrohSeed, n.state.RelayOnly, n.state.Address, n.state.RelayURL
	peers := n.allowedLocked()
	n.mu.Unlock()
	result, err := bridge(map[string]any{"op": "start", "key": hex.EncodeToString(seed[:]), "relayOnly": relayOnly, "peers": peers, "relayURL": relayURL})
	if err != nil {
		return errors.New("Не удалось запустить iroh. Выключите и включите приём.")
	}
	link := &irohLink{handle: result.Handle}
	if result.Handle == 0 || result.Address != address {
		link.close()
		return errors.New("Ошибка проверки ключа устройства")
	}
	ctx, cancel := context.WithCancel(context.Background())
	n.mu.Lock()
	n.link = link
	_, _ = link.call("allow", map[string]any{"peers": n.allowedLocked()})
	n.cancel = cancel
	n.enabled = true
	n.online = false
	n.status = "Подключение к iroh…"
	n.revision++
	n.generation++
	gen := n.generation
	n.mu.Unlock()
	go n.receiveIroh(ctx, link)
	go n.runIroh(ctx, link, gen)
	go n.runDelivery(ctx, link)
	return nil
}
func (n *Node) Stop() { n.life.Lock(); defer n.life.Unlock(); n.stopLocked() }
func (n *Node) stopLocked() {
	n.mu.Lock()
	cancel, link := n.cancel, n.link
	n.cancel = nil
	n.link = nil
	n.generation++
	n.enabled = false
	n.online = false
	n.status = "Приём выключен"
	n.revision++
	n.relay = ""
	for _, probe := range n.probes {
		probe.cancel()
	}
	n.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if link != nil {
		link.close()
	}
}
func (n *Node) runIroh(ctx context.Context, link *irohLink, gen int) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		status, err := link.call("status", nil)
		n.mu.Lock()
		if n.generation != gen {
			n.mu.Unlock()
			return
		}
		previousStatus, previousOnline, previousRelay := n.status, n.online, n.relay
		n.online = err == nil && status.Online && (!n.networkKnown || n.networkAvailable)
		n.relay = status.Relay
		if n.online {
			n.status = "Подключено · iroh"
			if n.state.RelayOnly {
				n.status += " · через ретранслятор"
			}
		} else {
			if n.networkKnown && !n.networkAvailable {
				n.status = "Нет сетевого подключения"
			} else {
				n.status = "Связь с домашним сервером не установлена"
			}
		}
		if previousStatus != n.status || previousOnline != n.online || previousRelay != n.relay {
			n.revision++
		}
		n.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Delivery can wait for peers without delaying network status or UI polling.
func (n *Node) runDelivery(ctx context.Context, link *irohLink) {
	client := &http.Client{Transport: link}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		n.flush(ctx, client)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-n.wake:
		}
	}
}
func (n *Node) allowedLocked() []string {
	peers := []string{}
	for _, c := range n.state.Contacts {
		if validAddress(c.Address) {
			peers = append(peers, c.Address)
		}
	}
	return peers
}
func (n *Node) commitContacts(state diskState) error {
	if err := n.commit(state); err != nil {
		return err
	}
	if n.link != nil {
		_, _ = n.link.call("allow", map[string]any{"peers": n.allowedLocked()})
	}
	n.retry = map[string]retryState{}
	select {
	case n.wake <- struct{}{}:
	default:
	}
	return nil
}
func (n *Node) receiveIroh(ctx context.Context, link *irohLink) {
	for ctx.Err() == nil {
		event, err := link.call("next", nil)
		if err != nil {
			return
		}
		if event.Request == 0 {
			continue
		}
		response := n.irohPacket(event.Peer, event.Data)
		data, _ := json.Marshal(response)
		_, _ = link.call("reply", map[string]any{"request": event.Request, "data": string(data)})
	}
}
func (n *Node) irohPacket(remote, data string) wireResponse {
	n.mu.Lock()
	peerID := ""
	for _, contact := range n.state.Contacts {
		if validAddress(contact.Address) && contact.Address == remote {
			peerID = contact.ID
			break
		}
	}
	n.mu.Unlock()
	if peerID == "" {
		return wireResponse{Status: 403, Code: "unknown_contact"}
	}
	if data == `{"probe":true}` {
		return wireResponse{Status: 200}
	}
	var env envelope
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	decoder.DisallowUnknownFields()
	if len(data) > 20000 || decoder.Decode(&env) != nil || env.From != peerID {
		return wireResponse{Status: 403, Code: "invalid_message"}
	}
	ack, _, err := n.accept(env)
	if err != nil {
		code, status := "storage_failed", 503
		switch {
		case errors.Is(err, errUnknownContact):
			code, status = "unknown_contact", 403
		case errors.Is(err, errInvalidMessage):
			code, status = "invalid_message", 403
		case errors.Is(err, errHistoryFull):
			code, status = "history_full", 507
		}
		return wireResponse{Status: status, Code: code}
	}
	encoded, _ := json.Marshal(ack)
	return wireResponse{Status: 200, Data: string(encoded)}
}

// SetRelayOnly is a persistent diagnostic mode. Restart the endpoint, never the identity.
func (n *Node) SetRelayURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = homeRelayURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return errors.New("Введите HTTPS-адрес сервера без логина, пути и параметров")
	}
	if u.Port() != "" {
		port, e := strconv.Atoi(u.Port())
		if e != nil || port < 1 || port > 65535 {
			return errors.New("Неверный порт сервера")
		}
	}
	u.Path = "/"
	raw = u.String()
	n.life.Lock()
	defer n.life.Unlock()
	n.mu.Lock()
	if n.state.RelayURL == raw {
		n.mu.Unlock()
		return nil
	}
	state := n.cloneMetadata()
	state.RelayURL = raw
	enabled := n.enabled
	err = n.commit(state)
	n.mu.Unlock()
	if err != nil {
		return err
	}
	if enabled {
		n.stopLocked()
		return n.startLocked()
	}
	return nil
}
func (n *Node) SetRelayOnly(value bool) error {
	n.life.Lock()
	defer n.life.Unlock()
	n.mu.Lock()
	state := n.cloneMetadata()
	enabled := n.enabled
	state.RelayOnly = value
	err := n.commit(state)
	n.mu.Unlock()
	if err != nil {
		return err
	}
	if enabled {
		n.stopLocked()
		return n.startLocked()
	}
	return nil
}
func (n *Node) NetworkChanged() {
	n.mu.Lock()
	n.retry = map[string]retryState{}
	link := n.link
	n.mu.Unlock()
	if link != nil {
		_, _ = link.call("network", nil)
		select {
		case n.wake <- struct{}{}:
		default:
		}
	}
}
func (n *Node) SetNetworkAvailable(value bool) {
	n.mu.Lock()
	changed := !n.networkKnown || n.networkAvailable != value
	n.networkKnown = true
	n.networkAvailable = value
	if changed {
		n.revision++
	}
	n.mu.Unlock()
	if changed {
		n.NetworkChanged()
	}
}
func (n *Node) CancelContactCheck(id string) {
	n.mu.Lock()
	attempt := n.probes[id]
	n.mu.Unlock()
	if attempt != nil {
		attempt.cancel()
	}
}
func (n *Node) CheckContact(id string) (string, error) {
	n.mu.Lock()
	peer, exists := n.peer(id)
	link := n.link
	known, available, online, server := n.networkKnown, n.networkAvailable, n.online, n.state.RelayURL
	n.mu.Unlock()
	if !exists {
		return "", errors.New("Контакт не найден")
	}
	if !validAddress(peer.Address) {
		return "Обновите приложение на обоих устройствах и обменяйтесь новыми QR. История сохранена.", nil
	}
	if link == nil {
		return "Включите приём на этом устройстве.", nil
	}
	if known && !available {
		return "На этом устройстве нет сетевого подключения. Подключитесь к Wi-Fi или мобильной сети.", nil
	}
	if server == "" {
		server = homeRelayURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	attempt := &deliveryAttempt{cancel: cancel}
	n.mu.Lock()
	if n.probes == nil {
		n.probes = map[string]*deliveryAttempt{}
	}
	if previous := n.probes[id]; previous != nil {
		previous.cancel()
	}
	n.probes[id] = attempt
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		if n.probes[id] == attempt {
			delete(n.probes, id)
		}
		n.mu.Unlock()
	}()
	result, err := link.exchangeContext(ctx, peer.Address, `{"probe":true}`)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return "Проверка отменена.", nil
		}
		n.mu.Lock()
		online = n.online
		n.mu.Unlock()
		if !online {
			return "Связь с домашним сервером не установлена: " + server + "\nПроверьте, что серверный компьютер и туннель включены. Собеседник также не ответил; причина на его устройстве неизвестна.", nil
		}
		return "Связь с домашним сервером есть. Собеседник не отвечает: у него может быть выключен приём или отсутствовать сеть. Проверьте приём на обоих устройствах и наличие ваших QR в обе стороны.", nil
	}
	if result.Status != 200 {
		return "Соединение iroh установлено, но у собеседника нет вашего нового QR. Обменяйтесь кодами в обе стороны.", nil
	}
	return "Собеседник ответил через iroh и подтвердил ваш контакт. Можно отправлять сообщения.", nil
}
