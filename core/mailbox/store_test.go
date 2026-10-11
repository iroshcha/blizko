package mailbox

import (
	"blizko/core/transport"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	webpush "github.com/SherClockHolmes/webpush-go"
	bolt "go.etcd.io/bbolt"
	"strings"
	"testing"
	"time"
)

func identity() ed25519.PrivateKey { _, k, _ := ed25519.GenerateKey(rand.Reader); return k }
func packet(k ed25519.PrivateKey, to string) transport.Frame {
	raw, _ := json.Marshal(map[string]any{"v": 2, "from": strings.Repeat("a", 64), "nonce": make([]byte, 24), "body": []byte(transport.ID())})
	f := transport.Frame{Type: "request", ID: transport.ID(), From: transport.Address(k), To: to, Data: string(raw)}
	f.Sign(k)
	return f
}
func receipt(f transport.Frame, k ed25519.PrivateKey) transport.Frame {
	r := transport.Frame{Type: "response", ID: f.ID, From: f.To, To: f.From, Result: &transport.Response{Status: 200, Data: packet(k, f.From).Data}}
	r.Sign(k)
	return r
}
func control(s *Store, k ed25519.PrivateKey, req any) transport.Response {
	b, _ := json.Marshal(req)
	f := transport.Frame{Type: "request", ID: transport.ID(), From: transport.Address(k), To: transport.ControlAddress, Data: string(b)}
	f.Sign(k)
	return s.Control(f)
}
func mustStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(t.TempDir(), "https://example.com")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDurableReceiptRestartAndDedup(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir, "")
	if e != nil {
		t.Fatal(e)
	}
	a, b := identity(), identity()
	f := packet(a, transport.Address(b))
	o, e := s.Offer(f)
	if e != nil || !o.Stored {
		t.Fatal(o, e)
	}
	dup := f
	dup.ID = transport.ID()
	dup.Sign(a)
	o, e = s.Offer(dup)
	if e != nil || o.Frame.ID != f.ID {
		t.Fatal("dedup changed original signature", e)
	}
	public := s.public
	s.Close()
	s, e = Open(dir, "")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if public != s.public {
		t.Fatal("VAPID identity changed")
	}
	pending := s.Pending(f.To, 8)
	if len(pending) != 1 || !pending[0].Valid() {
		t.Fatal("lost signed packet")
	}
	bad := receipt(f, identity())
	s.SaveReceipt(bad)
	if len(s.Pending(f.To, 8)) != 1 {
		t.Fatal("forged receipt deleted data")
	}
	r := receipt(f, b)
	if e = s.SaveReceipt(r); e != nil {
		t.Fatal(e)
	}
	if len(s.Pending(f.To, 8)) != 0 || len(s.Pending(f.From, 8)) != 1 {
		t.Fatal("receipt routing")
	}
	s.db.View(func(tx *bolt.Tx) error {
		var v record
		json.Unmarshal(tx.Bucket(records).Get(key(f)), &v)
		if v.Request.Data != "" {
			t.Error("message ciphertext retained after receipt")
		}
		return nil
	})
	control(s, b, map[string]string{"op": "receipt-done", "id": f.ID, "sender": f.To})
	if len(s.Pending(f.From, 8)) != 1 {
		t.Fatal("wrong identity consumed receipt")
	}
	if control(s, a, map[string]string{"op": "receipt-done", "id": f.ID, "sender": f.To}).Status != 200 {
		t.Fatal("ack control")
	}
	if len(s.Pending(f.From, 8)) != 0 {
		t.Fatal("completed receipt repeated")
	}
	o, e = s.Offer(dup)
	if e != nil || o.Frame.Type != "response" || !o.Frame.Valid() {
		t.Fatal("lost cached proof")
	}
}
func TestExpiryQuotaAndFailure(t *testing.T) {
	s := mustStore(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	a, b := identity(), identity()
	for i := 0; i < maxPerDevice; i++ {
		if _, e := s.Offer(packet(a, transport.Address(b))); e != nil {
			t.Fatal(i, e)
		}
	}
	if _, e := s.Offer(packet(a, transport.Address(b))); e == nil {
		t.Fatal("quota bypass")
	}
	now = now.Add(retention + time.Second)
	if len(s.Pending(transport.Address(b), 8)) != 0 {
		t.Fatal("expired packet replayed")
	}
	if e := s.Prune(); e != nil {
		t.Fatal(e)
	}
	f := packet(a, transport.Address(b))
	if _, e := s.Offer(f); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if o, e := s.Offer(packet(a, f.To)); e == nil || o.Stored {
		t.Fatal("closed DB claimed storage")
	}
}
func TestReplayDoesNotStarveLaterPackets(t *testing.T) {
	s := mustStore(t)
	a, b := identity(), identity()
	seen := map[string]bool{}
	for i := 0; i < 25; i++ {
		s.Offer(packet(a, transport.Address(b)))
	}
	for i := 0; i < 4; i++ {
		for _, f := range s.Pending(transport.Address(b), 8) {
			seen[f.ID] = true
		}
	}
	if len(seen) != 25 {
		t.Fatal(len(seen))
	}
}
func testSubscription(t *testing.T) *subscription {
	k, e := ecdh.P256().GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	return &subscription{Endpoint: "https://web.push.apple.com/Q-test-only", Keys: webpush.Keys{Auth: base64.RawURLEncoding.EncodeToString(make([]byte, 16)), P256dh: base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())}}
}
func TestPushPrivacyRateRetryAndRevocation(t *testing.T) {
	s := mustStore(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	a, b, unknown := identity(), identity(), identity()
	sub := testSubscription(t)
	for _, url := range []string{"http://web.push.apple.com/q", "https://127.0.0.1/q", "https://web.push.apple.com.evil.test/q", "https://user@web.push.apple.com/q", "https://web.push.apple.com:8080/q"} {
		bad := *sub
		bad.Endpoint = url
		if validSubscription(&bad) {
			t.Fatal("SSRF", url)
		}
	}
	if r := s.setSubscription(transport.Address(b), sub, []string{transport.Address(a)}); r.Status != 200 {
		t.Fatal(r)
	}
	calls := 0
	status := 201
	fail := false
	s.sendPush = func(*subscription) (int, error) {
		calls++
		if fail {
			return 0, errors.New("network")
		}
		return status, nil
	}
	s.Offer(packet(unknown, transport.Address(b)))
	now = now.Add(6 * time.Second)
	s.pushOnce()
	if calls != 0 {
		t.Fatal("unknown sender triggered notification")
	}
	s.Offer(packet(a, transport.Address(b)))
	s.pushOnce()
	if calls != 0 {
		t.Fatal("no foreground grace period")
	}
	now = now.Add(6 * time.Second)
	s.pushOnce()
	s.pushOnce()
	if calls != 1 {
		t.Fatal("duplicate push", calls)
	}
	s.Offer(packet(a, transport.Address(b)))
	now = now.Add(10 * time.Second)
	s.pushOnce()
	if calls != 1 {
		t.Fatal("rate limit")
	}
	now = now.Add(time.Minute)
	fail = true
	s.pushOnce()
	if calls != 2 {
		t.Fatal("missing attempt")
	}
	now = now.Add(time.Minute)
	fail = false
	s.pushOnce()
	if calls != 3 {
		t.Fatal("missing retry")
	}
	now = now.Add(time.Minute)
	s.testPush(transport.Address(b))
	status = 410
	s.pushOnce()
	var state map[string]any
	json.Unmarshal([]byte(s.pushStatus(transport.Address(b)).Data), &state)
	if state["registered"] != false {
		t.Fatal("expired subscription retained")
	}
}
