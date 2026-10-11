// Package mailbox persists only signed encrypted packets and encrypted receipts.
package mailbox

import (
	"blizko/core/transport"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

const retention = 7 * 24 * time.Hour
const maxRecords = 4096
const maxPerDevice = 256

var records = []byte("packets")
var subscriptions = []byte("subscriptions")
var settings = []byte("settings")

type record struct {
	Created int64            `json:"created"`
	Expires int64            `json:"expires"`
	Request transport.Frame  `json:"request"`
	Receipt *transport.Frame `json:"receipt,omitempty"`
	Done    bool             `json:"done,omitempty"`
}
type Store struct {
	db                       *bolt.DB
	now                      func() time.Time
	private, public, subject string
	sendPush                 func(*subscription) (int, error)
	replayMu                 sync.Mutex
	replay                   map[string]int
}

func Open(dir, subject string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("unsafe mailbox directory")
	}
	filename := filepath.Join(dir, "mailbox.db")
	if info, err := os.Lstat(filename); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("unsafe mailbox file")
	}
	db, err := bolt.Open(filename, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, now: time.Now, subject: subject, replay: map[string]int{}}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{records, subscriptions, settings} {
			if _, e := tx.CreateBucketIfNotExists(name); e != nil {
				return e
			}
		}
		return nil
	})
	if err == nil {
		err = s.initPush()
	}
	if err == nil {
		err = s.Prune()
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func key(f transport.Frame) []byte {
	h := sha256.Sum256([]byte(f.From + "\x00" + f.To + "\x00" + f.Data))
	return []byte(hex.EncodeToString(h[:]))
}
func encrypted(data string) bool {
	var v struct {
		V     int    `json:"v"`
		From  string `json:"from"`
		Nonce []byte `json:"nonce"`
		Body  []byte `json:"body"`
	}
	if len(data) > transport.MaxPacket {
		return false
	}
	d := json.NewDecoder(bytes.NewBufferString(data))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || v.V != 2 || len(v.From) != 64 || len(v.Nonce) != 24 || len(v.Body) < 16 || len(v.Body) > 12000 {
		return false
	}
	if _, err := hex.DecodeString(v.From); err != nil {
		return false
	}
	var tail any
	return d.Decode(&tail) == io.EOF
}
func (s *Store) Offer(f transport.Frame) (transport.Offered, error) {
	out := transport.Offered{Frame: f}
	if f.Type != "request" || !f.Valid() || !encrypted(f.Data) {
		return out, nil
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(records)
		now := s.now().UnixMilli()
		k := key(f)
		if raw := b.Get(k); raw != nil {
			var r record
			if json.Unmarshal(raw, &r) != nil {
				return errors.New("damaged mailbox record")
			}
			if r.Expires > now {
				out = transport.Offered{Frame: r.Request, Stored: true, Expires: r.Expires}
				if r.Receipt != nil {
					out.Frame = *r.Receipt
				}
				return nil
			}
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		total, from, to := 0, 0, 0
		var completed []byte
		var oldest int64
		if err := b.ForEach(func(k, raw []byte) error {
			var r record
			if json.Unmarshal(raw, &r) != nil {
				return errors.New("damaged record")
			}
			total++
			if r.Done {
				if completed == nil || r.Created < oldest {
					completed = append([]byte{}, k...)
					oldest = r.Created
				}
				return nil
			}
			if r.Request.From == f.From {
				from++
			}
			if r.Request.To == f.To {
				to++
			}
			return nil
		}); err != nil {
			return err
		}
		if total >= maxRecords && completed != nil {
			if err := b.Delete(completed); err != nil {
				return err
			}
			total--
		}
		if total >= maxRecords || from >= maxPerDevice || to >= maxPerDevice || tx.Size() > 256*1024*1024 {
			return errors.New("mailbox full")
		}
		r := record{Created: now, Expires: s.now().Add(retention).UnixMilli(), Request: f}
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if err = b.Put(k, raw); err != nil {
			return err
		}
		out = transport.Offered{Frame: f, Stored: true, Expires: r.Expires}
		return nil
	})
	return out, err
}
func (s *Store) SaveReceipt(f transport.Frame) error {
	if f.Type != "response" || !f.Valid() || f.Result == nil || f.Result.Status != 200 || !encrypted(f.Result.Data) {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(records)
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var r record
			if json.Unmarshal(v, &r) != nil {
				return errors.New("damaged record")
			}
			if r.Request.ID == f.ID && r.Request.From == f.To && r.Request.To == f.From && r.Receipt == nil {
				r.Receipt = &f
				// Remove ciphertext after the recipient's signed durable receipt. Retain
				// only routing, content hash (bucket key), and the encrypted receipt.
				r.Request.Data = ""
				r.Request.Signature = ""
				raw, err := json.Marshal(r)
				if err != nil {
					return err
				}
				return b.Put(k, raw)
			}
		}
		return nil
	})
}
func (s *Store) Pending(address string, limit int) []transport.Frame {
	var entries []record
	if limit < 1 || limit > 8 {
		limit = 8
	}
	_ = s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(records).ForEach(func(_, v []byte) error {
			var r record
			if json.Unmarshal(v, &r) != nil {
				return nil
			}
			if r.Done || r.Expires <= s.now().UnixMilli() {
				return nil
			}
			if (r.Receipt != nil && r.Receipt.To == address) || (r.Receipt == nil && r.Request.To == address) {
				entries = append(entries, r)
			}
			return nil
		})
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Created < entries[j].Created })
	if len(entries) == 0 {
		return nil
	}
	s.replayMu.Lock()
	if len(s.replay) >= 256 {
		for k := range s.replay {
			delete(s.replay, k)
			break
		}
	}
	offset := s.replay[address] % len(entries)
	s.replay[address] = offset + limit
	s.replayMu.Unlock()
	out := []transport.Frame{}
	for i := 0; i < len(entries) && i < limit; i++ {
		r := entries[(offset+i)%len(entries)]
		if r.Receipt != nil {
			out = append(out, *r.Receipt)
		} else {
			out = append(out, r.Request)
		}
	}
	return out
}
func (s *Store) Prune() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(records)
		c := b.Cursor()
		now := s.now().UnixMilli()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var r record
			if json.Unmarshal(v, &r) != nil {
				return errors.New("damaged record")
			}
			if r.Expires <= now {
				if err := c.Delete(); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func (s *Store) Control(f transport.Frame) transport.Response {
	if f.Type != "request" || f.To != transport.ControlAddress || !f.Valid() {
		return transport.Response{Status: 403}
	}
	var req struct {
		Op           string        `json:"op"`
		ID           string        `json:"id"`
		Sender       string        `json:"sender"`
		Subscription *subscription `json:"subscription"`
		Contacts     []string      `json:"contacts"`
	}
	d := json.NewDecoder(bytes.NewBufferString(f.Data))
	d.DisallowUnknownFields()
	if d.Decode(&req) != nil {
		return transport.Response{Status: 400}
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return transport.Response{Status: 400}
	}
	switch req.Op {
	case "config":
		raw, _ := json.Marshal(map[string]any{"publicKey": s.public, "retentionDays": 7, "push": s.subject != ""})
		return transport.Response{Status: 200, Data: string(raw)}
	case "receipt-done":
		err := s.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(records)
			c := b.Cursor()
			for k, v := c.First(); k != nil; k, v = c.Next() {
				var r record
				if json.Unmarshal(v, &r) != nil {
					return errors.New("damaged record")
				}
				if r.Receipt != nil && r.Receipt.To == f.From && r.Receipt.From == req.Sender && r.Receipt.ID == req.ID {
					r.Done = true
					raw, _ := json.Marshal(r)
					return b.Put(k, raw)
				}
			}
			return nil
		})
		if err != nil {
			return transport.Response{Status: 503}
		}
		return transport.Response{Status: 200}
	case "push-set":
		return s.setSubscription(f.From, req.Subscription, req.Contacts)
	case "push-remove":
		if err := s.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(subscriptions).Delete([]byte(f.From)) }); err != nil {
			return transport.Response{Status: 503}
		}
		return transport.Response{Status: 200}
	case "push-status":
		return s.pushStatus(f.From)
	case "push-test":
		return s.testPush(f.From)
	default:
		return transport.Response{Status: 400}
	}
}
