package clientapp

import (
	"sync"
	"testing"
	"time"
)

func TestHandleShutdownMsgAckFirstAndDedupe(t *testing.T) {
	ResetShutdownDedupe()
	defer func() { CommandRunner = runCommand }()

	var mu sync.Mutex
	var msgs []map[string]any
	ran := make(chan struct{}, 1)
	CommandRunner = func(cmd string) error {
		ran <- struct{}{}
		return nil
	}
	write := func(v any) error {
		mu.Lock()
		defer mu.Unlock()
		msgs = append(msgs, v.(map[string]any))
		return nil
	}

	HandleShutdownMsg(map[string]any{"type": "shutdown", "requestId": "r1"}, "poweroff", write)
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("command not run")
	}
	// wait for executed ack
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(msgs)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	if len(msgs) < 1 || msgs[0]["status"] != "accepted" {
		t.Fatalf("first msg: %+v", msgs)
	}
	mu.Unlock()

	// duplicate
	HandleShutdownMsg(map[string]any{"type": "shutdown", "requestId": "r1"}, "poweroff", write)
	mu.Lock()
	defer mu.Unlock()
	last := msgs[len(msgs)-1]
	if last["status"] != "duplicate" {
		t.Fatalf("want duplicate, got %+v", last)
	}
}
