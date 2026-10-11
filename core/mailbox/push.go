package mailbox

import (
	"blizko/core/transport"
	"context"
	"crypto/ecdh"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	bolt "go.etcd.io/bbolt"
)

type subscription struct {
	Endpoint string       `json:"endpoint"`
	Keys     webpush.Keys `json:"keys"`
}
type pushRecord struct {
	Subscription subscription `json:"subscription"`
	Contacts     []string     `json:"contacts"`
	Generation   string       `json:"generation"`
	Cursor       int64        `json:"cursor"`
	Next         int64        `json:"next"`
	Test         bool         `json:"test"`
	Status       string       `json:"status"`
}

func (s *Store) initPush() error {
	if err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(settings)
		if raw := b.Get([]byte("vapid")); raw != nil {
			var keys []string
			if json.Unmarshal(raw, &keys) != nil || len(keys) != 2 {
				return errors.New("invalid VAPID state")
			}
			s.private, s.public = keys[0], keys[1]
			priv, e := base64.RawURLEncoding.DecodeString(s.private)
			if e != nil || len(priv) != 32 {
				return errors.New("invalid VAPID key")
			}
			x, y := elliptic.P256().ScalarBaseMult(priv)
			if base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)) != s.public {
				return errors.New("VAPID key mismatch")
			}
			return nil
		}
		var e error
		s.private, s.public, e = webpush.GenerateVAPIDKeys()
		if e != nil {
			return e
		}
		raw, _ := json.Marshal([]string{s.private, s.public})
		return b.Put([]byte("vapid"), raw)
	}); err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 8 * time.Second}).DialContext, TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 8 * time.Second}}
	s.sendPush = func(sub *subscription) (int, error) {
		if !validSubscription(sub) {
			return 0, errors.New("invalid subscription")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		response, err := webpush.SendNotificationWithContext(ctx, []byte(`{"v":1}`), &webpush.Subscription{Endpoint: sub.Endpoint, Keys: sub.Keys}, &webpush.Options{HTTPClient: httpClient, Subscriber: s.subject, VAPIDPublicKey: s.public, VAPIDPrivateKey: s.private, TTL: 3600, Topic: "blizko-message", Urgency: webpush.UrgencyHigh})
		if err != nil {
			return 0, errors.New("push transport failed")
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return response.StatusCode, nil
	}
	return nil
}
func validSubscription(s *subscription) bool {
	if s == nil || len(s.Endpoint) > 2048 {
		return false
	}
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Path == "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	// Untrusted subscriptions must never become arbitrary HTTP requests (SSRF).
	host := strings.ToLower(u.Hostname())
	if host != "web.push.apple.com" && host != "fcm.googleapis.com" && host != "updates.push.services.mozilla.com" {
		return false
	}
	auth, e := base64.RawURLEncoding.DecodeString(s.Keys.Auth)
	if e != nil || len(auth) != 16 {
		return false
	}
	pub, e := base64.RawURLEncoding.DecodeString(s.Keys.P256dh)
	if e != nil || len(pub) != 65 {
		return false
	}
	_, e = ecdh.P256().NewPublicKey(pub)
	return e == nil
}
func (s *Store) setSubscription(address string, sub *subscription, contacts []string) transport.Response {
	if s.subject == "" || !validSubscription(sub) || len(contacts) > 100 {
		return transport.Response{Status: 400, Code: "invalid_subscription"}
	}
	for _, c := range contacts {
		raw, e := hex.DecodeString(c)
		if e != nil || len(raw) != 32 || c != strings.ToLower(c) {
			return transport.Response{Status: 400}
		}
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(subscriptions)
		r := pushRecord{Subscription: *sub, Contacts: contacts, Generation: transport.ID(), Status: "ready"}
		if raw := b.Get([]byte(address)); raw != nil {
			var old pushRecord
			if json.Unmarshal(raw, &old) != nil {
				return errors.New("damaged subscription")
			}
			if old.Subscription == *sub {
				r.Cursor, r.Next, r.Status, r.Test = old.Cursor, old.Next, old.Status, old.Test
			}
		} else if b.Stats().KeyN >= 512 {
			return errors.New("subscription limit")
		}
		// One endpoint belongs to exactly one authenticated identity. Do not let a
		// copied endpoint expose another profile's notifications after restore.
		if err := b.ForEach(func(k, v []byte) error {
			var other pushRecord
			if json.Unmarshal(v, &other) != nil {
				return errors.New("damaged subscription")
			}
			if string(k) != address && other.Subscription.Endpoint == sub.Endpoint {
				return errors.New("endpoint already registered")
			}
			return nil
		}); err != nil {
			return err
		}
		raw, _ := json.Marshal(r)
		return b.Put([]byte(address), raw)
	})
	if err != nil {
		return transport.Response{Status: 503, Code: "subscription_failed"}
	}
	return transport.Response{Status: 200}
}
func (s *Store) pushStatus(address string) transport.Response {
	result := map[string]any{"registered": false}
	err := s.db.View(func(tx *bolt.Tx) error {
		if raw := tx.Bucket(subscriptions).Get([]byte(address)); raw != nil {
			var r pushRecord
			if json.Unmarshal(raw, &r) != nil {
				return errors.New("bad state")
			}
			result["registered"] = true
			result["status"] = r.Status
		}
		return nil
	})
	if err != nil {
		return transport.Response{Status: 503}
	}
	raw, _ := json.Marshal(result)
	return transport.Response{Status: 200, Data: string(raw)}
}
func (s *Store) testPush(address string) transport.Response {
	status := 200
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(subscriptions)
		raw := b.Get([]byte(address))
		if raw == nil {
			status = 404
			return nil
		}
		var r pushRecord
		if json.Unmarshal(raw, &r) != nil {
			return errors.New("bad state")
		}
		if r.Next > s.now().UnixMilli() {
			status = 429
			return nil
		}
		r.Test = true
		r.Status = "pending"
		encoded, _ := json.Marshal(r)
		return b.Put([]byte(address), encoded)
	})
	if err != nil {
		status = 503
	}
	return transport.Response{Status: status}
}

