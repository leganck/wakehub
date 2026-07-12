package main

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/leganck/wakehub/internal/clientcfg"
)

func TestNormalizeWS(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"127.0.0.1:8080", "ws://127.0.0.1:8080/api/ws/client"},
		{"http://host:9/api/ws/client", "ws://host:9/api/ws/client"},
		{"https://host/path", "wss://host/path"},
		{"ws://h:1/", "ws://h:1/api/ws/client"},
	}
	for _, c := range cases {
		got, err := normalizeWS(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%q => %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := normalizeWS("ftp://x"); err == nil {
		t.Fatal("expected scheme error")
	}
}

func TestHandleHelloResponse(t *testing.T) {
	if err := handleHelloResponse([]byte(`{"type":"hello_ok"}`)); err != nil {
		t.Fatal(err)
	}
	err := handleHelloResponse([]byte(`{"type":"error","message":"token mismatch"}`))
	var fatal *fatalSessionError
	if !errors.As(err, &fatal) {
		t.Fatalf("want fatal, got %v", err)
	}
	if !strings.Contains(fatal.Error(), "token") {
		t.Fatalf("msg: %v", fatal)
	}
	err = handleHelloResponse([]byte(`{"type":"error","message":"other"}`))
	if errors.As(err, &fatal) {
		t.Fatal("non-token error should not be fatal")
	}
}

func TestWinQuote(t *testing.T) {
	if winQuote(`C:\a.exe`) != `C:\a.exe` {
		t.Fatal(winQuote(`C:\a.exe`))
	}
	if winQuote(`C:\Program Files\a.exe`) != `"C:\Program Files\a.exe"` {
		t.Fatal(winQuote(`C:\Program Files\a.exe`))
	}
}

func TestBuildServiceCommandUsesConfigOnly(t *testing.T) {
	cfg := filepath.Join("ProgramData", "wakehub-client", "config.json")
	cmd := buildServiceCommand(`/opt/wakehub-client`, cfg)
	for _, frag := range []string{"run", "-config"} {
		if !strings.Contains(cmd, frag) {
			t.Fatalf("missing %q in %q", frag, cmd)
		}
	}
	// secrets must not appear in service command line
	for _, bad := range []string{"-token", "-server", "-key", "-shutdown-cmd"} {
		if strings.Contains(cmd, bad) {
			t.Fatalf("service command should not contain %q: %s", bad, cmd)
		}
	}
	if runtime.GOOS != "windows" && !strings.Contains(cmd, shellQuote(cfg)) && !strings.Contains(cmd, cfg) {
		t.Fatalf("config path missing: %s", cmd)
	}
}

func TestLoadRuntimeConfigMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	mdnsOff := false
	if err := clientcfg.Save(path, clientcfg.Config{
		Server: "ws://from-file/api/ws/client",
		Token:  "file-token",
		Key:    "file-key",
		MDNS:   &mdnsOff,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, used, err := loadRuntimeConfig(path, "ws://cli/api/ws/client", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if used != path {
		t.Fatalf("used=%q", used)
	}
	if cfg.Server != "ws://cli/api/ws/client" {
		t.Fatalf("server override: %s", cfg.Server)
	}
	if cfg.Token != "file-token" || cfg.Key != "file-key" {
		t.Fatalf("file fields lost: %+v", cfg)
	}
	if cfg.MDNSEnabled() {
		t.Fatal("mdns should stay false from file")
	}
}
