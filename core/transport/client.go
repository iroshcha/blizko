package transport

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type pending struct {
	peer  string
	reply chan Frame
}
type session struct {
	conn    *websocket.Conn
	done    chan struct{}
	mu      sync.Mutex
	pending map[string]pending
}
type Client struct {
	key      ed25519.PrivateKey
	base     string
	identity string
	Handle   func(string, string) Response
	State    func(bool)
	Receipt  func(string, string) bool // True only after an encrypted peer receipt is persisted.
	mu       sync.Mutex
	current  *session
	wake     chan struct{}
}

func NewClient(seed [32]byte, base string, handler func(string, string) Response, state func(bool)) *Client {
	key := ed25519.NewKeyFromSeed(seed[:])
	return &Client{key: key, identity: Address(key), base: base, Handle: handler, State: state, wake: make(chan struct{}, 1)}
}
func (c *Client) Reconnect() {
	c.mu.Lock()
	s := c.current
	c.mu.Unlock()
	if s != nil {
		s.conn.CloseNow()
	}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}
func (c *Client) Run(ctx context.Context) {
	delay := 250 * time.Millisecond
	for ctx.Err() == nil {
		socket, e := dial(ctx, c.base, c.key)
		if e == nil {
			delay = 250 * time.Millisecond
			s := &session{conn: socket, done: make(chan struct{}), pending: make(map[string]pending)}
			c.mu.Lock()
			c.current = s
			c.mu.Unlock()
			if c.State != nil {
				c.State(true)
			}
			c.serve(ctx, s)
			c.mu.Lock()
			if c.current == s {
				c.current = nil
			}
			c.mu.Unlock()
			close(s.done)
			socket.CloseNow()
		}
		if c.State != nil {
			c.State(false)
		}
		timer := time.NewTimer(delay + time.Duration(rand.IntN(250))*time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-c.wake:
			timer.Stop()
			delay = 250 * time.Millisecond
		case <-timer.C:
			if delay < 15*time.Second {
				delay *= 2
				if delay > 15*time.Second {
					delay = 15 * time.Second
				}
			}
		}
	}
}
func (c *Client) serve(ctx context.Context, s *session) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { s.conn.CloseNow() })
	defer stop()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ping, end := context.WithTimeout(ctx, 10*time.Second)
				e := s.conn.Ping(ping)
				end()
				if e != nil {
					s.conn.CloseNow()
					return
				}
			}
		}
	}()
	workers := make(chan struct{}, 8)
	var working sync.WaitGroup
	defer func() { cancel(); working.Wait() }()
	// Suppress only concurrent duplicates: a failed durable write must be retryable.
	var seen sync.Map
	for {
		var f Frame
		if readJSON(ctx, s.conn, &f) != nil {
			return
		}
		if f.To != c.identity {
			continue
		}
		if f.Type == "server" || f.Type == "stored" {
			s.mu.Lock()
			p, ok := s.pending[f.ID]
			s.mu.Unlock()
			if ok && p.peer == f.From && ((f.Type == "server" && p.peer == ControlAddress && f.Result != nil) || (f.Type == "stored" && p.peer != ControlAddress)) {
				select {
				case p.reply <- f:
				default:
				}
			}
			continue
		}
		if f.Type == "unavailable" {
			s.mu.Lock()
			p, ok := s.pending[f.ID]
			s.mu.Unlock()
			if ok && p.peer == f.From {
				select {
				case p.reply <- f:
				default:
				}
			}
			continue
		}
		if !f.Valid() {
			continue
		}
		if f.Type == "response" {
			if c.Receipt != nil && f.Result.Status == 200 && f.Result.Data != "" {
				select {
				case workers <- struct{}{}:
					working.Add(1)
					go func(f Frame) {
						defer working.Done()
						defer func() { <-workers }()
						if c.Receipt(f.From, f.Result.Data) {
							data, _ := json.Marshal(map[string]string{"op": "receipt-done", "sender": f.From, "id": f.ID})
							ack := Frame{Type: "request", ID: ID(), From: c.identity, To: ControlAddress, Data: string(data)}
							ack.Sign(c.key)
							_ = write(ctx, s.conn, ack)
						}
					}(f)
				default:
				}
			}
			s.mu.Lock()
			p, ok := s.pending[f.ID]
			s.mu.Unlock()
			if ok && p.peer == f.From {
				select {
				case p.reply <- f:
				default:
				}
			}
			continue
		}
		key := f.From + ":" + f.ID
		if _, ok := seen.LoadOrStore(key, struct{}{}); ok {
			continue
		}
		select {
		case workers <- struct{}{}:
		default:
			seen.Delete(key)
			continue
		}
		working.Add(1)
		go func(f Frame) {
			defer seen.Delete(f.From + ":" + f.ID)
			defer working.Done()
			defer func() { <-workers }()
			result := c.Handle(f.From, f.Data)
			reply := Frame{Type: "response", ID: f.ID, From: c.identity, To: f.From, Result: &result}
			reply.Sign(c.key)
			_ = write(ctx, s.conn, reply)
		}(f)
	}
}
func (c *Client) Exchange(ctx context.Context, peer, data string) (Response, error) {
	if !validHex(peer, 32) || len(data) > MaxPacket {
		return Response{}, ErrOffline
	}
	c.mu.Lock()
	s := c.current
	c.mu.Unlock()
	if s == nil {
		return Response{}, ErrOffline
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	f := Frame{Type: "request", ID: ID(), From: c.identity, To: peer, Data: data}
	f.Sign(c.key)
	p := pending{peer: peer, reply: make(chan Frame, 1)}
	s.mu.Lock()
	if len(s.pending) >= 16 {
		s.mu.Unlock()
		return Response{}, ErrOffline
	}
	s.pending[f.ID] = p
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, f.ID); s.mu.Unlock() }()
	if e := write(ctx, s.conn, f); e != nil {
		return Response{}, e
	}
	select {
	case <-ctx.Done():
		return Response{}, ctx.Err()
	case <-s.done:
		return Response{}, ErrOffline
	case response := <-p.reply:
		if response.Type == "stored" {
			return Response{Status: 202, Code: "stored", Data: response.Data}, nil
		}
		if response.Type == "unavailable" {
			return Response{}, ErrOffline
		}
		return *response.Result, nil
	}
}
