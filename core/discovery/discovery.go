// Package discovery verifies the small public relay directory. No profile data
// or private chat keys are sent to the directory host.
package discovery

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const DirectoryURL = "https://raw.githubusercontent.com/iroshcha/blizko/relay-address/relay.json"
const PublicKeyHex = "a39185062bec2cd0d01d26cdc319d3d3e7f0bb7bca117b1da1b68246936037e2"
const FallbackURL = "https://ample-raven-6363.a.tuna.free/"
const Domain = "blizko-relay-directory-v1\x00"

type Record struct {
	Version  int    `json:"v"`
	URL      string `json:"url"`
	Revision int64  `json:"revision"`
	Expires  int64  `json:"expires"`
}
type Envelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

func ValidURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawPath == "" && u.Path == "/" && u.String() == raw && strings.Contains(u.Hostname(), ".") && net.ParseIP(u.Hostname()) == nil && !strings.HasSuffix(u.Hostname(), ".local") && !strings.HasSuffix(u.Hostname(), ".localhost")
}
func Verify(raw []byte, key ed25519.PublicKey, now time.Time) (Record, error) {
	var r Record
	var env Envelope
	if len(raw) > 4096 || json.Unmarshal(raw, &env) != nil {
		return r, errors.New("invalid directory")
	}
	payload, e := base64.StdEncoding.DecodeString(env.Payload)
	if e != nil {
		return r, e
	}
	sig, e := base64.StdEncoding.DecodeString(env.Signature)
	if e != nil {
		return r, e
	}
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, append([]byte(Domain), payload...), sig) {
		return r, errors.New("invalid directory signature")
	}
	if json.Unmarshal(payload, &r) != nil || r.Version != 1 || !ValidURL(r.URL) || r.Revision <= 0 || r.Revision > now.Add(10*time.Minute).UnixMilli() || r.Expires <= now.UnixMilli() || r.Expires <= r.Revision || r.Expires-r.Revision > int64((8*24*time.Hour)/time.Millisecond) {
		return Record{}, errors.New("invalid or expired directory record")
	}
	return r, nil
}
func Sign(r Record, key ed25519.PrivateKey) ([]byte, error) {
	payload, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	return json.Marshal(Envelope{base64.StdEncoding.EncodeToString(payload), base64.StdEncoding.EncodeToString(ed25519.Sign(key, append([]byte(Domain), payload...)))})
}

type Resolver struct {
	mu     sync.Mutex
	path   string
	key    ed25519.PublicKey
	record Record
	next   time.Time
}

func New(path string) *Resolver {
	key, _ := hex.DecodeString(PublicKeyHex)
	r := &Resolver{path: path, key: key}
	if raw, e := os.ReadFile(path); e == nil {
		r.record, _ = Verify(raw, key, time.Now())
	}
	return r
}
func (r *Resolver) Resolve(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fallback := FallbackURL
	if r.record.Expires > time.Now().UnixMilli() {
		fallback = r.record.URL
	}
	if time.Now().Before(r.next) {
		return fallback, nil
	}
	r.next = time.Now().Add(time.Minute)
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", DirectoryURL+"?t="+time.Now().UTC().Format("200601021504"), nil)
	if e != nil {
		return fallback, e
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return fallback, nil
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fallback, nil
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 4097))
	if e != nil {
		return fallback, nil
	}
	record, e := Verify(raw, r.key, time.Now())
	if e != nil || record.Revision < r.record.Revision || (record.Revision == r.record.Revision && record.URL != r.record.URL) {
		return fallback, nil
	}
	r.record = record
	// Public, signed metadata; an interrupted write only loses the optional cache.
	_ = os.WriteFile(r.path, raw, 0600)
	return record.URL, nil
}
func Managed(raw string) bool {
	return raw == "" || strings.TrimRight(raw, "/") == strings.TrimRight(FallbackURL, "/") || strings.TrimRight(raw, "/") == "https://ample-raven-6363.ru.tuna.am"
}
