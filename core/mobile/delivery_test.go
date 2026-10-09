package mobile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Route the real outbound HTTP request to a loopback receiver. This exercises
// both production HTTP paths without requiring a Tailscale account in tests.
func receiverClient(t *testing.T, peer *Node, handler http.Handler) *http.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	tr := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != net.JoinHostPort(peer.state.Address, "47831") {
			return nil, fmt.Errorf("unexpected destination: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr, Timeout: 3 * time.Second}
}

func TestHTTPMessagesInBothDirections(t *testing.T) {
	a, b := pair(t)
	if err := a.Send(b.self().ID, "Android → Android"); err != nil {
		t.Fatal(err)
	}
	if err := b.Send(a.self().ID, "Ответ"); err != nil {
		t.Fatal(err)
	}
	a.flush(context.Background(), receiverClient(t, b, b.messageHandler()))
	b.flush(context.Background(), receiverClient(t, a, a.messageHandler()))
	for _, n := range []*Node{a, b} {
		if len(n.state.Messages) != 2 {
			t.Fatalf("expected sent and received message, got %d", len(n.state.Messages))
		}
		for _, m := range n.state.Messages {
			if !m.Delivered || m.Packet != nil {
				t.Fatal("HTTP receipt was not applied")
			}
		}
		restored, err := NewNode(n.dir, bytes.Repeat([]byte{42}, 32))
		if err != nil || len(restored.state.Messages) != 2 {
			t.Fatal("HTTP delivery was not durable", err)
		}
	}
}

func TestHTTPUnknownContactRecoversAfterQRImport(t *testing.T) {
	a, b := testNode(t, "100.64.0.1"), testNode(t, "100.64.0.2")
	bc, _ := b.MyCode()
	ac, _ := a.MyCode()
	if err := a.AddContact("Друг", bc); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(b.self().ID, "Ждёт добавления QR"); err != nil {
		t.Fatal(err)
	}
	client := receiverClient(t, b, b.messageHandler())
	a.flush(context.Background(), client)
	if a.state.Messages[0].Delivered || a.state.Messages[0].Packet == nil || len(b.state.Messages) != 0 {
		t.Fatal("unknown sender was delivered or outbox lost")
	}
	if !strings.Contains(a.deliveryIssues[b.self().ID], "нет вашего контакта") {
		t.Fatal("receiver rejection was hidden")
	}
	if err := b.AddContact("Отправитель", ac); err != nil {
		t.Fatal(err)
	}
	a.flush(context.Background(), client)
	if !a.state.Messages[0].Delivered || len(b.state.Messages) != 1 || a.deliveryIssues[b.self().ID] != "" {
		t.Fatal("retry after QR import failed or diagnostic was not cleared")
	}
}

func TestHTTPCorruptReceiptRetriesWithoutDuplicate(t *testing.T) {
	a, b := pair(t)
	if err := a.Send(b.self().ID, "Одно сообщение"); err != nil {
		t.Fatal(err)
	}
	var corrupt atomic.Bool
	corrupt.Store(true)
	receiver := b.messageHandler()
	client := receiverClient(t, b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		receiver.ServeHTTP(recorder, r)
		var ack envelope
		if err := json.Unmarshal(recorder.Body.Bytes(), &ack); err != nil {
			t.Error(err)
			http.Error(w, "invalid", 500)
			return
		}
		if corrupt.Load() {
			ack.Body[0] ^= 1
		}
		json.NewEncoder(w).Encode(ack)
	}))
	a.flush(context.Background(), client)
	if a.state.Messages[0].Delivered || len(b.state.Messages) != 1 || a.deliveryIssues[b.self().ID] == "" {
		t.Fatal("unverified receipt accepted or failure hidden")
	}
	corrupt.Store(false)
	a.flush(context.Background(), client)
	if !a.state.Messages[0].Delivered || len(b.state.Messages) != 1 {
		t.Fatal("valid retry failed or duplicated message")
	}
}
