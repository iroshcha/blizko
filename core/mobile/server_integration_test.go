package mobile

import (
	"blizko/core/transport"
	"bytes"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	until := time.Now().Add(20 * time.Second)
	for time.Now().Before(until) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}
func delivered(n *Node, text string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, m := range n.state.Messages {
		if m.Text == text && m.Out && m.Delivered {
			return true
		}
	}
	return false
}
func online(n *Node) bool { n.mu.Lock(); defer n.mu.Unlock(); return n.online }
func TestServerDeliveryOutboxRestartAndIdentity(t *testing.T) {
	broker := transport.NewServer()
	server := httptest.NewServer(broker)
	defer func() { broker.Close(); server.Close() }()
	t.Setenv("BLIZKO_SERVER_URL", server.URL)
	a, b := pair(t)
	defer a.Stop()
	defer b.Stop()
	code, _ := a.MyCode()
	seed := a.state.NetworkSeed
	if e := a.Start(); e != nil {
		t.Fatal(e)
	}
	if e := b.Start(); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return online(a) && online(b) })
	start := time.Now()
	if e := a.Send(b.self().ID, "Привет 👋"); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return delivered(a, "Привет 👋") })
	t.Logf("Local encrypted receipt: %s", time.Since(start))
	b.Stop()
	a.Send(b.self().ID, "offline queued")
	a.Stop()
	a2, e := NewNode(a.dir, bytes.Repeat([]byte{42}, 32))
	if e != nil {
		t.Fatal(e)
	}
	a = a2
	defer a.Stop()
	next, _ := a.MyCode()
	if next != code || a.state.NetworkSeed != seed {
		t.Fatal("identity changed")
	}
	a.Start()
	b.Start()
	eventually(t, func() bool { return delivered(a, "offline queued") })
	// Stop and recreate the real broker on the same address. Both clients must reconnect.
	addr := server.Listener.Addr().String()
	broker.Close()
	server.Close()
	eventually(t, func() bool { return !online(a) && !online(b) })
	if e = a.Send(b.self().ID, "server restarted"); e != nil {
		t.Fatal(e)
	}
	if delivered(a, "server restarted") {
		t.Fatal("server outage forged delivery")
	}
	broker = transport.NewServer()
	server = httptest.NewUnstartedServer(broker)
	listener, e := net.Listen("tcp", addr)
	if e != nil {
		t.Fatal(e)
	}
	server.Listener = listener
	server.Start()
	eventually(t, func() bool { return delivered(a, "server restarted") })
	b.mu.Lock()
	count := len(b.state.Messages)
	b.mu.Unlock()
	if count != 3 {
		t.Fatalf("messages lost/duplicated: %d", count)
	}
	// Going online again must not duplicate durable messages.
	a.NetworkChanged()
	b.NetworkChanged()
	eventually(t, func() bool { return online(a) && online(b) })
	if e = b.Send(a.self().ID, "reply"); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return delivered(b, "reply") })
}
