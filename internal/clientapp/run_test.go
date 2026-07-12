package clientapp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/leganck/wakehub/internal/clientcfg"
)

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := Run(ctx, RunOptions{
		Config: clientcfg.Config{
			Server:      "ws://127.0.0.1:9/api/ws/client",
			ShutdownCmd: "true",
			MDNS:        clientcfg.BoolPtr(false),
		},
		Session: func(ctx context.Context, server, token, key, hostname, shutdownCmd, restartCmd, version string) (bool, error) {
			calls++
			return false, fmt.Errorf("dial failed")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls < 1 {
		t.Fatal("session never called")
	}
}

func TestRunFatalNoReconnect(t *testing.T) {
	ctx := context.Background()
	calls := 0
	err := Run(ctx, RunOptions{
		Config: clientcfg.Config{
			Server:      "ws://x/api/ws/client",
			ShutdownCmd: "true",
			MDNS:        clientcfg.BoolPtr(false),
		},
		Session: func(ctx context.Context, server, token, key, hostname, shutdownCmd, restartCmd, version string) (bool, error) {
			calls++
			return false, &FatalError{Msg: "token mismatch"}
		},
	})
	if !IsFatal(err) {
		t.Fatalf("want fatal, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}
