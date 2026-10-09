package mobile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
	"io"
	"net/netip"
	"net/url"
	"strings"
)

const maxText = 4000

type contact struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Key     [32]byte `json:"key"`
	Address string   `json:"address"`
	DNSName string   `json:"dns,omitempty"`
	Invite  string   `json:"invite,omitempty"`
}
type envelope struct {
	Version int    `json:"v"`
	From    string `json:"from"`
	Nonce   []byte `json:"nonce"`
	Body    []byte `json:"body"`
}
type payload struct {
	Version int    `json:"v"`
	ID      string `json:"id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Kind    string `json:"kind"`
	Text    string `json:"text,omitempty"`
	Ack     string `json:"ack,omitempty"`
}

func keyID(key [32]byte) string { h := sha256.Sum256(key[:]); return hex.EncodeToString(h[:]) }
func randomID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func validAddress(s string) bool {
	a, e := netip.ParseAddr(s)
	if e != nil {
		return false
	}
	return netip.MustParsePrefix("100.64.0.0/10").Contains(a) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(a)
}
func encodeContact(c contact) string {
	b, _ := json.Marshal(c)
	return "blizko:2:" + base64.RawURLEncoding.EncodeToString(b)
}
func decodeContact(code string) (contact, error) {
	var c contact
	code = strings.TrimSpace(code)
	if len(code) > 4096 || !strings.HasPrefix(code, "blizko:2:") {
		return c, errors.New("Неверный код контакта")
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "blizko:2:"))
	if e != nil {
		return c, errors.New("Код повреждён")
	}
	if e = json.Unmarshal(b, &c); e != nil || c.ID != keyID(c.Key) || !validAddress(c.Address) || c.Key == ([32]byte{}) {
		return c, errors.New("Неверные ключи или адрес контакта")
	}
	if c.Invite != "" && !validInvitation(c.Invite) {
		return c, errors.New("Неверная ссылка приглашения Tailscale")
	}
	if c.DNSName != "" && !validDNSName(c.DNSName) {
		return c, errors.New("Неверное имя устройства Tailscale")
	}
	// NaCl's original ScalarMult API accepts low-order points. Reject those
	// at contact import with X25519's all-zero shared-secret check.
	probe := make([]byte, 32)
	probe[0] = 9
	if _, err := curve25519.X25519(probe, c.Key[:]); err != nil {
		return c, errors.New("Недопустимый ключ контакта")
	}
	return c, nil
}

func validInvitation(link string) bool {
	if len(link) > 1024 {
		return false
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.RawPath != "" {
		return false
	}
	if u.Host != "login.tailscale.com" && u.Host != "console.tailscale.com" {
		return false
	}
	if !strings.HasPrefix(u.Path, "/admin/invite/") {
		return false
	}
	token := strings.TrimPrefix(u.Path, "/admin/invite/")
	if token == "" || token == "." || token == ".." {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func validDNSName(name string) bool {
	if len(name) > 253 || !strings.HasSuffix(name, ".ts.net") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

// ContactInvitation returns only a validated Tailscale invitation. The app must
// still let the user accept it in Tailscale's browser authorization screen.
func ContactInvitation(code string) (string, error) {
	c, err := decodeContact(code)
	return c.Invite, err
}

// NaCl box authenticates the pinned sender public key and encrypts the entire
// payload, including message IDs, recipients and receipt references.
func seal(p payload, peer contact, secret [32]byte) (envelope, error) {
	var nonce [24]byte
	if _, e := io.ReadFull(rand.Reader, nonce[:]); e != nil {
		return envelope{}, e
	}
	b, e := json.Marshal(p)
	if e != nil {
		return envelope{}, e
	}
	return envelope{Version: 2, From: p.From, Nonce: nonce[:], Body: box.Seal(nil, b, &nonce, &peer.Key, &secret)}, nil
}
func openEnvelope(e envelope, peer contact, secret [32]byte, self string) (payload, error) {
	var p payload
	if e.Version != 2 || e.From != peer.ID || len(e.Nonce) != 24 || len(e.Body) > 12000 {
		return p, errors.New("invalid envelope")
	}
	var nonce [24]byte
	copy(nonce[:], e.Nonce)
	b, ok := box.Open(nil, e.Body, &nonce, &peer.Key, &secret)
	if !ok {
		return p, errors.New("authentication failed")
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, err
	}
	if p.Version != 2 || p.From != peer.ID || p.To != self || len(p.ID) != 32 || (p.Kind != "text" && p.Kind != "ack") {
		return p, errors.New("invalid payload")
	}
	if _, err := hex.DecodeString(p.ID); err != nil {
		return p, err
	}
	if p.Kind == "text" && (len(p.Text) == 0 || len(p.Text) > maxText) {
		return p, errors.New("invalid text")
	}
	if p.Kind == "ack" && len(p.Ack) != 32 {
		return p, errors.New("invalid acknowledgement")
	}
	return p, nil
}
