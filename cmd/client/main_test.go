package main

import (
	"errors"
	"strings"
	"testing"
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

func TestBuildServiceCommandWindowsShape(t *testing.T) {
	// Only shape-check: contains run and flags (platform-specific quoting differs).
	cmd := buildServiceCommand(`/opt/wakehub-client`, `ws://h:8080/api/ws/client`, `tok`, `my-pc`, `systemctl poweroff`, true)
	for _, frag := range []string{"run", "-server", "-token", "-key", "-shutdown-cmd", "-mdns=true"} {
		if !strings.Contains(cmd, frag) {
			t.Fatalf("missing %q in %q", frag, cmd)
		}
	}
}
