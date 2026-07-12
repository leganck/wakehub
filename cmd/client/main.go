package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/leganck/wakehub/internal/clientcfg"
	"github.com/leganck/wakehub/internal/config"
	"github.com/leganck/wakehub/internal/mdns"
	"github.com/leganck/wakehub/internal/service"
)

// Set by GoReleaser ldflags.
var (
	version   = "dev"
	buildTime = "unknown"
)

// fatalSessionError stops the reconnect loop (e.g. auth/config errors).
type fatalSessionError struct {
	msg string
}

func (e *fatalSessionError) Error() string { return e.msg }

const serviceName = "wakehub-client"

// recentShutdownIDs dedupes remote shutdown requests (process-local).
var recentShutdownIDs sync.Map // requestId -> time.Time

func main() {
	// When started by Windows SCM, we must call StartServiceCtrlDispatcher
	// (via service.Run). Without this the service times out and auto-stops.
	if service.IsWindowsService() {
		os.Exit(runAsWindowsService())
	}

	if len(os.Args) < 2 {
		printRootHelp()
		return
	}

	cmd := os.Args[1]
	switch cmd {
	case "help", "-h", "--help":
		printRootHelp()
		return
	case "version", "-version", "--version", "-v":
		printVersion()
		return
	case "run":
		os.Exit(runClient(os.Args[2:]))
	case "service":
		os.Exit(runService(os.Args[2:]))
	// Deprecated top-level aliases (keep working, print hint).
	case "install", "uninstall", "start", "stop", "status":
		fmt.Fprintf(os.Stderr, "note: use %q instead of top-level %q (deprecated)\n", "service "+cmd, cmd)
		os.Exit(runService(append([]string{cmd}, os.Args[2:]...)))
	default:
		if strings.HasPrefix(cmd, "-") {
			os.Exit(runClient(os.Args[1:]))
		}
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		printRootHelp()
		os.Exit(2)
	}
}

// runAsWindowsService is the entry path when SCM launches binPath (… run -config …).
func runAsWindowsService() int {
	setupServiceLog()
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	err := service.Run(serviceName, func(ctx context.Context) error {
		code := runClientCtx(ctx, args)
		if code != 0 {
			return fmt.Errorf("client exited with code %d", code)
		}
		return nil
	})
	if err != nil {
		log.Printf("service run: %v", err)
		return 1
	}
	return 0
}

// setupServiceLog writes logs under the client data dir (no console when hosted by SCM).
func setupServiceLog() {
	dir := clientcfg.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, "client.log")
	rotateLogIfNeeded(path, 5<<20) // 5 MiB
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("wakehub-client %s starting as Windows service", version)
}

func rotateLogIfNeeded(path string, maxBytes int64) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < maxBytes {
		return
	}
	bak := path + ".1"
	_ = os.Remove(bak)
	_ = os.Rename(path, bak)
}

func printVersion() {
	fmt.Printf("wakehub-client %s (built %s)\n", version, buildTime)
}

func printRootHelp() {
	exe := filepath.Base(os.Args[0])
	fmt.Printf(`WakeHub Client — connect to wakehub-server for remote shutdown and NIC report.

Usage:
  %s <command> [arguments]

Commands:
  run                 Run client in foreground
  service             Manage system service (install/uninstall/start/stop/status)
  version             Print version
  help                Show this help

Examples:
  %s run -server ws://192.168.1.50:8080/api/ws/client -token SECRET -key my-pc
  %s service install -server ws://192.168.1.50:8080/api/ws/client -token SECRET -key my-pc
  %s service status
  %s service uninstall

Notes:
  - service install writes settings to a config file; binPath does not embed the token
  - re-run service install to update config (idempotent)
  - bare flags (e.g. -server ...) also run the client without the "run" subcommand
  - top-level install/start/stop/status still work but are deprecated; use "service ..."

Run '%s run -h' or '%s service -h' for command-specific flags.
`, exe, exe, exe, exe, exe, exe, exe)
}

