package mobile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHistoryPast2000PagedClearAndDuplicateAfterRestart(t *testing.T) {
	a, b := pair(t)
	// Seed a realistic old archive without spending time on 2001 fsyncs.
	for i := 0; i < 2001; i++ {
		b.state.Messages = append(b.state.Messages, message{ID: fmt.Sprint(i), Peer: a.self().ID, Text: "old", Delivered: true, Order: int64(i + 1)})
	}
	b.rebuildIndexes()
	if err := a.Send(b.self().ID, "new incoming"); err != nil {
		t.Fatal(err)
	}
	packet := *a.state.Messages[0].Packet
	if _, fresh, err := b.accept(packet); err != nil || !fresh {
		t.Fatal("history stopped receiving", err)
	}
	if err := b.Send(a.self().ID, "new outgoing"); err != nil {
		t.Fatal("history stopped sending", err)
	}
	var page struct {
		Messages []message
		HasMore  bool
		Previews map[string]string
	}
	if err := json.Unmarshal([]byte(b.SnapshotPage(a.self().ID, 0, 50)), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 50 || !page.HasMore || page.Messages[49].Text != "new outgoing" {
		t.Fatal("bad latest page")
	}
	cursor := page.Messages[0].Order
	if err := json.Unmarshal([]byte(b.SnapshotPage(a.self().ID, cursor, 50)), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 50 || page.Messages[49].Order >= cursor {
		t.Fatal("paging overlaps")
	}
	// Only durable rows are used below; the synthetic archive is not on disk.
	again, err := NewNode(b.dir, bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	code, _ := again.MyCode()
	if err = again.ClearHistory(a.self().ID); err != nil {
		t.Fatal(err)
	}
	restored, err := NewNode(b.dir, bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	after, _ := restored.MyCode()
	if after != code {
		t.Fatal("clearing changed identity")
	}
	if _, fresh, err := restored.accept(packet); err != nil || fresh {
		t.Fatal("cleared message reappeared", err)
	}
	if err = json.Unmarshal([]byte(restored.SnapshotPage(a.self().ID, 0, 50)), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].Text != "new outgoing" || page.Messages[0].Delivered {
		t.Fatal("clear lost pending message or exposed archived text")
	}
}

func TestV3MigrationRetainsKeysOutboxAndEncryptedRecords(t *testing.T) {
	a, b := pair(t)
	if err := a.Send(b.self().ID, "migration secret"); err != nil {
		t.Fatal(err)
	}
	legacy := a.clone()
	legacy.Version = 3
	legacy.Messages[0].Order = 0
	raw, _ := json.Marshal(legacy)
	if err := a.storage.write("chat-v2", raw); err != nil {
		t.Fatal(err)
	}
	oldCode, _ := a.MyCode()
	migrated, err := NewNode(a.dir, bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	newCode, _ := migrated.MyCode()
	if newCode != oldCode || migrated.state.Version != 4 || len(migrated.state.Messages) != 1 || migrated.state.Messages[0].Packet == nil {
		t.Fatal("migration lost identity or outbox")
	}
	raw, err = migrated.storage.read("chat-v2")
	if err != nil {
		t.Fatal(err)
	}
	var metadata diskState
	if err = json.Unmarshal(raw, &metadata); err != nil || len(metadata.Messages) != 0 {
		t.Fatal("metadata contains history", err)
	}
	path := filepath.Join(migrated.storage.dir, "messages", recordID(migrated.state.Messages[0])+".enc")
	encrypted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("migration secret")) {
		t.Fatal("plaintext on disk")
	}
	encrypted[len(encrypted)-1] ^= 1
	if err = os.WriteFile(path, encrypted, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = NewNode(a.dir, bytes.Repeat([]byte{42}, 32)); err == nil {
		t.Fatal("corrupt history silently accepted")
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("damaged data deleted")
	}
}

func TestMessageWriteFailureProducesNoReceipt(t *testing.T) {
	a, b := pair(t)
	if err := a.Send(b.self().ID, "must persist"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.storage.dir, "messages"), []byte("block directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, fresh, err := b.accept(*a.state.Messages[0].Packet); err == nil || fresh || len(b.state.Messages) != 0 {
		t.Fatal("acknowledged failed storage")
	}
}

func TestQueueOfflinePeerAttemptsOnceAndKeepsOtherContactOrder(t *testing.T) {
	a, b := pair(t)
	offline := testNode(t, "")
	code, _ := offline.MyCode()
	if err := a.AddContact("offline", code); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 99; i++ {
		if err := a.Send(offline.self().ID, "waiting"); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"first", "second", "third"} {
		if err := a.Send(b.self().ID, value); err != nil {
			t.Fatal(err)
		}
	}
	var lock sync.Mutex
	var order []string
	offlineAttempts := 0
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == offline.state.Address {
			lock.Lock()
			offlineAttempts++
			lock.Unlock()
			return nil, fmt.Errorf("offline")
		}
		var packet envelope
		if err := json.NewDecoder(req.Body).Decode(&packet); err != nil {
			return nil, err
		}
		p, err := openEnvelope(packet, a.self(), b.state.Secret, b.self().ID)
		if err != nil {
			return nil, err
		}
		if p.Text == "first" {
			time.Sleep(20 * time.Millisecond)
		}
		lock.Lock()
		order = append(order, p.Text)
		lock.Unlock()
		ack, _, err := b.accept(packet)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(ack)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: ioBody(string(raw))}, nil
	})}
	a.flush(context.Background(), client)
	a.flush(context.Background(), client)
	if offlineAttempts != 1 || strings.Join(order, ",") != "first,second,third" {
		t.Fatalf("attempts=%d order=%v", offlineAttempts, order)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func ioBody(raw string) *bodyReader                                          { return &bodyReader{strings.NewReader(raw)} }

type bodyReader struct{ *strings.Reader }

func (*bodyReader) Close() error { return nil }

func BenchmarkStatusLargeHistory(b *testing.B) {
	n := &Node{state: diskState{Messages: make([]message, 100000)}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n.Status()
	}
}
