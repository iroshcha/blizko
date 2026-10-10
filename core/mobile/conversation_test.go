package mobile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func reopen(t *testing.T, n *Node) *Node {
	t.Helper()
	again, err := NewNode(n.dir, bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return again
}
func TestInventoryRejectsMissingMetadataAndRecords(t *testing.T) {
	for _, loss := range []string{"metadata", "directory", "record", "journal", "head"} {
		t.Run(loss, func(t *testing.T) {
			a, b := pair(t)
			if err := a.Send(b.self().ID, "retained"); err != nil {
				t.Fatal(err)
			}
			path := a.storage.filename("chat-v2")
			switch loss {
			case "directory":
				path = filepath.Join(a.storage.dir, "messages")
			case "record":
				path = filepath.Join(a.storage.dir, "messages", recordID(a.state.Messages[0])+".enc")
			case "journal":
				path = filepath.Join(a.storage.dir, "records.log")
			case "head":
				path = a.storage.filename("records-head")
			}
			if err := os.Rename(path, path+".saved"); err != nil {
				t.Fatal(err)
			}
			if _, err := NewNode(a.dir, bytes.Repeat([]byte{42}, 32)); err == nil {
				t.Fatal("lost data silently accepted")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("missing storage silently recreated")
			}
			if err := os.Rename(path+".saved", path); err != nil {
				t.Fatal(err)
			}
			again := reopen(t, a)
			old, _ := a.MyCode()
			new, _ := again.MyCode()
			if old != new || len(again.state.Messages) != 1 {
				t.Fatal("loss handler changed identity or queue")
			}
		})
	}
}
func TestInventoryRecoversEachInterruptedCommitWithoutEarlyReceipt(t *testing.T) {
	for _, stage := range []string{"intent", "record", "journal", "head"} {
		t.Run(stage, func(t *testing.T) {
			a, b := pair(t)
			a.Send(b.self().ID, "durable before ACK")
			packet := *a.state.Messages[0].Packet
			b.storage.recordFault = func(at string) error {
				if at == stage {
					return errors.New("simulated power interruption")
				}
				return nil
			}
			if _, fresh, err := b.accept(packet); err == nil || fresh {
				t.Fatal("acknowledged interrupted write")
			}
			again := reopen(t, b)
			if len(again.state.Messages) != 1 || again.state.Messages[0].Text != "durable before ACK" {
				t.Fatal("recovery lost intent")
			}
			ack, fresh, err := again.accept(packet)
			if err != nil || fresh {
				t.Fatal("retry duplicated recovered message", err)
			}
			if err = a.applyAck(ack, b.self().ID, a.state.Messages[0].ID); err != nil {
				t.Fatal(err)
			}
			reopen(t, again)
		})
	}
}
func TestInventoryRecoversTornUncommittedTail(t *testing.T) {
	a, b := pair(t)
	a.storage.recordFault = func(stage string) error {
		if stage == "record" {
			return errors.New("interrupted")
		}
		return nil
	}
	if a.Send(b.self().ID, "tail recovery") == nil {
		t.Fatal("fault did not fire")
	}
	f, err := os.OpenFile(filepath.Join(a.storage.dir, "records.log"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte{80, 0, 0, 0, 1, 2})
	f.Close()
	again := reopen(t, a)
	if len(again.state.Messages) != 1 {
		t.Fatal("torn write lost intent")
	}
	reopen(t, again)
}
func TestMessageWireLimitRejectsEscapedTextBeforeQueue(t *testing.T) {
	a, b := pair(t)
	if a.Send(b.self().ID, "prefix"+strings.Repeat("\x00", 2000)+"suffix") == nil {
		t.Fatal("oversized encrypted packet accepted")
	}
	if len(a.state.Messages) != 0 {
		t.Fatal("bad text queued")
	}
	if err := a.Send(b.self().ID, strings.Repeat("я", 2000)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.accept(*a.state.Messages[0].Packet); err != nil {
		t.Fatal("valid boundary not deliverable", err)
	}
}
func TestCancelInflightAndRetryPreservesMessageID(t *testing.T) {
	a, b := pair(t)
	a.Send(b.self().ID, "cancel me")
	id := a.state.Messages[0].ID
	started := make(chan struct{})
	done := make(chan struct{})
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	go func() { a.flush(context.Background(), client); close(done) }()
	<-started
	if err := a.CancelMessage(b.self().ID, id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt exchange")
	}
	again := reopen(t, a)
	if !again.state.Messages[0].Cancelled || again.state.Messages[0].Packet == nil {
		t.Fatal("cancel not durable or retry lost packet")
	}
	if err := again.RetryMessage(b.self().ID, id); err != nil {
		t.Fatal(err)
	}
	packet := *again.state.Messages[0].Packet
	ack, fresh, err := b.accept(packet)
	if err != nil || !fresh {
		t.Fatal(err)
	}
	if err = again.applyAck(ack, b.self().ID, id); err != nil {
		t.Fatal(err)
	}
	if again.state.Messages[0].ID != id || !again.state.Messages[0].Delivered || again.state.Messages[0].Cancelled {
		t.Fatal("retry changed ID or lost receipt")
	}
}
func TestUnreadSearchAndEncryptedDraftSurviveRestart(t *testing.T) {
	a, b := pair(t)
	for i := 0; i < 65; i++ {
		a.Send(b.self().ID, "Поиск 🌿")
		b.accept(*a.state.Messages[i].Packet)
	}
	var page struct {
		Messages []message
		Before   int64
		Unread   map[string]int
		Draft    string
	}
	json.Unmarshal([]byte(b.UnreadPage(a.self().ID, 50)), &page)
	if len(page.Messages) != 50 || page.Messages[0].Order != 1 || page.Unread[a.self().ID] != 65 {
		t.Fatal("first unread page wrong")
	}
	b.MarkRead(a.self().ID, 50)
	b.SaveDraft(a.self().ID, "новый черновик")
	b.ClearSentDraft(a.self().ID, "предыдущий текст")
	again := reopen(t, b)
	json.Unmarshal([]byte(again.SearchPage(a.self().ID, "поИСК", 0, 50)), &page)
	if len(page.Messages) != 50 || page.Unread[a.self().ID] != 15 || page.Draft != "новый черновик" {
		t.Fatal("read state, search or draft lost")
	}
	code, _ := a.MyCode()
	history := again.historyIndexes[a.self().ID]
	again.AddContact("Новое имя", code)
	if &history[0] != &again.historyIndexes[a.self().ID][0] {
		t.Fatal("contact update rebuilt history indexes")
	}
	again.ClearSentDraft(a.self().ID, "новый черновик")
	if reopen(t, again).state.Drafts[a.self().ID] != "" {
		t.Fatal("sent draft persisted")
	}
}
func TestBackgroundClearIsCancellableAndRetainsQueue(t *testing.T) {
	a, b := pair(t)
	for i := 0; i < 35; i++ {
		a.Send(b.self().ID, "clearable")
		b.accept(*a.state.Messages[i].Packet)
	}
	b.Send(a.self().ID, "pending stays")
	b.storage.recordFault = func(stage string) error {
		if stage == "intent" {
			time.Sleep(4 * time.Millisecond)
		}
		return nil
	}
	start := time.Now()
	if err := b.BeginClearHistory(a.self().ID); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("clear start blocks commands")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		done := b.clearTask.Done
		b.mu.Unlock()
		if done >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	b.CancelClearHistory()
	for time.Now().Before(deadline) {
		b.mu.Lock()
		running := b.clearTask.Running
		b.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(time.Millisecond)
	}
	b.mu.Lock()
	running, cancelled := b.clearTask.Running, b.clearTask.Cancelled
	b.mu.Unlock()
	if running || !cancelled {
		t.Fatal("clear cancellation failed")
	}
	again := reopen(t, b)
	found := false
	for _, m := range again.state.Messages {
		if m.Text == "pending stays" && !m.Delivered {
			found = true
		}
	}
	if !found {
		t.Fatal("clear lost pending queue")
	}
}
