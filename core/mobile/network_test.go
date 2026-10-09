package mobile

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in smoke test: establishes an unregistered client, never logs in to an
// account, and never prints the one-time login URL or device keys.
func TestNetworkReachesLogin(t *testing.T) {
	if os.Getenv("BLIZKO_NETWORK_SMOKE") != "1" {
		t.Skip("opt-in network smoke")
	}
	n := testNode(t, "100.64.0.1")
	defer n.Stop()
	for attempt := 0; attempt < 2; attempt++ {
		if e := n.Start(); e != nil {
			t.Fatal("tsnet start failed", e)
		}
		deadline := time.Now().Add(35 * time.Second)
		gotLogin := false
		for time.Now().Before(deadline) {
			n.mu.Lock()
			url := n.authURL
			n.mu.Unlock()
			if strings.HasPrefix(url, "https://login.tailscale.com/") {
				gotLogin = true
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !gotLogin {
			t.Fatalf("no login URL received within timeout on startup %d", attempt+1)
		}
		n.Stop()
	}
}

func TestChangingNetworkPreservesChat(t *testing.T) {
	if os.Getenv("BLIZKO_NETWORK_SMOKE") != "1" {
		t.Skip("opt-in network smoke")
	}
	a, b := pair(t)
	if err := a.Send(b.self().ID, "Ожидающее сообщение"); err != nil {
		t.Fatal(err)
	}
	id, messageID := a.self().ID, a.state.Messages[0].ID
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	defer a.Stop()
	if err := a.ChangeNetwork(); err != nil {
		t.Fatal("new network login failed", err)
	}
	deadline := time.Now().Add(35 * time.Second)
	gotLogin := false
	for time.Now().Before(deadline) {
		a.mu.Lock()
		url := a.authURL
		a.mu.Unlock()
		if strings.HasPrefix(url, "https://login.tailscale.com/") {
			gotLogin = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !gotLogin {
		t.Fatal("new network login URL missing")
	}
	a.mu.Lock()
	if a.self().ID != id || len(a.state.Contacts) != 1 || len(a.state.Messages) != 1 || a.state.Messages[0].ID != messageID || a.state.Messages[0].Packet == nil {
		t.Error("network change lost identity, contact or outbox")
	}
	a.mu.Unlock()
}
