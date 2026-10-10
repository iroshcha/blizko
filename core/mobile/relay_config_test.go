package mobile

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRelayURLValidationPersistenceAndIdentity(t *testing.T) {
	n := testNode(t, "")
	code, _ := n.MyCode()
	for _, url := range []string{"http://relay.example.com/", "https://user@relay.example.com/", "https://relay.example.com/?token=secret", "https://relay.example.com/path", "https://relay.example.com:99999/", "not a url"} {
		if err := n.SetRelayURL(url); err == nil {
			t.Fatal("invalid server accepted", url)
		}
	}
	if err := n.SetRelayURL("https://relay.example.com"); err != nil {
		t.Fatal(err)
	}
	again, err := NewNode(n.dir, bytes.Repeat([]byte{42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	after, _ := again.MyCode()
	if after != code || again.state.RelayURL != "https://relay.example.com/" {
		t.Fatal("server change lost identity or setting")
	}
	if err = again.SetRelayURL(""); err != nil {
		t.Fatal(err)
	}
	var status struct{ RelayURL string }
	if err = json.Unmarshal([]byte(again.Status()), &status); err != nil || status.RelayURL != homeRelayURL {
		t.Fatal("default server not restored", err)
	}
}
