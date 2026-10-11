package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// No messages are stored on disk. A bounded queue only bridges live sockets;
// offline messages and retries remain in the sender's encrypted local outbox.
type Server struct {
	mu            sync.Mutex
	clients       map[string]*peer
	slots         chan struct{}
	closed        bool
	browserOrigin string // Explicit deployment origin; empty preserves native-only access.
}
type peer struct {
	conn    *websocket.Conn
	address string
	out     chan Frame
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewServer() *Server {
	return &Server{clients: make(map[string]*peer), slots: make(chan struct{}, 128)}
}

// NewServerWithBrowserOrigin allows one exact, trusted web application origin.
// Native clients still authenticate identically without an Origin header.
func NewServerWithBrowserOrigin(origin string) *Server {
	s := NewServer()
	s.browserOrigin = origin
	return s
}
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	for _, p := range s.clients {
		p.cancel()
		p.conn.CloseNow()
	}
	s.mu.Unlock()
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" || r.URL.Path != Path || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	// Never infer trust from Host or forwarded headers supplied by a tunnel.
	if origin := r.Header.Get("Origin"); origin != "" && (s.browserOrigin == "" || origin != s.browserOrigin) {
		http.Error(w, "browser origin not allowed", 403)
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		http.Error(w, "busy", 503)
		return
	}
	// The complete origin (scheme + host + port) was checked above, not a glob.
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled, InsecureSkipVerify: true})
	if e != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(MaxFrame)
	handshake, finish := context.WithTimeout(r.Context(), 10*time.Second)
	defer finish()
	nonce := make([]byte, 32)
	if _, e = rand.Read(nonce); e != nil {
		return
	}
	challenge := hex.EncodeToString(nonce)
	if writeJSON(handshake, c, hello{Type: "challenge", Challenge: challenge}) != nil {
		return
	}
	var auth hello
	if readJSON(handshake, c, &auth) != nil || auth.Type != "auth" || !validHex(auth.Address, 32) {
		return
	}
	pub, _ := hex.DecodeString(auth.Address)
	sig, e := base64.RawStdEncoding.DecodeString(auth.Signature)
	if e != nil || !ed25519.Verify(pub, authBytes(challenge, auth.Address), sig) {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	p := &peer{conn: c, address: auth.Address, out: make(chan Frame, 8), ctx: ctx, cancel: cancel}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	old := s.clients[p.address]
	s.clients[p.address] = p
	s.mu.Unlock()
	if old != nil {
		old.cancel()
		old.conn.CloseNow()
	}
	defer func() {
		s.mu.Lock()
		if s.clients[p.address] == p {
			delete(s.clients, p.address)
		}
		s.mu.Unlock()
	}()
	if writeJSON(handshake, c, hello{Type: "ready"}) != nil {
		return
	}
	finish()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer c.CloseNow()
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-p.out:
				if write(ctx, c, f) != nil {
					return
				}
			}
		}
	}()
	defer func() { cancel(); c.CloseNow(); <-done }()
	allowance := 40.0
	last := time.Now()
	for {
		var f Frame
		if readJSON(ctx, c, &f) != nil {
			return
		}
		now := time.Now()
		allowance += now.Sub(last).Seconds() * 20
		if allowance > 40 {
			allowance = 40
		}
		last = now
		if allowance < 1 {
			return
		}
		allowance--
		if f.From != p.address || !f.Valid() {
			return
		}
		s.mu.Lock()
		destination := s.clients[f.To]
		s.mu.Unlock()
		if destination != nil {
			select {
			case destination.out <- f:
				continue
			default:
			}
		}
		// This hint is deliberately unauthenticated and only triggers a retry. It
		// must never acknowledge delivery or permanently reject a message.
		if f.Type == "request" {
			hint := Frame{Type: "unavailable", ID: f.ID, From: f.To, To: f.From}
			select {
			case p.out <- hint:
			default:
				return
			}
		}
	}
}
