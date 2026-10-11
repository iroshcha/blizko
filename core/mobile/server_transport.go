package mobile

import (
	"blizko/core/transport"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const homeRelayURL = "https://ample-raven-6363.ru.tuna.am/"

type wireResponse = transport.Response
type serverLink struct{ client *transport.Client }

func (l *serverLink) exchangeContext(ctx context.Context, peer, data string) (wireResponse, error) {
	return l.client.Exchange(ctx, peer, data)
}
func (l *serverLink) RoundTrip(req *http.Request) (*http.Response, error) {
	data, e := io.ReadAll(io.LimitReader(req.Body, 20001))
	if e != nil || len(data) > 20000 {
		return nil, errors.New("invalid request")
	}
	result, e := l.exchangeContext(req.Context(), req.URL.Hostname(), string(data))
	if e != nil {
		return nil, e
	}
	header := make(http.Header)
	header.Set("X-Blizko-Error", result.Code)
	return &http.Response{StatusCode: result.Status, Header: header, Body: io.NopCloser(strings.NewReader(result.Data)), Request: req}, nil
}
func (n *Node) Start() error { n.life.Lock(); defer n.life.Unlock(); return n.startLocked() }
func (n *Node) startLocked() error {
	n.mu.Lock()
	if n.enabled {
		n.mu.Unlock()
		return nil
	}
	base := n.state.RelayURL
	if base == "" {
		base = homeRelayURL
	}
	if override := os.Getenv("BLIZKO_SERVER_URL"); override != "" {
		base = override
	} else if legacy := os.Getenv("BLIZKO_RELAY_URL"); legacy != "" {
		base = legacy
	}
	if _, e := transport.ServerURL(base); e != nil {
		n.mu.Unlock()
		return errors.New("Неверный адрес домашнего сервера")
	}
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	n.enabled = true
	n.online = false
	n.status = "Подключение к серверу…"
	n.revision++
	n.generation++
	gen := n.generation
	link := &serverLink{}
	link.client = transport.NewClient(n.state.NetworkSeed, base, n.serverPacket, func(online bool) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.generation != gen {
			return
		}
		n.online = online && (!n.networkKnown || n.networkAvailable)
		if n.online {
			n.status = "Подключено · через сервер"
			n.retry = map[string]retryState{}
			select {
			case n.wake <- struct{}{}:
			default:
			}
		} else if n.networkKnown && !n.networkAvailable {
			n.status = "Нет сетевого подключения"
		} else {
			n.status = "Связь с домашним сервером не установлена"
		}
		n.relay = base
		n.revision++
	})
	n.link = link
	n.mu.Unlock()
	go link.client.Run(ctx)
	go n.runDelivery(ctx, link)
	return nil
}
func (n *Node) Stop() { n.life.Lock(); defer n.life.Unlock(); n.stopLocked() }
func (n *Node) stopLocked() {
	n.mu.Lock()
	cancel := n.cancel
	n.cancel = nil
	n.link = nil
	n.generation++
	n.enabled = false
	n.online = false
	n.status = "Приём выключен"
	n.revision++
	n.relay = ""
	for _, p := range n.probes {
		p.cancel()
	}
	n.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (n *Node) runDelivery(ctx context.Context, link *serverLink) {
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
func (n *Node) commitContacts(state diskState) error {
	if err := n.commit(state); err != nil {
		return err
	}
	n.retry = map[string]retryState{}
	select {
	case n.wake <- struct{}{}:
	default:
	}
	return nil
}
func (n *Node) serverPacket(remote, data string) wireResponse {
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

func (n *Node) SetRelayURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = homeRelayURL
	}
	if _, err := transport.ServerURL(raw); err != nil {
		return errors.New("Введите HTTPS-адрес сервера без логина, пути и параметров")
	}
	u, err := url.Parse(raw)
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

// Retained for existing bridge callers; all traffic now uses the server.
func (n *Node) SetRelayOnly(bool) error { return nil }
func (n *Node) NetworkChanged() {
	n.mu.Lock()
	n.retry = map[string]retryState{}
	link := n.link
	n.mu.Unlock()
	if link != nil {
		link.client.Reconnect()
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
		return "Соединение с собеседником установлено, но у собеседника нет вашего нового QR. Обменяйтесь кодами в обе стороны.", nil
	}
	return "Собеседник ответил через сервер и подтвердил ваш контакт. Можно отправлять сообщения.", nil
}
