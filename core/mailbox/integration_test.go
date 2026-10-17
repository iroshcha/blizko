package mailbox_test

import (
	"blizko/core/mailbox"
	"blizko/core/mobile"
	"blizko/core/transport"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

type snapshot struct {
	Online   bool `json:"online"`
	Messages []struct {
		ID, Text                  string
		Out, Delivered, Submitted bool
		StoredUntil               int64 `json:"storedUntil"`
	} `json:"messages"`
}

func view(n *mobile.Node) snapshot {
	var v snapshot
	json.Unmarshal([]byte(n.Snapshot()), &v)
	return v
}
func wait(t *testing.T, fn func() bool) {
	t.Helper()
	end := time.Now().Add(25 * time.Second)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("timed out")
}
func contactID(code string) string {
	raw, _ := base64.RawURLEncoding.DecodeString(code[9:])
	var c struct{ ID string }
	json.Unmarshal(raw, &c)
	return c.ID
}
func TestOfflineDevicesAcrossServerRestart(t *testing.T) {
	dir := t.TempDir()
	s, e := mailbox.Open(dir, "")
	if e != nil {
		t.Fatal(e)
	}
	broker := transport.NewServer()
	broker.Durable = s
	httpServer := httptest.NewServer(broker)
	a, e := mobile.NewNode(t.TempDir(), make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	b, e := mobile.NewNode(t.TempDir(), make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	defer a.Stop()
	defer b.Stop()
	t.Setenv("BLIZKO_SERVER_URL", httpServer.URL)
	ac, _ := a.MyCode()
	bc, _ := b.MyCode()
	if e = a.AddContact("B", bc); e != nil {
		t.Fatal(e)
	}
	if e = b.AddContact("A", ac); e != nil {
		t.Fatal(e)
	}
	if e = a.Start(); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 20; i++ {
		if e = a.Send(contactID(bc), "Offline "+transport.ID()); e != nil {
			t.Fatal(e)
		}
	}
	wait(t, func() bool {
		v := view(a)
		if len(v.Messages) != 20 {
			return false
		}
		for _, m := range v.Messages {
			if m.StoredUntil == 0 || m.Delivered {
				return false
			}
		}
		return true
	})
	a.Stop()
	broker.Close()
	httpServer.Close()
	s.Close()
	// Nothing is shared in memory across this restart; both devices are stopped.
	s, e = mailbox.Open(dir, "")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	broker = transport.NewServer()
	broker.Durable = s
	httpServer = httptest.NewServer(broker)
	defer httpServer.Close()
	defer broker.Close()
	t.Setenv("BLIZKO_SERVER_URL", httpServer.URL)
	if e = b.Start(); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { return len(view(b).Messages) == 20 })
	b.Stop()
	if e = a.Start(); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool {
		v := view(a)
		for _, m := range v.Messages {
			if !m.Delivered {
				return false
			}
		}
		return len(v.Messages) == 20
	})
	if len(view(b).Messages) != 20 {
		t.Fatal("duplicate delivery")
	}
}
