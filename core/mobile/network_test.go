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
	if e := n.Start(); e != nil {
		t.Fatal("tsnet start failed", e)
	}
	defer n.Stop()
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		url := n.authURL
		n.mu.Unlock()
		if strings.HasPrefix(url, "https://login.tailscale.com/") {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("no login URL received within timeout")
}
