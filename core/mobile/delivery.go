package mobile

import (
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
