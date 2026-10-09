package mobile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func testNode(t *testing.T, addr string) *Node {
	t.Helper()
	n, e := NewNode(t.TempDir(), bytes.Repeat([]byte{42}, 32))
	if e != nil {
		t.Fatal(e)
	}
	n.state.Address = addr
	return n
}
func pair(t *testing.T) (*Node, *Node) {
	a, b := testNode(t, "100.64.0.1"), testNode(t, "100.64.0.2")
	ac, _ := a.MyCode()
	bc, _ := b.MyCode()
	if e := a.AddContact("Б", bc); e != nil {
		t.Fatal(e)
	}
	if e := b.AddContact("А", ac); e != nil {
		t.Fatal(e)
	}
	return a, b
}
func TestRoundTripAndDurableReceipt(t *testing.T) {
	a, b := pair(t)
	peer := b.self().ID
	if e := a.Send(peer, "Привет, iPhone 👋"); e != nil {
		t.Fatal(e)
	}
	m := a.state.Messages[0]
	ack, fresh, e := b.accept(*m.Packet)
	if e != nil || !fresh {
		t.Fatal(e)
	}
	if e = a.applyAck(ack, peer, m.ID); e != nil {
		t.Fatal(e)
	}
	if !a.state.Messages[0].Delivered || b.state.Messages[0].Text != m.Text {
		t.Fatal("delivery failed")
	}
	again, e := NewNode(b.dir, bytes.Repeat([]byte{42}, 32))
	if e != nil || len(again.state.Messages) != 1 {
		t.Fatal("receipt preceded persistence", e)
	}
}
func TestLostReceiptRetryDoesNotDuplicate(t *testing.T) {
	a, b := pair(t)
	_ = a.Send(b.self().ID, "test")
	p := *a.state.Messages[0].Packet
	_, _, _ = b.accept(p)
	_, fresh, e := b.accept(p)
	if e != nil || fresh || len(b.state.Messages) != 1 {
		t.Fatal("duplicate", e)
	}
}
func TestTamperAndUnknownSenderRejected(t *testing.T) {
	a, b := pair(t)
	_ = a.Send(b.self().ID, "secret")
	p := *a.state.Messages[0].Packet
	p.Body = append([]byte(nil), p.Body...)
	p.Body[3] ^= 1
	if _, _, e := b.accept(p); e == nil {
		t.Fatal("tamper accepted")
	}
	p = *a.state.Messages[0].Packet
	p.From = "unknown"
	if _, _, e := b.accept(p); e == nil {
		t.Fatal("unknown accepted")
	}
}
func TestReceiptCannotMarkAnotherMessageDelivered(t *testing.T) {
	a, b := pair(t)
	_ = a.Send(b.self().ID, "one")
	_ = a.Send(b.self().ID, "two")
	ack, _, _ := b.accept(*a.state.Messages[0].Packet)
	if e := a.applyAck(ack, b.self().ID, a.state.Messages[1].ID); e == nil {
		t.Fatal("wrong ack accepted")
	}
	if a.state.Messages[1].Delivered {
		t.Fatal("wrong delivered flag")
	}
}
func TestPendingSurvivesRestartAndDiskIsEncrypted(t *testing.T) {
	a, b := pair(t)
	secret := "UNIQUE-PLAINTEXT-MUST-NOT-BE-ON-DISK"
	_ = a.Send(b.self().ID, secret)
	again, e := NewNode(a.dir, bytes.Repeat([]byte{42}, 32))
	if e != nil {
		t.Fatal(e)
	}
	if len(again.state.Messages) != 1 || again.state.Messages[0].Packet == nil || again.state.Messages[0].Delivered {
		t.Fatal("outbox lost")
	}
	_ = filepath.WalkDir(a.dir, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			v, _ := os.ReadFile(p)
			if bytes.Contains(v, []byte(secret)) {
				t.Error("plaintext on disk")
			}
		}
		return nil
	})
	if _, e = NewNode(a.dir, bytes.Repeat([]byte{43}, 32)); e == nil {
		t.Fatal("wrong vault key accepted")
	}
}
func TestContactRejectsPublicInternetAddress(t *testing.T) {
	n := testNode(t, "100.64.0.1")
	c := n.self()
	c.Address = "8.8.8.8"
	if _, e := decodeContact(encodeContact(c)); e == nil {
		t.Fatal("public target accepted")
	}
}

func TestContactRejectsLowOrderKey(t *testing.T) {
	c := contact{Address: "100.64.0.1"}
	c.Key[0] = 1
	c.ID = keyID(c.Key)
	if _, e := decodeContact(encodeContact(c)); e == nil {
		t.Fatal("low-order public key accepted")
	}
}
func TestSnapshotNeverContainsPrivateKeysOrCiphertext(t *testing.T) {
	n := testNode(t, "100.64.0.1")
	var snapshot map[string]any
	if e := json.Unmarshal([]byte(n.Snapshot()), &snapshot); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"secret", "public", "storageKey"} {
		if _, ok := snapshot[key]; ok {
			t.Fatal("key exposed")
		}
	}
}
func TestFailedStorageDoesNotAcknowledge(t *testing.T) {
	a, b := pair(t)
	_ = a.Send(b.self().ID, "hello")
	b.storage.dir = filepath.Join(t.TempDir(), "does-not-exist")
	if _, _, e := b.accept(*a.state.Messages[0].Packet); e == nil {
		t.Fatal("ack on failed storage")
	}
	if len(b.state.Messages) != 0 {
		t.Fatal("failed mutation visible")
	}
}
