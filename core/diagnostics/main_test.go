package diagnostics

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*collector, []byte) {
	t.Helper()
	b := batch{V: 1, Device: "12345678-1234-1234-1234-123456789abc", Version: 11, SDK: 34, Events: []event{{ID: "abcdefab-1234-1234-1234-123456789abc", Event: "power", Time: time.Now().UnixMilli(), Network: "wifi", Battery: 50}}}
	raw, _ := json.Marshal(b)
	return &collector{key: sha256.Sum256([]byte("test-secret")), slots: make(chan struct{}, 4), store: &archive{dir: t.TempDir(), now: time.Now}}, raw
}
func request(c *collector, body []byte, token string) int {
	r := httptest.NewRequest("POST", route, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	c.ServeHTTP(w, r)
	return w.Code
}
func TestAuthenticationSchemaAndLimits(t *testing.T) {
	c, raw := fixture(t)
	if got := request(c, raw, "wrong"); got != 401 {
		t.Fatal(got)
	}
	if _, e := os.Stat(c.store.path(0)); !os.IsNotExist(e) {
		t.Fatal("unauthorized persisted")
	}
	for _, bad := range [][]byte{append(raw, []byte(`{}`)...), []byte(strings.Replace(string(raw), `"network":"wifi"`, `"network":"secret message"`, 1)), []byte(strings.Replace(string(raw), `"v":1`, `"text":"private","v":1`, 1)), bytes.Repeat([]byte(" "), maxBody+1)} {
		if got := request(c, bad, "test-secret"); got != 400 {
			t.Fatalf("bad schema %d", got)
		}
	}
	if got := request(c, raw, "test-secret"); got != 204 {
		t.Fatal(got)
	}
	data, e := os.ReadFile(c.store.path(0))
	if e != nil || !bytes.Contains(data, []byte(`"received"`)) || bytes.Contains(data, []byte("test-secret")) {
		t.Fatal("storage", e)
	}
	for i := 0; i < 121; i++ {
		request(c, raw, "test-secret")
	}
	if got := request(c, raw, "test-secret"); got != 429 {
		t.Fatal("unbounded writes", got)
	}
}
func TestRetentionMixedSegmentAndDiskFailure(t *testing.T) {
	c, raw := fixture(t)
	b, _ := decode(bytes.NewReader(raw))
	now := time.Now()
	c.store.now = func() time.Time { return now.Add(-8 * 24 * time.Hour) }
	if e := c.store.append(b); e != nil {
		t.Fatal(e)
	}
	// A recent row in the same recently modified file must not retain an old row.
	old, _ := os.ReadFile(c.store.path(0))
	recent := fmt.Sprintf("{\"received\":%d,\"batch\":%s}\n", now.UnixMilli(), raw)
	os.WriteFile(c.store.path(0), append(old, []byte(recent)...), 0600)
	c.store.now = func() time.Time { return now }
	if e := c.store.prune(); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(c.store.path(0))
	if bytes.Count(data, []byte("\n")) != 1 {
		t.Fatal("old records retained")
	}
	os.Remove(c.store.path(0))
	os.Mkdir(c.store.path(0), 0700)
	if got := request(c, raw, "test-secret"); got != 503 {
		t.Fatal("false acknowledgement", got)
	}
}
func TestRotationBound(t *testing.T) {
	c, raw := fixture(t)
	b, _ := decode(bytes.NewReader(raw))
	for len(b.Events) < 64 {
		b.Events = append(b.Events, b.Events[0])
	}
	for i := 0; i < 1800; i++ {
		if e := c.store.append(b); e != nil {
			t.Fatal(e)
		}
	}
	files, _ := os.ReadDir(c.store.dir)
	var total int64
	for _, f := range files {
		s, _ := f.Info()
		total += s.Size()
	}
	if len(files) > segments || total > segments*segmentSize {
		t.Fatal("unbounded storage", total, len(files))
	}
}
