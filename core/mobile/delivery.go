package mobile

import (
	"context"
	"errors"
	"net/http"
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
		if m.Out && m.Peer == peer && !m.Delivered && !m.Cancelled && m.Failure == "" {
			pending = true
			break
		}
	}
	previous := n.deliveryIssues[peer]
	if !pending {
		delete(n.deliveryIssues, peer)
	} else if issue != "" {
		n.deliveryIssues[peer] = issue
	} else {
		delete(n.deliveryIssues, peer)
	}
	if previous != n.deliveryIssues[peer] {
		n.revision++
	}
}

type deliveryAttempt struct {
	cancel   context.CancelFunc
	peer, id string
}

func (n *Node) CancelMessage(peer, id string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	index, ok := n.messageIndexes[recordID(message{Peer: peer, ID: id, Out: true})]
	if !ok {
		return errors.New("Сообщение не найдено")
	}
	m := n.state.Messages[index]
	if m.Delivered {
		return errors.New("Сообщение уже доставлено")
	}
	if m.Cancelled {
		return nil
	}
	m.Cancelled = true
	m.Failure = ""
	if err := n.storeMessage(m, index); err != nil {
		return err
	}
	if attempt := n.inflight[recordID(m)]; attempt != nil {
		attempt.cancel()
	}
	delete(n.retry, peer)
	delete(n.deliveryIssues, peer)
	select {
	case n.wake <- struct{}{}:
	default:
	}
	return nil
}
func (n *Node) RetryMessage(peer, id string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	index, ok := n.messageIndexes[recordID(message{Peer: peer, ID: id, Out: true})]
	if !ok {
		return errors.New("Сообщение не найдено")
	}
	m := n.state.Messages[index]
	if m.Delivered {
		return errors.New("Сообщение уже доставлено")
	}
	if !validPacket(m.Packet) {
		return errors.New("Пакет слишком велик. Отмените сообщение и отправьте сокращённый текст.")
	}
	m.Cancelled = false
	m.Failure = ""
	if err := n.storeMessage(m, index); err != nil {
		return err
	}
	delete(n.retry, peer)
	delete(n.deliveryIssues, peer)
	select {
	case n.wake <- struct{}{}:
	default:
	}
	return nil
}
func (n *Node) failMessage(m message, reason string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	index, exists := n.messageIndexes[recordID(m)]
	if !exists {
		return
	}
	current := n.state.Messages[index]
	if current.Delivered || current.Cancelled {
		return
	}
	current.Failure = reason
	_ = n.storeMessage(current, index)
}
