package mobile

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

func TestInvitationQRRequiresTrustedURLAndKeepsGrantPrivate(t *testing.T) {
	a, b := pair(t)
	a.state.DNSName = "blizko-a.example.ts.net"
	link := "https://login.tailscale.com/admin/invite/test-grant_A1"
	if err := a.SetInvitation(" " + link + "\n"); err != nil {
		t.Fatal(err)
	}
	code, err := a.MyCode()
	if err != nil {
		t.Fatal(err)
	}
	invite, err := ContactInvitation(code)
	if err != nil || invite != link {
		t.Fatal("invitation was not included in QR", err)
	}
	if err := b.AddContact("Updated QR", code); err != nil {
		t.Fatal(err)
	}
	c, _ := b.peer(a.self().ID)
	if c.Invite != "" || c.DNSName != a.state.DNSName {
		t.Fatal("received grant retained or DNS lost")
	}
	if err := a.Send(b.self().ID, "Keep history"); err != nil {
		t.Fatal(err)
	}
	restored, err := NewNode(a.dir, bytes.Repeat([]byte{42}, 32))
	if err != nil || restored.state.Invite != link || len(restored.state.Messages) != 1 {
		t.Fatal("setting/history did not survive restart", err)
	}
	if err := filepath.WalkDir(a.dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if bytes.Contains(data, []byte(link)) {
			t.Error("share invitation stored as plaintext")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetInvitation(""); err != nil {
		t.Fatal(err)
	}
	if len(a.state.Messages) != 1 || a.state.Invite != "" {
		t.Fatal("clearing invitation damaged history")
	}
	if err := testNode(t, "").SetInvitation(link); err == nil {
		t.Fatal("grant accepted before device login")
	}
}

func TestInvitationRejectsUntrustedAndMalformedQR(t *testing.T) {
	n := testNode(t, "100.64.0.1")
	for _, link := range []string{
		"http://login.tailscale.com/admin/invite/token",
		"https://login.tailscale.com.evil.example/admin/invite/token",
		"https://login.tailscale.com@evil.example/admin/invite/token",
		"https://evil.example@console.tailscale.com/admin/invite/token",
		"https://console.tailscale.com:8443/admin/invite/token",
		"https://console.tailscale.com/admin/machines",
		"https://console.tailscale.com/admin/invite/",
		"https://console.tailscale.com/admin/invite/../machines",
		"https://console.tailscale.com/admin/invite/token#fragment",
		"https://console.tailscale.com/admin/invite/token?redirect=https://evil.example",
		"https://console.tailscale.com/admin/invite/token%2fextra",
	} {
		c := n.self()
		c.Invite = link
		if _, err := ContactInvitation(encodeContact(c)); err == nil {
			t.Errorf("untrusted URL accepted: %s", link)
		}
		if err := n.SetInvitation(link); err == nil {
			t.Error("owner setting accepted invalid invitation")
		}
	}
	for _, host := range []string{"login.tailscale.com", "console.tailscale.com"} {
		if !validInvitation("https://" + host + "/admin/invite/valid_TOKEN-123") {
			t.Fatal("official invitation rejected")
		}
	}
	c := n.self()
	c.ID = "forged"
	c.Invite = "https://console.tailscale.com/admin/invite/token"
	if _, err := ContactInvitation(encodeContact(c)); err == nil {
		t.Fatal("invitation extracted from invalid contact key")
	}
}

func TestSharedRouteUsesAuthorizedPeerDNSWithRemappedIP(t *testing.T) {
	a, b := pair(t)
	b.state.DNSName = "blizko-b.other.ts.net"
	code, _ := b.MyCode()
	if err := a.AddContact("B", code); err != nil {
		t.Fatal(err)
	}
	old := b.state.Address
	b.state.Address = "100.100.100.10"
	st := &ipnstate.Status{Peer: map[key.NodePublic]*ipnstate.PeerStatus{
		key.NewNode().Public(): {DNSName: b.state.DNSName + ".", TailscaleIPs: []netip.Addr{netip.MustParseAddr(b.state.Address)}, Online: true},
		key.NewNode().Public(): {DNSName: "wrong.same.ts.net.", TailscaleIPs: []netip.Addr{netip.MustParseAddr(old)}, Online: true},
		key.NewNode().Public(): {DNSName: "public.other.ts.net.", TailscaleIPs: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, Online: true},
	}}
	c, _ := a.peer(b.self().ID)
	address, online := peerVisible(st, c)
	if address != b.state.Address || !online {
		t.Fatal("selected stale/conflicting peer address")
	}
	routes := sharedRoutes(st)
	if routes["public.other.ts.net"] != "" {
		t.Fatal("public address admitted to peer routes")
	}
	if err := a.Send(c.ID, "Shared node"); err != nil {
		t.Fatal(err)
	}
	a.flushRoutes(context.Background(), receiverClient(t, b, b.messageHandler()), routes)
	if !a.state.Messages[0].Delivered || len(b.state.Messages) != 1 {
		t.Fatal("remapped route failed to deliver")
	}
	if a.state.Contacts[0].Address != old {
		t.Fatal("runtime route replaced original QR address")
	}
}
