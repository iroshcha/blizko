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
