package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/leganck/wol/internal/clientlink"
	"github.com/leganck/wol/internal/config"
	"github.com/leganck/wol/internal/device"
	"github.com/leganck/wol/internal/mdns"
	webserver "github.com/leganck/wol/internal/web"
)

// Set by GoReleaser ldflags.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	cfgPath := flag.String("config", "data/config.json", "config file path")
	listen := flag.String("listen", "", "override listen address")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("wol-server %s (built %s)\n", version, buildTime)
		return
	}

	store, err := config.Open(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if *listen != "" {
		_ = store.UpdateSettings(func(s *config.Settings) error {
			s.Listen = *listen
			return nil
		})
	}

	hub := clientlink.NewHub(store.Settings().ClientToken)
	svc := device.NewService(store, hub)
	if err := svc.StartBemfa(); err != nil {
		log.Printf("bemfa start warning: %v", err)
	}

	browser := mdns.NewBrowser()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go browser.Run(ctx)

	api := webserver.New(store, svc, hub, browser)
	addr := store.Settings().Listen
	if addr == "" {
		addr = config.DefaultListen
	}
	srv := &http.Server{Addr: addr, Handler: api.Handler()}

	go func() {
		printStartupSummary(store, svc, hub, addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	log.Printf("shutting down...")
	cancel()
	svc.Bemfa().Close()
	shctx, c2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer c2()
	_ = srv.Shutdown(shctx)
}

func printStartupSummary(store *config.Store, svc *device.Service, hub *clientlink.Hub, addr string) {
	st := store.Settings()
	wsPath := st.WSPath
	if wsPath == "" {
		wsPath = config.DefaultWSPath
	}

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		// e.g. invalid form; show whole addr as fallback
		port = addr
	}
	if port == "" {
		port = "?"
	}

	devices := store.ListDevices()
	bemfaUIDSet := strings.TrimSpace(st.BemfaUID) != ""
	bemfaEnabledCount := 0
	for _, d := range devices {
		if d.BemfaEnable {
			bemfaEnabledCount++
		}
	}
	tokenHint := "(empty)"
	if strings.TrimSpace(st.ClientToken) != "" {
		tokenHint = "set"
	}
	bemfaStatus := "disabled (UID empty)"
	if bemfaUIDSet {
		if svc.Bemfa().Connected() {
			bemfaStatus = "connected"
		} else {
			bemfaStatus = "connecting / not connected"
		}
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("========== WOL Server ==========\n")
	fmt.Fprintf(&b, "  Version     : %s (%s)\n", version, buildTime)
	fmt.Fprintf(&b, "  Listen      : %s\n", addr)
	fmt.Fprintf(&b, "  Port        : %s\n", port)
	fmt.Fprintf(&b, "  Web UI      : http://127.0.0.1:%s/\n", port)
	fmt.Fprintf(&b, "  Client WS   : ws://<host>:%s%s\n", port, wsPath)
	fmt.Fprintf(&b, "  Config      : %s\n", store.Path())
	fmt.Fprintf(&b, "  Devices     : %d (bemfa enabled: %d)\n", len(devices), bemfaEnabledCount)
	fmt.Fprintf(&b, "  Online clients: %d\n", len(hub.List()))
	fmt.Fprintf(&b, "  Client token: %s\n", tokenHint)
	fmt.Fprintf(&b, "  Bemfa       : %s\n", bemfaStatus)
	b.WriteString("================================\n")
	log.Print(b.String())
}