func printServiceHelp() {
	exe := filepath.Base(os.Args[0])
	cfgPath := clientcfg.DefaultPath()
	fmt.Printf(`Manage the %s system service (Windows service / systemd unit).

Usage:
  %s service <subcommand> [arguments]

Subcommands:
  install     Install/update service and write config (idempotent)
  uninstall   Stop and remove the service
  start       Start the service
  stop        Stop the service
  status      Show service status
  help        Show this help

Install flags (saved to config file):
  -config string
        Config file path (default %q)
  -server string
        WebSocket URL (default "ws://127.0.0.1:8080/api/ws/client")
  -token string
        Client token (must match server clientToken when set)
  -key string
        Client key / identity (default: hostname)
  -shutdown-cmd string
        Shell command to power off the machine
  -mdns
        Publish mDNS (default true)

Examples:
  %s service install -server ws://192.168.1.50:8080/api/ws/client -token SECRET -key my-pc
  %s service install -token NEWTOKEN   # update token only (other fields kept if present)
  %s service start
  %s service status
  %s service uninstall

Config file: %s
Service log:  %s
`, serviceName, exe, cfgPath, exe, exe, exe, exe, exe, cfgPath, filepath.Join(clientcfg.LogDir(), "client.log"))
}

func printRunHelp(fs *flag.FlagSet) {
	exe := filepath.Base(os.Args[0])
	fmt.Fprintf(fs.Output(), `Run the WakeHub client in the foreground.

Usage:
  %s run [flags]
  %s [flags]          (same as run; flags without a subcommand)

Flags:
`, exe, exe)
	fs.PrintDefaults()
	fmt.Fprintf(fs.Output(), `
Notes:
  - flags override values from -config when non-empty
  - key must match the device "boundClientKey" on the server for remote shutdown
  - token must match server settings.clientToken when the server requires it
  - use '%s service install ...' to install as a system service
`, exe)
}

func runService(args []string) int {
	if len(args) == 0 {
		printServiceHelp()
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printServiceHelp()
		return 0
	case "install":
		return runInstall(rest)
	case "uninstall":
		return runUninstall()
	case "start":
		return service.Start(serviceName)
	case "stop":
		return service.Stop(serviceName)
	case "status":
		return runStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown service subcommand %q\n\n", sub)
		printServiceHelp()
		return 2
	}
}

func runStatus() int {
	code := service.Status(serviceName)
	cfgPath := clientcfg.DefaultPath()
	if c, err := clientcfg.Load(cfgPath); err == nil {
		fmt.Printf("\nConfig: %s\n  server: %s\n  key: %s\n  token: %s\n  mdns: %v\n",
			cfgPath, c.Server, c.Key, clientcfg.RedactToken(c.Token), c.MDNSEnabled())
	} else {
		fmt.Printf("\nConfig: %s (not loaded: %v)\n", cfgPath, err)
	}
	fmt.Printf("Log: %s\n", filepath.Join(clientcfg.LogDir(), "client.log"))
	return code
}

func runClient(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runClientCtx(ctx, args)
}