// Run owns the push scheduler. Failed provider requests retry with a delay;
// revoked/expired endpoints are discarded. No content or endpoint is logged.
func (s *Store) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	lastPrune := s.now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if s.now().Sub(lastPrune) > time.Hour {
			_ = s.Prune()
			lastPrune = s.now()
		}
		s.pushOnce()
	}
}
func (s *Store) pushOnce() {
	if s.subject == "" {
		return
	}
	type job struct {
		address string
		record  pushRecord
		cursor  int64
	}
	var jobs []job
	now := s.now().UnixMilli()
	_ = s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(subscriptions).ForEach(func(k, v []byte) error {
			if len(jobs) >= 2 {
				return nil
			}
			var r pushRecord
			if json.Unmarshal(v, &r) != nil || r.Next > now {
				return nil
			}
			pending := r.Test
			if !pending {
				_ = tx.Bucket(records).ForEach(func(_, raw []byte) error {
					var entry record
					if json.Unmarshal(raw, &entry) != nil {
						return nil
					}
					if entry.Receipt != nil || entry.Expires <= now || entry.Request.To != string(k) || entry.Created <= r.Cursor || entry.Created > now-5000 {
						return nil
					}
					for _, c := range r.Contacts {
						if c == entry.Request.From {
							pending = true
							break
						}
					}
					return nil
				})
			}
			if pending {
				jobs = append(jobs, job{string(k), r, now - 5000})
			}
			return nil
		})
	})
	for _, j := range jobs {
		code, err := s.sendPush(&j.record.Subscription)
		_ = s.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(subscriptions)
			raw := b.Get([]byte(j.address))
			if raw == nil {
				return nil
			}
			var r pushRecord
			if json.Unmarshal(raw, &r) != nil || r.Generation != j.record.Generation {
				return nil
			}
			if code == 404 || code == 410 {
				return b.Delete([]byte(j.address))
			}
			r.Next = s.now().Add(time.Minute).UnixMilli()
			if err == nil && code >= 200 && code < 300 {
				r.Cursor = j.cursor
				r.Test = false
				r.Status = "accepted"
			} else {
				r.Status = "retry"
			}
			encoded, _ := json.Marshal(r)
			return b.Put([]byte(j.address), encoded)
		})
	}
}
