package mobile

import (
	"context"
	"errors"
	"net"
	"net/http"
	"tailscale.com/ipn/ipnstate"
	"time"
)

var (
	errUnknownContact = errors.New("unknown contact")
	errInvalidMessage = errors.New("invalid message")
	errHistoryFull    = errors.New("local history full")
)

func deliveryResponseIssue(res *http.Response) string {
	switch res.Header.Get("X-Blizko-Error") {
	case "unknown_contact":
		return "На телефоне собеседника нет вашего контакта. Попросите его заново добавить ваш QR. Сообщение осталось в очереди."
	case "invalid_message":
		return "Телефон собеседника отклонил сообщение. Проверьте QR-контакты на обоих телефонах. Сообщение осталось в очереди."
	case "history_full":
		return "На телефоне собеседника достигнут лимит истории. Сообщение осталось в очереди."
	case "storage_failed":
		return "Телефон собеседника не смог сохранить сообщение. Сообщение осталось в очереди."
	}
	if res.StatusCode == http.StatusForbidden {
		return "Телефон собеседника отклонил сообщение. Попросите его проверить ваш QR-контакт и обновить приложение."
	}
	return "Телефон собеседника ответил с ошибкой. Сообщение осталось в очереди; отправка будет повторена."
}

func (n *Node) recordDeliveryIssue(peer, issue string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.deliveryIssues == nil {
		n.deliveryIssues = map[string]string{}
	}
	pending := false
	for _, m := range n.state.Messages {
		if m.Out && m.Peer == peer && !m.Delivered {
			pending = true
			break
		}
	}
	if !pending {
		delete(n.deliveryIssues, peer)
	} else if issue != "" {
		n.deliveryIssues[peer] = issue
	}
}

func peerVisible(st *ipnstate.Status, address string) (visible, online bool) {
	for _, p := range st.Peer {
		for _, ip := range p.TailscaleIPs {
			if ip.String() == address {
				return true, p.Online
			}
		}
	}
	return false, false
}

// CheckContact checks the route and receiving port without sending a chat message.
func (n *Node) CheckContact(id string) (string, error) {
	n.mu.Lock()
	c, exists := n.peer(id)
	enabled := n.enabled
	n.mu.Unlock()
	if !exists {
		return "", errors.New("Контакт не найден")
	}
	if !enabled {
		return "Включите приём на этом телефоне.", nil
	}
	n.life.Lock()
	ts := n.ts
	n.life.Unlock()
	if ts == nil {
		return "Включите приём на этом телефоне.", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	lc, err := ts.LocalClient()
	if err != nil {
		return "Не удалось проверить подключение. Выключите и включите приём.", nil
	}
	st, err := lc.Status(ctx)
	if err != nil {
		return "Не удалось получить состояние сети. Проверьте интернет на этом телефоне.", nil
	}
	if st.BackendState != "Running" {
		return "Сначала завершите вход в Tailscale на этом телефоне.", nil
	}
	visible, online := peerVisible(st, c.Address)
	if !visible {
		return "Адрес собеседника не виден в вашей сети Tailscale. Пригласите друга в общую сеть и выберите её при входе. Если он уже в ней, заново обменяйтесь QR и проверьте разрешения сети.", nil
	}
	conn, err := ts.Dial(ctx, "tcp", net.JoinHostPort(c.Address, "47831"))
	if err != nil {
		if !online {
			return "Устройство собеседника видно в общей сети, но оно сейчас не подключено. Попросите его открыть «Близко» и включить приём.", nil
		}
		return "Устройство видно в общей сети, но соединение для сообщений не открывается. Включите приём у собеседника; если он включён, проверьте разрешения Tailscale для порта 47831.", nil
	}
	conn.Close()
	return "Соединение с телефоном собеседника открывается. Доставка сообщения подтверждается отдельно надписью «Доставлено».", nil
}

// ChangeNetwork starts a fresh Tailscale login selected by the user. Chat keys,
// contacts and queued messages remain in the existing encrypted vault.
func (n *Node) ChangeNetwork() error {
	n.life.Lock()
	defer n.life.Unlock()
	if n.ts == nil {
		return errors.New("Сначала включите приём, затем выберите другую сеть")
	}
	lc, err := n.ts.LocalClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if err = lc.SwitchToEmptyProfile(ctx); err != nil {
		return errors.New("Не удалось начать новый вход. Проверьте интернет и попробуйте снова.")
	}
	n.stopLocked()
	n.mu.Lock()
	s := n.clone()
	s.Address = ""
	err = n.commit(s)
	n.mu.Unlock()
	if err != nil {
		return errors.New("Не удалось сохранить смену подключения. История не удалена.")
	}
	return n.startLocked()
}
