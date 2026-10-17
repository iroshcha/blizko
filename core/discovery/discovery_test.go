package discovery

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestSignedDirectory(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	r := Record{1, "https://relay.example.com/", now.UnixMilli(), now.Add(7 * 24 * time.Hour).UnixMilli()}
	raw, _ := Sign(r, key)
	got, e := Verify(raw, pub, now)
	if e != nil || got != r {
		t.Fatal(got, e)
	}
	raw[len(raw)-5] ^= 1
	if _, e = Verify(raw, pub, now); e == nil {
		t.Fatal("tampered signature accepted")
	}
	for _, u := range []string{"http://relay.example.com/", "https://user@relay.example.com/", "https://127.0.0.1/", "https://relay.example.com/?x=1", "https://relay.example.com/path", "https://relay.example.com:443/"} {
		r.URL = u
		raw, _ = Sign(r, key)
		if _, e = Verify(raw, pub, now); e == nil {
			t.Fatal("invalid URL accepted", u)
		}
	}
	r.URL = "https://relay.example.com/"
	r.Expires = now.Add(-time.Second).UnixMilli()
	raw, _ = Sign(r, key)
	if _, e = Verify(raw, pub, now); e == nil {
		t.Fatal("expired directory accepted")
	}
	r.Expires = now.Add(9 * 24 * time.Hour).UnixMilli()
	raw, _ = Sign(r, key)
	if _, e = Verify(raw, pub, now); e == nil {
		t.Fatal("excessive lifetime accepted")
	}
}
func TestManagedAliases(t *testing.T) {
	for _, u := range []string{"", FallbackURL, "https://ample-raven-6363.ru.tuna.am/"} {
		if !Managed(u) {
			t.Fatal(u)
		}
	}
	if Managed("https://my-own-server.example/") {
		t.Fatal("custom server silently overridden")
	}
}
