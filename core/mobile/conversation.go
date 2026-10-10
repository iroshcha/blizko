package mobile

import (
	"context"
	"errors"
	"strings"
)

func (n *Node) SaveDraft(peer, text string) error {
	if len(text) > maxText {
		return errors.New("Черновик превышает 4000 байт текста")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.peer(peer); !ok {
		return errors.New("Контакт не найден")
	}
	if n.state.Drafts[peer] == text {
		return nil
	}
	state := n.cloneMetadata()
	state.Drafts = map[string]string{}
	for id, value := range n.state.Drafts {
		state.Drafts[id] = value
	}
	if text == "" {
		delete(state.Drafts, peer)
	} else {
		state.Drafts[peer] = text
	}
	return n.saveMetadata(state)
}

// Clear a saved draft only if no newer text replaced the sent draft.
func (n *Node) ClearSentDraft(peer, sent string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state.Drafts[peer] != sent {
		return nil
	}
	state := n.cloneMetadata()
	state.Drafts = map[string]string{}
	for id, value := range n.state.Drafts {
		if id != peer {
			state.Drafts[id] = value
		}
	}
	return n.saveMetadata(state)
}
func (n *Node) MarkRead(peer string, through int64) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.peer(peer); !ok {
		return errors.New("Контакт не найден")
	}
	if through <= n.state.ReadThrough[peer] {
		return nil
	}
	var latest int64
	indices := n.historyIndexes[peer]
	if len(indices) > 0 {
		latest = n.state.Messages[indices[len(indices)-1]].Order
	}
	if through > latest {
		through = latest
	}
	if through <= n.state.ReadThrough[peer] {
		return nil
	}
	state := n.cloneMetadata()
	state.ReadThrough = map[string]int64{}
	for id, value := range n.state.ReadThrough {
		state.ReadThrough[id] = value
	}
	state.ReadThrough[peer] = through
	return n.saveMetadata(state)
}

type clearProgress struct {
	Peer      string `json:"peer"`
	Running   bool   `json:"running"`
	Done      int    `json:"done"`
	Total     int    `json:"total"`
	Error     string `json:"error"`
	Cancelled bool   `json:"cancelled"`
}

func (n *Node) BeginClearHistory(peer string) error {
	n.mu.Lock()
	if _, ok := n.peer(peer); !ok {
		n.mu.Unlock()
		return errors.New("Контакт не найден")
	}
	if n.clearTask != nil && n.clearTask.Running {
		n.mu.Unlock()
		return errors.New("Очистка уже выполняется")
	}
	ctx, cancel := context.WithCancel(context.Background())
	n.clearCancel = cancel
	n.clearTask = &clearProgress{Peer: peer, Running: true}
	task := n.clearTask
	n.revision++
	n.mu.Unlock()
	go func() {
		defer cancel()
		err := n.clearHistory(ctx, peer, func(done, total int) { n.mu.Lock(); task.Done = done; task.Total = total; n.revision++; n.mu.Unlock() })
		n.mu.Lock()
		defer n.mu.Unlock()
		task.Running = false
		task.Cancelled = errors.Is(err, context.Canceled)
		if err != nil && !task.Cancelled {
			task.Error = strings.TrimSpace(err.Error())
		}
		n.clearCancel = nil
		n.revision++
	}()
	return nil
}
func (n *Node) CancelClearHistory() {
	n.mu.Lock()
	cancel := n.clearCancel
	n.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
