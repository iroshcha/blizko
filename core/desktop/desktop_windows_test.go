//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"blizko/core/mobile"
)

func TestStorageKeySurvivesRestartAndRejectsMissingKey(t *testing.T) {
	dir := t.TempDir()
	key, err := storageKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := os.ReadFile(filepath.Join(dir, "master.wrapped"))
	if err != nil || bytes.Contains(wrapped, key) {
		t.Fatal("unprotected key", err)
	}
	reloaded, err := storageKey(dir)
	if err != nil || !bytes.Equal(key, reloaded) {
		t.Fatal("key changed", err)
	}
	node, err := mobile.NewNode(filepath.Join(dir, "core"), key)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := node.MyCode()
	if err := os.Remove(filepath.Join(dir, "master.wrapped")); err != nil {
		t.Fatal(err)
	}
	if _, err := storageKey(dir); err == nil {
		t.Fatal("lost key replaced")
	}
	if next, _ := node.MyCode(); next != code {
		t.Fatal("failed load modified identity")
	}
}

func TestStorageLockExcludesAnotherEngine(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockStorage(dir); err == nil {
		second()
		t.Fatal("second engine acquired storage")
	}
	unlock()
	next, err := lockStorage(dir)
	if err != nil {
		t.Fatal("lock not released", err)
	}
	next()
}

func TestDesktopCommandsPersistUnicodeQueueWithoutSecrets(t *testing.T) {
	key := bytes.Repeat([]byte{42}, 32)
	dir := t.TempDir()
	node, err := mobile.NewNode(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := mobile.NewNode(t.TempDir(), key)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := peer.MyCode()
	var peerState struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(peer.Snapshot()), &peerState)
	var input bytes.Buffer
	writer := json.NewEncoder(&input)
	_ = writer.Encode(command{ID: 1, Op: "add", First: "Друг 👋", Second: code})
	_ = writer.Encode(command{ID: 2, Op: "send", First: peerState.ID, Second: "Привет с Windows 👋\nВторая строка"})
	_ = writer.Encode(command{ID: 3, Op: "snapshot"})
	_ = writer.Encode(command{ID: 4, Op: "unknown"})
	_ = writer.Encode(command{ID: 5, Op: "quit"})
	var output bytes.Buffer
	if err := run(&input, &output, node); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte(`"secret"`)) || bytes.Contains(output.Bytes(), []byte(`"irohSeed"`)) {
		t.Fatal("secret exposed to UI")
	}
	reader := json.NewDecoder(&output)
	for id := 1; id <= 5; id++ {
		var result response
		if err := reader.Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.ID != id || (result.Error != "") != (id == 4) {
			t.Fatalf("wrong result: %+v", result)
		}
	}
	restored, err := mobile.NewNode(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(restored.Snapshot()), []byte("Привет с Windows")) {
		t.Fatal("Unicode queue lost after restart")
	}
}