func runClientCtx(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { printRunHelp(fs) }

	configPath := fs.String("config", "", "config file path (default: platform path if present)")
	server := fs.String("server", "", "server websocket url (overrides config)")
	token := fs.String("token", "", "client token (overrides config)")
	key := fs.String("key", "", "client key (overrides config; default hostname)")
	shutdownCmd := fs.String("shutdown-cmd", "", "shutdown command (overrides config)")
	// mdns: use string so we can detect "unset" vs false when merging config
	mdnsFlag := fs.String("mdns", "", "enable mDNS publish: true|false (overrides config; default true)")
	showVersion := fs.Bool("version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		printVersion()
		return 0
	}

	cfg, cfgFile, err := loadRuntimeConfig(*configPath, *server, *token, *key, *shutdownCmd, *mdnsFlag)
	if err != nil {
		log.Printf("config: %v", err)
		return 2
	}
	if cfgFile != "" {
		log.Printf("using config %s (token=%s)", cfgFile, clientcfg.RedactToken(cfg.Token))
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
		p, err := mdns.Publish("wakehub-"+sanitizeInstance(clientKey), clientKey, hostname, mac, 9)
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

	delay := 3 * time.Second
	const maxDelay = 60 * time.Second

	for {
		if err := ctx.Err(); err != nil {
			log.Printf("client stopping: %v", err)
			return 0
		}
		authed, err := session(ctx, cfg.Server, cfg.Token, clientKey, hostname, cfg.ShutdownCmd)
		if authed {
			delay = 3 * time.Second
		}
		if err == nil {
			continue
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0
		}
		var fatal *fatalSessionError
		if errors.As(err, &fatal) {
			log.Printf("fatal: %v (not reconnecting; fix token/key/server and restart)", fatal)
			return 1
		}
		// jitter ~0–20% to avoid reconnect stampedes
		jitter := time.Duration(int64(delay) * (time.Now().UnixNano() % 20) / 100)
		wait := delay + jitter
		log.Printf("session ended: %v; retry in %v", err, wait)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
		if !authed {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
}

// loadRuntimeConfig merges optional file + CLI overrides.
func loadRuntimeConfig(configPath, server, token, key, shutdownCmd, mdnsStr string) (clientcfg.Config, string, error) {
	var cfg clientcfg.Config
	usedPath := ""

	path := strings.TrimSpace(configPath)
	tryDefault := path == ""
	if path == "" {
		path = clientcfg.DefaultPath()
	}
	loaded, err := clientcfg.Load(path)
	if err == nil {
		cfg = loaded
		usedPath = path
	} else if !tryDefault || !errors.Is(err, os.ErrNotExist) {
		// Explicit -config must exist; missing default is OK.
		if !tryDefault {
			return cfg, "", fmt.Errorf("load %s: %w", path, err)
		}
		if !errors.Is(err, os.ErrNotExist) {
			// corrupt default: warn but continue with flags
			log.Printf("warning: ignore config %s: %v", path, err)
		}
	}

	if server != "" {
		cfg.Server = server
	}
	if token != "" {
		cfg.Token = token
	}
	if key != "" {
		cfg.Key = key
	}
	if shutdownCmd != "" {
		cfg.ShutdownCmd = shutdownCmd
	}
	if mdnsStr != "" {
		v := strings.EqualFold(mdnsStr, "true") || mdnsStr == "1"
		cfg.MDNS = clientcfg.BoolPtr(v)
	}

	if cfg.Server == "" {
		cfg.Server = "ws://127.0.0.1:8080/api/ws/client"
	}
	if cfg.ShutdownCmd == "" {
		cfg.ShutdownCmd = defaultShutdownCmd()
	}
	return cfg, usedPath, nil
}

func sanitizeInstance(s string) string {
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, s)
	if s == "" {
		return "client"
	}
	return s
}

// session returns (authed, err). authed is true after a successful hello_ok.
func session(ctx context.Context, server, token, key, hostname, shutdownCmd string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	u, err := normalizeWS(server)
	if err != nil {
		return false, &fatalSessionError{msg: "invalid server url: " + err.Error()}
	}
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	conn, _, err := dialer.DialContext(ctx, u, nil)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	hello := map[string]any{
		"type":     "hello",
		"key":      key,
		"token":    token,
		"hostname": hostname,
		"nics":     collectNICs(),
		"version":  version,
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
	}
	if err := conn.WriteJSON(hello); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, err
	}

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, err
	}
	if err := handleHelloResponse(msg); err != nil {
		return false, err
	}
	log.Printf("connected as key=%s version=%s", key, version)

	writeMu := make(chan struct{}, 1)
	writeMu <- struct{}{}
	writeJSON := func(v any) error {
		<-writeMu
		defer func() { writeMu <- struct{}{} }()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(v)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			switch m["type"] {
			case "shutdown":
				handleShutdownMsg(m, shutdownCmd, writeJSON)
			case "pong":
			case "error":
				log.Printf("server error message: %s", string(data))
			default:
				log.Printf("msg: %s", string(data))
			}
		}
	}()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-done:
			return true, fmt.Errorf("connection closed")
		case <-ticker.C:
			_ = writeJSON(map[string]any{
				"type":     "ping",
				"hostname": hostname,
				"nics":     collectNICs(),
			})
		}
	}
}

