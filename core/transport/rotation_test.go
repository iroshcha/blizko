package transport

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRelayRotationPreservesIdentity(t *testing.T) {
	a, b := NewServer(), NewServer()
	defer a.Close()
	defer b.Close()
	first, second := httptest.NewServer(a), httptest.NewServer(b)
	defer first.Close()
	defer second.Close()
	c := connectClient(t, first.URL, 90, func(string, string) Response { return Response{Status: 200} })
	identity := c.identity
	key := string(c.key)
	c.Resolve = func(context.Context) (string, error) { return second.URL, nil }
	c.refreshAddress(context.Background())
	peer := connectClient(t, second.URL, 91, func(string, string) Response { return Response{Status: 200, Data: "received"} })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := c.Exchange(context.Background(), peer.identity, "packet")
		if err == nil && r.Data == "received" {
			if c.identity != identity || string(c.key) != key {
				t.Fatal("profile replaced")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("did not reconnect to changed relay")
}
