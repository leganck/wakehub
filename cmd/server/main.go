package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/leganck/wakehub/internal/clientlink"
	"github.com/leganck/wakehub/internal/config"
	"github.com/leganck/wakehub/internal/device"
	"github.com/leganck/wakehub/internal/mdns"
	"github.com/leganck/wakehub/internal/probe"
	"github.com/leganck/wakehub/internal/schedule"
	webserver "github.com/leganck/wakehub/internal/web"
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
		fmt.Printf("wakehub-server %s (built %s)\n", version, buildTime)
		return
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	store, err := config.Open(*cfgPath)
	if err != nil {
		slog.Error("config open failed", "err", err)
		os.Exit(1)
	}
	if *listen != "" {
		_ = store.UpdateSettings(func(s *config.Settings) error {
			s.Listen = *listen
			return nil
		})
	}

	if _, generated, err := store.EnsureClientToken(); err != nil {
		slog.Warn("client token ensure", "err", err)
	} else if generated {
		slog.Info("client token was empty; generated random token (see Web UI settings)")
	}

	hub := clientlink.NewHub(store.Settings().ClientToken)
	svc := device.NewService(store, hub)
	if err := svc.StartBemfa(); err != nil {
		slog.Warn("bemfa start", "err", err)
	}

	browser := mdns.NewBrowser()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go browser.Run(ctx)

	prober := probe.NewRunner(store, svc.ProbeCache(), svc.NICsForProbe, svc.OnProbeChange)
	go prober.Run(ctx)
	sched := schedule.New(store, svc, scheduleNote{svc: svc})
	go sched.Run(ctx)

	api := webserver.New(store, svc, hub, browser)
	api.SetProber(probeAdapter{r: prober})
	api.SetBuildInfo(webserver.BuildInfo{Version: version, BuildTime: buildTime, StartedAt: time.Now()})
	addr := store.Settings().Listen
	if addr == "" {
		addr = config.DefaultListen
	}
	srv := &http.Server{Addr: addr, Handler: api.Handler()}

	go func() {
		printStartupSummary(store, svc, hub, addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen failed", "err", err)
			os.Exit(1)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	slog.Info("shutting down")
	cancel()
	svc.Bemfa().Close()
	shctx, c2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer c2()
	_ = srv.Shutdown(shctx)
}

type probeAdapter struct{ r *probe.Runner }

func (p probeAdapter) ProbeDevice(id string) (any, error) {
	return p.r.ProbeDevice(id)
}

type scheduleNote struct{ svc *device.Service }

func (n scheduleNote) OnSchedule(action, targetType, targetID, targetName string, ok bool, errMsg string) {
	n.svc.OnSchedule(action, targetType, targetID, targetName, ok, errMsg)
}

func printStartupSummary(store *config.Store, svc *device.Service, hub *clientlink.Hub, addr string) {
	st := store.Settings()
	wsPath := st.WSPath
	if wsPath == "" {
		wsPath = config.DefaultWSPath
	}

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
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
	fmt.Fprintf(&b, "wakehub-server %s (built %s)\n", version, buildTime)
	fmt.Fprintf(&b, "  listen        %s\n", addr)
	fmt.Fprintf(&b, "  web           http://127.0.0.1:%s/\n", port)
	fmt.Fprintf(&b, "  health        http://127.0.0.1:%s/healthz\n", port)
	fmt.Fprintf(&b, "  client ws     %s\n", wsPath)
	fmt.Fprintf(&b, "  client token  %s\n", tokenHint)
	fmt.Fprintf(&b, "  devices       %d (bemfa-enabled %d)\n", len(devices), bemfaEnabledCount)
	fmt.Fprintf(&b, "  clients online %d\n", len(hub.List()))
	fmt.Fprintf(&b, "  bemfa         %s\n", bemfaStatus)
	if st.BasicAuthEnable {
		fmt.Fprintf(&b, "  basic auth    on (user=%s)\n", st.BasicAuthUser)
	} else {
		fmt.Fprintf(&b, "  basic auth    off\n")
	}
	slog.Info("server ready")
	fmt.Print(b.String())
}