func handleShutdownMsg(m map[string]any, shutdownCmd string, writeJSON func(any) error) {
	reqID, _ := m["requestId"].(string)
	if reqID != "" {
		if _, loaded := recentShutdownIDs.LoadOrStore(reqID, time.Now()); loaded {
			log.Printf("duplicate shutdown requestId=%s ignored", reqID)
			_ = writeJSON(map[string]any{
				"type": "shutdown_ack", "ok": true, "status": "duplicate", "requestId": reqID,
			})
			return
		}
		// prune old ids occasionally
		go pruneShutdownIDs()
	}

	// ACK first so the server records acceptance before the host powers off.
	if err := writeJSON(map[string]any{
		"type": "shutdown_ack", "ok": true, "status": "accepted", "requestId": reqID,
	}); err != nil {
		log.Printf("send shutdown accepted: %v", err)
	}

	log.Printf("exec shutdown requestId=%s cmd=%s", reqID, shutdownCmd)
	go func() {
		err := runCommand(shutdownCmd)
		if err != nil {
			log.Printf("shutdown error requestId=%s: %v", reqID, err)
			if werr := writeJSON(map[string]any{
				"type": "shutdown_err", "ok": false, "error": err.Error(), "requestId": reqID,
			}); werr != nil {
				log.Printf("send shutdown_err: %v", werr)
			}
			return
		}
		log.Printf("shutdown command started ok requestId=%s", reqID)
		if werr := writeJSON(map[string]any{
			"type": "shutdown_ack", "ok": true, "status": "executed", "requestId": reqID,
		}); werr != nil {
			log.Printf("send shutdown executed: %v", werr)
		}
	}()
}

func pruneShutdownIDs() {
	cutoff := time.Now().Add(-10 * time.Minute)
	recentShutdownIDs.Range(func(k, v any) bool {
		if t, ok := v.(time.Time); ok && t.Before(cutoff) {
			recentShutdownIDs.Delete(k)
		}
		return true
	})
}

func handleHelloResponse(msg []byte) error {
	var m map[string]any
	if err := json.Unmarshal(msg, &m); err != nil {
		return fmt.Errorf("invalid hello response: %w", err)
	}
	t, _ := m["type"].(string)
	switch t {
	case "hello_ok":
		return nil
	case "error":
		message, _ := m["message"].(string)
		if message == "" {
			message = string(msg)
		}
		// Auth / permanent config errors: do not spin reconnect.
		low := strings.ToLower(message)
		if strings.Contains(low, "token") || strings.Contains(low, "invalid hello") {
			return &fatalSessionError{msg: message}
		}
		return fmt.Errorf("server error: %s", message)
	default:
		if strings.Contains(string(msg), "hello_ok") {
			return nil
		}
		return fmt.Errorf("unexpected hello response: %s", string(msg))
	}
}

func normalizeWS(s string) (string, error) {
	if !strings.Contains(s, "://") {
		s = "ws://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = config.DefaultWSPath
	}
	return u.String(), nil
}

func collectNICs() []config.NICInfo {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []config.NICInfo
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || len(iface.HardwareAddr) == 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		info := config.NICInfo{Name: iface.Name, MAC: iface.HardwareAddr.String()}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			ip4 := ipn.IP.To4()
			info.IPv4 = append(info.IPv4, ip4.String())
			mask := ipn.Mask
			if len(mask) == 4 {
				b := make(net.IP, 4)
				for i := 0; i < 4; i++ {
					b[i] = ip4[i] | ^mask[i]
				}
				info.Broadcast = append(info.Broadcast, b.String())
			}
		}
		if info.MAC != "" {
			out = append(out, info)
		}
	}
	return out
}

