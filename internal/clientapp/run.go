package clientapp

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/leganck/wakehub/internal/clientcfg"
	"github.com/leganck/wakehub/internal/mdns"
)

// RunOptions configures the long-running client loop.
type RunOptions struct {
	Config  clientcfg.Config
	Version string
	// Session is optional; tests may inject a stub. nil => Session.
	Session func(ctx context.Context, server, token, key, hostname, shutdownCmd, restartCmd, version string) (bool, error)
}

// Run reconnects until ctx is cancelled or a fatal session error occurs.
// Returns nil on clean stop, *FatalError on permanent failure.
func Run(ctx context.Context, opt RunOptions) error {
	cfg := opt.Config
	version := opt.Version
	if version == "" {
		version = "dev"
	}
	sessionFn := opt.Session
	if sessionFn == nil {
		sessionFn = Session
	}

	clientKey := cfg.Key
	if clientKey == "" {
		h, _ := os.Hostname()
		clientKey = h
	}
	hostname, _ := os.Hostname()

	var pub *mdns.Publisher
	if cfg.MDNSEnabled() {
		mac := mdns.PrimaryMAC()
		p, err := mdns.Publish("wakehub-"+SanitizeInstance(clientKey), clientKey, hostname, mac, 9)
		if err != nil {
			log.Printf("mdns publish failed: %v", err)
		} else {
			pub = p
			log.Printf("mdns published as wakehub-%s", clientKey)
		}
	}
	if pub != nil {
		defer pub.Shutdown()
	}

	delay := InitialBackoff
	for {
		if err := ctx.Err(); err != nil {
			log.Printf("client stopping: %v", err)
			return nil
		}
		restartCmd := DefaultRestartCmd()
		authed, err := sessionFn(ctx, cfg.Server, cfg.Token, clientKey, hostname, cfg.ShutdownCmd, restartCmd, version)
		if authed {
			delay = InitialBackoff
		}
		if err == nil {
			continue
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		if f := asFatal(err); f != nil {
			log.Printf("fatal: %v (not reconnecting; fix token/key/server and restart)", f)
			return f
		}
		wait := WithJitter(delay, 20, time.Now().UnixNano())
		log.Printf("session ended: %v; retry in %v", err, wait)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if !authed {
			delay = NextBackoff(delay, MaxBackoff)
		}
	}
}
