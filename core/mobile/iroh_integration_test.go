//go:build iroh

package mobile

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestIrohRealRelayDeliveryAndRestart(t *testing.T) {
	if os.Getenv("BLIZKO_NETWORK_SMOKE") != "1" {
		t.Skip("opt-in real public-relay test")
	}
	a, b := pair(t)
	for _, n := range []*Node{a, b} {
		if err := n.SetRelayOnly(true); err != nil {
			t.Fatal(err)
		}
		if err := n.Start(); err != nil {
			t.Fatal(err)
		}
		defer n.Stop()
	}
	wait := func(label string, condition func() bool) {
		t.Helper()
		until := time.Now().Add(60 * time.Second)
		for time.Now().Before(until) {
			if condition() {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatal(label)
	}
	delivered := func(n *Node, text string) bool {
		n.mu.Lock()
		defer n.mu.Unlock()
		for _, m := range n.state.Messages {
			if m.Text == text && m.Out && m.Delivered {
				return true
			}
		}
		return false
	}
	if err := a.Send(b.self().ID, "relay A to B"); err != nil {
		t.Fatal(err)
	}
	if err := b.Send(a.self().ID, "relay B to A"); err != nil {
		t.Fatal(err)
	}
	wait("relay receipt A", func() bool { return delivered(a, "relay A to B") })
	wait("relay receipt B", func() bool { return delivered(b, "relay B to A") })
	b.Stop()
	if err := a.Send(b.self().ID, "after reconnect"); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	wait("queued delivery after reconnect", func() bool { return delivered(a, "after reconnect") })
	var state map[string]any
	_ = json.Unmarshal([]byte(a.Snapshot()), &state)
	if state["transport"] != "iroh" || state["relayOnly"] != true {
		t.Fatal("wrong transport tested")
	}
}
