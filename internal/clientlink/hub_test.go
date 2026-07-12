package clientlink

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func dialWS(t *testing.T, ts *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + path
	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func TestHubTokenAndShutdown(t *testing.T) {
	hub := NewHub("secret")
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	t.Cleanup(ts.Close)

	// wrong token
	c := dialWS(t, ts, "/")
	_ = c.WriteJSON(map[string]any{"type": "hello", "key": "pc1", "token": "bad", "hostname": "h"})
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(msg), "token mismatch") {
		t.Fatalf("want token mismatch, got %s", msg)
	}
	_ = c.Close()

	// good token
	c2 := dialWS(t, ts, "/")
	_ = c2.WriteJSON(map[string]any{
		"type": "hello", "key": "pc1", "token": "secret", "hostname": "host1",
		"nics": []map[string]any{{"name": "eth0", "mac": "aa:bb:cc:dd:ee:ff"}},
	})
	_ = c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err = c2.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(msg), "hello_ok") {
		t.Fatalf("want hello_ok, got %s", msg)
	}

	// wait until registered
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.Online("pc1") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !hub.Online("pc1") {
		t.Fatal("client not online")
	}
	info, ok := hub.Get("pc1")
	if !ok || info.Hostname != "host1" {
		t.Fatalf("info: %+v", info)
	}

	done := make(chan string, 1)
	go func() {
		_ = c2.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, m, err := c2.ReadMessage()
		if err != nil {
			done <- "err:" + err.Error()
			return
		}
		done <- string(m)
	}()
	if err := hub.Shutdown("pc1"); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if !strings.Contains(got, "shutdown") {
		t.Fatalf("want shutdown msg, got %s", got)
	}
	// client reports ack
	_ = c2.WriteJSON(map[string]any{"type": "shutdown_ack", "ok": true})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, _ := hub.Get("pc1")
		if info.LastEvent == "shutdown_ack" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	info, _ = hub.Get("pc1")
	if info.LastEvent != "shutdown_ack" {
		t.Fatalf("want lastEvent shutdown_ack, got %+v", info)
	}
	_ = c2.Close()
}

func TestHubRejectsInvalidHello(t *testing.T) {
	hub := NewHub("")
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	t.Cleanup(ts.Close)
	c := dialWS(t, ts, "/")
	_ = c.WriteJSON(map[string]any{"type": "hello", "key": ""})
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(msg), "invalid hello") {
		t.Fatalf("got %s", msg)
	}
	_ = c.Close()
}
