// Package transport carries opaque application packets through one configured server.
// Device signatures authenticate both peers independently of the forwarding server.
package transport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const Path = "/blizko/v1/socket"
const MaxFrame = 48000
const MaxPacket = 20000
const protocol = "blizko-server-v1"

var ErrOffline = errors.New("server or peer unavailable")

type Response struct {
	Status int    `json:"status"`
	Code   string `json:"code,omitempty"`
	Data   string `json:"data,omitempty"`
}
type Frame struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Data      string    `json:"data,omitempty"`
	Result    *Response `json:"result,omitempty"`
	Signature string    `json:"signature,omitempty"`
}
type hello struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge,omitempty"`
	Address   string `json:"address,omitempty"`
	Signature string `json:"signature,omitempty"`
}

func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func Address(key ed25519.PrivateKey) string {
	return hex.EncodeToString(key.Public().(ed25519.PublicKey))
}
func validHex(s string, n int) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == n && strings.ToLower(s) == s
}
func (f Frame) unsigned() []byte {
	f.Signature = ""
	b, _ := json.Marshal(f)
	return append([]byte(protocol+"/frame\x00"), b...)
}
func (f *Frame) Sign(key ed25519.PrivateKey) {
	f.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, f.unsigned()))
}
func (f Frame) Valid() bool {
	if !validHex(f.ID, 16) || !validHex(f.From, 32) || !validHex(f.To, 32) {
		return false
	}
	if f.Type == "request" {
		if len(f.Data) > MaxPacket || f.Result != nil {
			return false
		}
	} else if f.Type == "response" {
		if f.Data != "" || f.Result == nil || f.Result.Status < 200 || f.Result.Status > 599 || len(f.Result.Code) > 64 || len(f.Result.Data) > MaxPacket {
			return false
		}
	} else {
		return false
	}
	key, _ := hex.DecodeString(f.From)
	sig, e := base64.RawStdEncoding.DecodeString(f.Signature)
	return e == nil && ed25519.Verify(key, f.unsigned(), sig)
}
func authBytes(challenge, address string) []byte {
	return []byte(protocol + "/login\x00" + challenge + "\x00" + address)
}
func readJSON(ctx context.Context, c *websocket.Conn, value any) error {
	kind, raw, e := c.Read(ctx)
	if e != nil {
		return e
	}
	if kind != websocket.MessageText {
		return errors.New("text frame required")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e = d.Decode(value); e != nil {
		return e
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}
func writeJSON(ctx context.Context, c *websocket.Conn, value any) error {
	b, e := json.Marshal(value)
	if e != nil || len(b) > MaxFrame {
		return errors.New("oversized frame")
	}
	return c.Write(ctx, websocket.MessageText, b)
}
func write(ctx context.Context, c *websocket.Conn, value any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return writeJSON(ctx, c, value)
}

// HTTP is allowed only on literal loopback for isolated tests. No redirects or proxy
// environment variables can silently move a client to a different server.
func ServerURL(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("invalid server URL")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", errors.New("invalid server port")
		}
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return "", errors.New("HTTPS required")
		}
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = Path
	return u.String(), nil
}
func dial(ctx context.Context, base string, key ed25519.PrivateKey) (*websocket.Conn, error) {
	address, e := ServerURL(base)
	if e != nil {
		return nil, e
	}
	tr := &http.Transport{DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second, ForceAttemptHTTP2: false}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	handshake, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, _, e := websocket.Dial(handshake, address, &websocket.DialOptions{HTTPClient: hc, CompressionMode: websocket.CompressionDisabled})
	if e != nil {
		return nil, e
	}
	c.SetReadLimit(MaxFrame)
	ok := false
	defer func() {
		if !ok {
			c.CloseNow()
		}
	}()
	var challenge hello
	if e = readJSON(handshake, c, &challenge); e != nil {
		return nil, e
	}
	if challenge.Type != "challenge" || !validHex(challenge.Challenge, 32) {
		return nil, errors.New("invalid challenge")
	}
	identity := Address(key)
	auth := hello{Type: "auth", Address: identity, Signature: base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, authBytes(challenge.Challenge, identity)))}
	if e = writeJSON(handshake, c, auth); e != nil {
		return nil, e
	}
	var ready hello
	if e = readJSON(handshake, c, &ready); e != nil || ready.Type != "ready" {
		return nil, errors.New("server did not authenticate")
	}
	ok = true
	return c, nil
}
