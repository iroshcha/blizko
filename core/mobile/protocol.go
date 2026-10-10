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
	"strings"
)

const maxText = 4000
const maxCipher = 12000

func validPacket(e *envelope) bool {
	if e == nil || len(e.Body) > maxCipher {
		return false
	}
	raw, err := json.Marshal(e)
	return err == nil && len(raw) <= 20000
}

type contact struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Key     [32]byte `json:"key"`
	Address string   `json:"address"`
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
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && s == strings.ToLower(s) && s != strings.Repeat("0", 64)
}
func encodeContact(c contact) string {
	b, _ := json.Marshal(c)
	return "blizko:3:" + base64.RawURLEncoding.EncodeToString(b)
}
func decodeContact(code string) (contact, error) {
	var c contact
	code = strings.TrimSpace(code)
	if strings.HasPrefix(code, "blizko:2:") {
		return c, errors.New("Это старый QR. Обновите оба приложения и обменяйтесь новыми QR; история останется.")
	}
	if len(code) > 4096 || !strings.HasPrefix(code, "blizko:3:") {
		return c, errors.New("Неверный код контакта")
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "blizko:3:"))
	if e != nil {
		return c, errors.New("Код повреждён")
	}
	if e = json.Unmarshal(b, &c); e != nil || c.ID != keyID(c.Key) || !validAddress(c.Address) || c.Key == ([32]byte{}) {
		return c, errors.New("Неверные ключи или адрес контакта")
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
	if e.Version != 2 || e.From != peer.ID || len(e.Nonce) != 24 || len(e.Body) > maxCipher {
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