func defaultShutdownCmd() string {
	if runtime.GOOS == "windows" {
		return "shutdown /s /t 0"
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		return "systemctl poweroff"
	}
	if _, err := exec.LookPath("poweroff"); err == nil {
		return "poweroff"
	}
	if _, err := exec.LookPath("shutdown"); err == nil {
		return "shutdown -h now"
	}
	return "systemctl poweroff"
}

// runCommand runs a shell command line so paths/args with spaces work.
func runCommand(cmdline string) error {
	cmdline = strings.TrimSpace(cmdline)
	if cmdline == "" {
		return fmt.Errorf("empty command")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", cmdline)
	} else {
		cmd = exec.Command("sh", "-c", cmdline)
	}
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log.Printf("cmd output: %s", string(out))
	}
	return err
}

func runInstall(args []string) int {
	fs := flag.NewFlagSet("service install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { printServiceHelp() }

	configPath := fs.String("config", clientcfg.DefaultPath(), "config file path")
	server := fs.String("server", "", "server websocket url")
	token := fs.String("token", "", "client token")
	key := fs.String("key", "", "client key")
	shutdownCmd := fs.String("shutdown-cmd", "", "shutdown command")
	mdnsFlag := fs.String("mdns", "", "enable mDNS: true|false")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	// Start from existing config so re-install can update a single field (e.g. token).
	cfg, err := clientcfg.Load(*configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("load config: %v", err)
		return 1
	}
	if *server != "" {
		cfg.Server = *server
	}
	if *token != "" {
		cfg.Token = *token
	}
	if *key != "" {
		cfg.Key = *key
	}
	if *shutdownCmd != "" {
		cfg.ShutdownCmd = *shutdownCmd
	}
	if *mdnsFlag != "" {
		v := strings.EqualFold(*mdnsFlag, "true") || *mdnsFlag == "1"
		cfg.MDNS = clientcfg.BoolPtr(v)
	}
	if cfg.Server == "" {
		cfg.Server = "ws://127.0.0.1:8080/api/ws/client"
	}
	if cfg.ShutdownCmd == "" {
		cfg.ShutdownCmd = defaultShutdownCmd()
	}
	if cfg.Key == "" {
		h, _ := os.Hostname()
		cfg.Key = h
	}
	if cfg.MDNS == nil {
		cfg.MDNS = clientcfg.BoolPtr(true)
	}

	if err := clientcfg.Save(*configPath, cfg); err != nil {
		log.Printf("save config: %v", err)
		return 1
	}
	log.Printf("wrote config %s (server=%s key=%s token=%s)",
		*configPath, cfg.Server, cfg.Key, clientcfg.RedactToken(cfg.Token))

	exe, err := os.Executable()
	if err != nil {
		log.Println(err)
		return 1
	}
	exe, _ = filepath.Abs(exe)
	bin := buildServiceCommand(exe, *configPath)
	existed := service.Exists(serviceName)
	if err := service.Install(serviceName, "WakeHub Client", bin); err != nil {
		log.Println(err)
		return 1
	}
	if existed {
		log.Printf("service updated: %s", serviceName)
	} else {
		log.Printf("service installed: %s", serviceName)
	}
	log.Printf("service command: %s", bin)
	return 0
}

// buildServiceCommand returns binPath/ExecStart that only points at config (no secrets).
func buildServiceCommand(exe, configPath string) string {
	if runtime.GOOS == "windows" {
		return strings.Join([]string{
			winQuote(exe),
			"run",
			"-config", winQuote(configPath),
		}, " ")
	}
	return strings.Join([]string{
		shellQuote(exe),
		"run",
		"-config", shellQuote(configPath),
	}, " ")
}

func winQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func runUninstall() int {
	if err := service.Uninstall(serviceName); err != nil {
		log.Println(err)
		return 1
	}
	log.Println("service uninstalled")
	log.Printf("note: config left at %s (delete manually if desired)", clientcfg.DefaultPath())
	return 0
}
