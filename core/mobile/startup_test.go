package mobile

import (
	"os"
	"path/filepath"
	"testing"

	"tailscale.com/logpolicy"
)

func TestStartupProvidesPrivateLogDirectory(t *testing.T) {
	t.Setenv("TS_LOGS_DIR", "")
	t.Setenv("TS_NO_LOGS_NO_SUPPORT", "")
	want := filepath.Join(t.TempDir(), "app-private", "network")
	got, err := prepareNetworkDirectory(filepath.Dir(want))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(got)
	if err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}
	// Exercise the dependency's exact lookup that panicked on Android.
	if actual := logpolicy.LogsDir(func(string, ...any) {}); actual != want {
		t.Fatalf("Tailscale escaped app directory: %q", actual)
	}
	if os.Getenv("TS_NO_LOGS_NO_SUPPORT") != "true" {
		t.Fatal("log upload enabled")
	}
}

func TestStartupRejectsUnavailableDirectory(t *testing.T) {
	t.Setenv("TS_LOGS_DIR", "unchanged")
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareNetworkDirectory(file); err == nil {
		t.Fatal("expected filesystem error")
	}
	if os.Getenv("TS_LOGS_DIR") != "unchanged" {
		t.Fatal("failed startup modified log path")
	}
}
