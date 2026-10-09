package mobile

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTailscaleMigrationPreservesHistoryKeysAndOutbox(t *testing.T) {
	a, b := pair(t)
	if err := a.Send(b.self().ID, "Keep this queued message"); err != nil {
		t.Fatal(err)
	}
	legacy := a.clone()
	legacy.Version = 2
	legacy.IrohSeed = [32]byte{}
	legacy.Address = "100.64.0.1"
	legacy.Contacts[0].Address = "100.64.0.2"
	if err := a.commit(legacy); err != nil {
		t.Fatal(err)
	}
	migrated, err := NewNode(a.dir, bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if migrated.state.Version != 3 || migrated.state.Secret != legacy.Secret || migrated.state.Public != legacy.Public || len(migrated.state.Messages) != 1 || migrated.state.Messages[0].Packet == nil {
		t.Fatal("migration lost state")
	}
	if migrated.state.Contacts[0].ID != b.self().ID || migrated.state.Contacts[0].Address != "" {
		t.Fatal("stale route retained or contact lost")
	}
	code, err := migrated.MyCode()
	if err != nil || !strings.HasPrefix(code, "blizko:3:") {
		t.Fatal("new QR unavailable", err)
	}
	restarted, err := NewNode(a.dir, bytes.Repeat([]byte{42}, 32))
	if err != nil || restarted.state.IrohSeed != migrated.state.IrohSeed || restarted.state.Address != migrated.state.Address {
		t.Fatal("network identity changed")
	}
	bc, _ := b.MyCode()
	if err := migrated.AddContact("B", bc); err != nil {
		t.Fatal(err)
	}
	if len(migrated.state.Contacts) != 1 || migrated.state.Contacts[0].Address != b.state.Address {
		t.Fatal("QR did not update existing contact")
	}
	response := b.irohPacket(migrated.state.Address, mustPacket(t, migrated.state.Messages[0].Packet))
	// The other party must update its QR too; the old endpoint identity is rejected.
	if response.Status != 403 {
		t.Fatal("old transport identity trusted after migration")
	}
	ac, _ := migrated.MyCode()
	if err := b.AddContact("A", ac); err != nil {
		t.Fatal(err)
	}
	response = b.irohPacket(migrated.state.Address, mustPacket(t, migrated.state.Messages[0].Packet))
	if response.Status != 200 || len(b.state.Messages) != 1 {
		t.Fatal("queued legacy message failed after QR refresh")
	}
}
func mustPacket(t *testing.T, packet *envelope) string {
	t.Helper()
	b, e := json.Marshal(packet)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestIrohBindsTransportIdentityToPinnedContact(t *testing.T) {
	a, b := pair(t)
	_ = a.Send(b.self().ID, "Authenticated")
	if b.irohPacket(strings.Repeat("f", 64), mustPacket(t, a.state.Messages[0].Packet)).Status != 403 {
		t.Fatal("unknown endpoint accepted")
	}
	c := testNode(t, "")
	cc, _ := c.MyCode()
	_ = b.AddContact("C", cc)
	if b.irohPacket(c.state.Address, mustPacket(t, a.state.Messages[0].Packet)).Status != 403 {
		t.Fatal("cross-contact identity accepted")
	}
	if b.irohPacket(a.state.Address, mustPacket(t, a.state.Messages[0].Packet)).Status != 200 {
		t.Fatal("authenticated message rejected")
	}
	for _, code := range []string{"blizko:2:old", "https://example.com", "blizko:3:bad"} {
		if _, err := decodeContact(code); err == nil {
			t.Fatal("bad/legacy QR accepted")
		}
	}
	if strings.Contains(a.Snapshot(), "irohSeed") {
		t.Fatal("transport secret leaked")
	}
}
