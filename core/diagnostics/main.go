// Opt-in, bounded diagnostic journals. No chat contents are accepted.
package diagnostics

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const route = "/blizko-diagnostics/v1"
const maxBody = 65536
const segmentSize = 2 * 1024 * 1024
const segments = 16
const retention = 7 * 24 * time.Hour

var uuid = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var temporary = regexp.MustCompile(`^\.retention-[0-9]+$`)
var events = strings.Fields("enabled process_start process_exit foreground background network power screen service_start service_restart service_stop service_destroy task_removed start_failed connection heartbeat pause_gap manual message_received send_queued")

type event struct {
	ID         string `json:"id"`
	Event      string `json:"event"`
	Time       int64  `json:"time"`
	Elapsed    int64  `json:"elapsed"`
	Uptime     int64  `json:"uptime"`
	Reason     int    `json:"reason"`
	ExitTime   int64  `json:"exitTime"`
	Battery    int    `json:"battery"`
	Charging   bool   `json:"charging"`
	Screen     bool   `json:"screen"`
	Idle       bool   `json:"idle"`
	Saving     bool   `json:"saving"`
	Exempt     bool   `json:"exempt"`
	Restricted bool   `json:"restricted"`
	Network    string `json:"network"`
	Validated  bool   `json:"validated"`
	Service    bool   `json:"service"`
	Online     bool   `json:"online"`
	Receive    bool   `json:"receive"`
}
type batch struct {
	V       int     `json:"v"`
	Device  string  `json:"device"`
	Version int     `json:"version"`
	SDK     int     `json:"sdk"`
	Events  []event `json:"events"`
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
func decode(r io.Reader) (batch, error) {
	var b batch
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(&b); err != nil {
		return b, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return b, errors.New("trailing input")
	}
	if b.V != 1 || !uuid.MatchString(b.Device) || b.Version < 1 || b.Version > 1000000 || b.SDK < 26 || b.SDK > 100 || len(b.Events) < 1 || len(b.Events) > 64 {
		return b, errors.New("invalid envelope")
	}
	for _, e := range b.Events {
		if !uuid.MatchString(e.ID) || !contains(events, e.Event) || e.Time <= 0 || e.Elapsed < 0 || e.Uptime < 0 || e.Uptime > e.Elapsed || e.ExitTime < 0 || e.Reason < 0 || e.Reason > 100 || e.Battery < -1 || e.Battery > 100 || !contains([]string{"none", "wifi", "cellular", "vpn", "other"}, e.Network) {
			return b, errors.New("invalid event")
		}
	}
	return b, nil
}

type archive struct {
	mu        sync.Mutex
	dir       string
	now       func() time.Time
	lastPrune time.Time
}

func (a *archive) path(i int) string {
	return filepath.Join(a.dir, fmt.Sprintf("events-%02d.jsonl", i))
}

// Only these fixed names are read, rotated or removed; never use client IDs as paths.
func (a *archive) prune() error {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if temporary.MatchString(entry.Name()) {
			info, e := entry.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				return errors.New("unsafe retention temporary file")
			}
			if e = os.Remove(filepath.Join(a.dir, entry.Name())); e != nil {
				return e
			}
		}
	}
	for i := 0; i < segments; i++ {
		p := a.path(i)
		s, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if !s.Mode().IsRegular() {
			return errors.New("unsafe journal file")
		}
		// Segment mtime is insufficient: appending would keep old rows forever.
		if s.Size() > segmentSize {
			return errors.New("oversized journal segment")
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), maxBody*2)
		var keep []byte
		changed := false
		for scanner.Scan() {
			line := scanner.Bytes()
			var row struct {
				Received int64 `json:"received"`
			}
			if json.Unmarshal(line, &row) != nil {
				f.Close()
				return errors.New("invalid journal row")
			}
			if row.Received <= a.now().Add(-retention).UnixMilli() {
				changed = true
			} else {
				keep = append(keep, line...)
				keep = append(keep, '\n')
			}
		}
		e = scanner.Err()
		f.Close()
		if e != nil {
			return e
		}
		if changed {
			if len(keep) == 0 {
				if e = os.Remove(p); e != nil {
					return e
				}
			} else {
				tmp, e := os.CreateTemp(a.dir, ".retention-")
				if e != nil {
					return e
				}
				name := tmp.Name()
				_, e = tmp.Write(keep)
				if e == nil {
					e = tmp.Sync()
				}
				closeErr := tmp.Close()
				if e == nil {
					e = closeErr
				}
				if e == nil {
					e = os.Rename(name, p)
				}
				if e != nil {
					os.Remove(name)
					return e
				}
			}
		}
	}
	a.lastPrune = a.now()
	return nil
}
func (a *archive) append(b batch) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.now().Sub(a.lastPrune) >= time.Hour {
		if e := a.prune(); e != nil {
			return e
		}
	}
	record := struct {
		Received int64 `json:"received"`
		Batch    batch `json:"batch"`
	}{a.now().UnixMilli(), b}
	raw, e := json.Marshal(record)
	if e != nil {
		return e
	}
	raw = append(raw, '\n')
	info, e := os.Lstat(a.path(0))
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if info != nil && !info.Mode().IsRegular() {
		return errors.New("unsafe journal file")
	}
	if info != nil && info.Size()+int64(len(raw)) > segmentSize {
		if e = os.Remove(a.path(segments - 1)); e != nil && !os.IsNotExist(e) {
			return e
		}
		for i := segments - 2; i >= 0; i-- {
			if e = os.Rename(a.path(i), a.path(i+1)); e != nil && !os.IsNotExist(e) {
				return e
			}
		}
	}
	f, e := os.OpenFile(a.path(0), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Write(raw); e != nil {
		return e
	}
	return f.Sync()
}

type collector struct {
	store *archive
	key   [32]byte
	mu    sync.Mutex
	start time.Time
	count int
	slots chan struct{}
}

func (c *collector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "query", 400)
		return
	}
	supplied := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare(supplied[:], c.key[:]) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		http.Error(w, "busy", 503)
		return
	}
	c.mu.Lock()
	if time.Since(c.start) >= time.Minute {
		c.start = time.Now()
		c.count = 0
	}
	c.count++
	allowed := c.count <= 120
	c.mu.Unlock()
	if !allowed {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "rate", 429)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Content-Encoding") != "" {
		http.Error(w, "type", 415)
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(15 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	defer r.Body.Close()
	b, e := decode(r.Body)
	if e != nil {
		http.Error(w, "invalid batch", 400)
		return
	}
	if e = c.store.append(b); e != nil {
		http.Error(w, "storage unavailable", 503)
		return
	}
	w.WriteHeader(204)
}

// Open validates private storage and returns a collector and retention cleanup.
// Caller must acquire its listener before opening storage to prevent two writers.
func Open(ctx context.Context, dir, tokenFile string) (http.Handler, error) {
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}
	value := strings.TrimSpace(string(token))
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 32 || len(value) != 43 {
		return nil, errors.New("invalid diagnostics token")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("unsafe journal directory")
	}
	a := &archive{dir: dir, now: time.Now}
	if err = a.prune(); err != nil {
		return nil, err
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.mu.Lock()
				e := a.prune()
				a.mu.Unlock()
				if e != nil {
					log.Print("Journal retention failed")
				}
			}
		}
	}()
	return &collector{store: a, key: sha256.Sum256([]byte(value)), slots: make(chan struct{}, 4)}, nil
}

const Path = route
