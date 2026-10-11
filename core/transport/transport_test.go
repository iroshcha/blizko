package transport

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func seed(n byte) (s [32]byte) { s[0] = n; return }
func TestExplicitBrowserOrigin(t *testing.T) {
	broker := NewServerWithBrowserOrigin("https://chat.example")
	defer broker.Close()
	host := httptest.NewServer(broker)
	defer host.Close()
	for _, origin := range []string{"https://evil.test", "http://chat.example", "https://chat.example.evil", "null", "https://chat.example:443"} {
		r, _ := http.NewRequest("GET", host.URL+Path, nil)
		r.Header.Set("Origin", origin)
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatal("origin allowed", origin)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	c, _, err := websocket.Dial(ctx, host.URL+Path, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://chat.example"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	var h hello
	if err = readJSON(ctx, c, &h); err != nil || h.Type != "challenge" {
		t.Fatal("trusted browser did not receive challenge", err)
	}
}
func TestSignaturesCannotChangePeerPayloadOrReceipt(t *testing.T) {
	s := seed(1)
	key := ed25519.NewKeyFromSeed(s[:])
	f := Frame{Type: "request", ID: ID(), From: Address(key), To: strings.Repeat("a", 64), Data: "encrypted packet"}
	f.Sign(key)
	if !f.Valid() {
		t.Fatal("valid signature")
	}
	for _, mutate := range []func(*Frame){func(x *Frame) { x.Data = "tampered" }, func(x *Frame) { x.To = strings.Repeat("b", 64) }, func(x *Frame) { x.ID = ID() }, func(x *Frame) { x.From = strings.Repeat("c", 64) }} {
		copy := f
		mutate(&copy)
		if copy.Valid() {
			t.Fatal("tamper accepted")
		}
	}
	f.Type = "response"
	f.Data = ""
	f.Result = &Response{Status: 200, Data: "encrypted ack"}
	f.Sign(key)
	if !f.Valid() {
		t.Fatal("valid receipt")
	}
	f.Result.Data = "forged ack"
	if f.Valid() {
		t.Fatal("forged receipt accepted")
	}
}
func connectClient(t *testing.T, base string, n byte, handler func(string, string) Response) *Client {
	t.Helper()
	ready := make(chan struct{}, 8)
	c := NewClient(seed(n), base, handler, func(up bool) {
		if up {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.Run(ctx)
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("not connected")
	}
	return c
}
func TestBidirectionalRequests(t *testing.T) {
	broker := NewServer()
	defer broker.Close()
	host := httptest.NewServer(broker)
	defer host.Close()
	handler := func(from, data string) Response { return Response{Status: 200, Data: from + ":" + data} }
	a := connectClient(t, host.URL, 1, handler)
	b := connectClient(t, host.URL, 2, handler)
	for i := 0; i < 5; i++ {
		answer, e := a.Exchange(context.Background(), b.identity, "Привет 👋")
		if e != nil || answer.Data != a.identity+":Привет 👋" {
			t.Fatal(answer, e)
		}
	}
	got, e := b.Exchange(context.Background(), a.identity, "reverse")
	if e != nil || got.Data != b.identity+":reverse" {
		t.Fatal(got, e)
	}
	if _, e = a.Exchange(context.Background(), strings.Repeat("f", 64), "offline"); e == nil {
		t.Fatal("offline acknowledged")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = a.Exchange(ctx, b.identity, "cancelled"); e == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestBrokerRejectsImpersonationAndReplayedLogin(t *testing.T) {
	broker := NewServer()
	defer broker.Close()
	host := httptest.NewServer(broker)
	defer host.Close()
	u, _ := ServerURL(host.URL)
	c, _, e := websocket.Dial(context.Background(), u, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	var h hello
	if readJSON(context.Background(), c, &h) != nil {
		t.Fatal("challenge")
	}
	key := ed25519.NewKeyFromSeed(make([]byte, 32))
	forged := hello{Type: "auth", Address: Address(key), Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, authBytes(strings.Repeat("0", 64), Address(key))))}
	write(context.Background(), c, forged)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if readJSON(ctx, c, &h) == nil {
		t.Fatal("replayed challenge accepted")
	}
}
func TestRequestCancellation(t *testing.T) {
	broker := NewServer()
	defer broker.Close()
	host := httptest.NewServer(broker)
	defer host.Close()
	var calls atomic.Int32
	a := connectClient(t, host.URL, 1, func(string, string) Response { return Response{Status: 200} })
	b := connectClient(t, host.URL, 2, func(string, string) Response {
		calls.Add(1)
		time.Sleep(100 * time.Millisecond)
		return Response{Status: 200}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := a.Exchange(ctx, b.identity, "slow"); e == nil {
		t.Fatal("deadline ignored")
	}
	time.Sleep(120 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatal("request not dispatched")
	}
	if _, e := a.Exchange(context.Background(), b.identity, "next"); e != nil {
		t.Fatal("cancel damaged subsequent delivery", e)
	}
}
func TestURLAndBrowserOrigin(t *testing.T) {
	for _, u := range []string{"http://example.com", "https://user@example.com", "https://example.com/path", "https://example.com/?key=x", "https://example.com/#x"} {
		if _, e := ServerURL(u); e == nil {
			t.Fatal("unsafe endpoint", u)
		}
	}
	r := httptest.NewRequest("GET", Path, nil)
	r.Header.Set("Origin", "https://evil.test")
	w := httptest.NewRecorder()
	NewServer().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("browser accepted")
	}
}

func TestConcurrentExchanges(t *testing.T) {
	broker := NewServer()
	defer broker.Close()
	host := httptest.NewServer(broker)
	defer host.Close()
	handler := func(from, data string) Response { return Response{Status: 200, Data: from + ":" + data} }
	a := connectClient(t, host.URL, 1, handler)
	b := connectClient(t, host.URL, 2, handler)
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			from, to := a, b
			if i%2 == 1 {
				from, to = b, a
			}
			data := fmt.Sprint(i)
			r, e := from.Exchange(context.Background(), to.identity, data)
			if e == nil && r.Data != from.identity+":"+data {
				e = fmt.Errorf("response mixed up")
			}
			results <- e
		}(i)
	}
	for i := 0; i < 8; i++ {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
}
func TestClientRejectsBrokerForgedFrames(t *testing.T) {
	peerSeed := seed(2)
	peerKey := ed25519.NewKeyFromSeed(peerSeed[:])
	badSeed := seed(3)
	badKey := ed25519.NewKeyFromSeed(badSeed[:])
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(MaxFrame)
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		write(ctx, c, hello{Type: "challenge", Challenge: strings.Repeat("a", 64)})
		var login hello
		if readJSON(ctx, c, &login) != nil {
			return
		}
		write(ctx, c, hello{Type: "ready"})
		var request Frame
		if readJSON(ctx, c, &request) != nil {
			return
		}
		forged := Frame{Type: "request", ID: ID(), From: Address(peerKey), To: request.From, Data: "forged request"}
		forged.Sign(badKey)
		write(ctx, c, forged)
		forged = Frame{Type: "response", ID: request.ID, From: Address(peerKey), To: request.From, Result: &Response{Status: 200, Data: "forged ack"}}
		forged.Sign(badKey)
		write(ctx, c, forged)
		forged.Sign(peerKey)
		forged.ID = ID()
		forged.Sign(peerKey)
		write(ctx, c, forged)
		time.Sleep(50 * time.Millisecond)
		good := Frame{Type: "response", ID: request.ID, From: Address(peerKey), To: request.From, Result: &Response{Status: 200, Data: "verified ack"}}
		good.Sign(peerKey)
		write(ctx, c, good)
		// Keep the socket alive until the test releases it.
		var ignored Frame
		_ = readJSON(ctx, c, &ignored)
	}))
	defer host.Close()
	var invoked atomic.Int32
	a := connectClient(t, host.URL, 1, func(string, string) Response { invoked.Add(1); return Response{Status: 200} })
	result, e := a.Exchange(context.Background(), Address(peerKey), "encrypted message")
	if e != nil || result.Data != "verified ack" || invoked.Load() != 0 {
		t.Fatal("forged broker data accepted", result, e, invoked.Load())
	}
}
