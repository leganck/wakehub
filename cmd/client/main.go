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
	"syscall"
	"time"

	"github.com/gorilla/websocket"
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
			// Flags without "run" subcommand: treat as runClient args.
			os.Exit(runClient(os.Args[1:]))
		}
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		printRootHelp()
		os.Exit(2)
	}
}

// runAsWindowsService is the entry path when SCM launches binPath (… run -server …).
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

// setupServiceLog writes logs under %ProgramData%\wakehub-client (no console when hosted by SCM).
func setupServiceLog() {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	dir := filepath.Join(base, "wakehub-client")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "client.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("wakehub-client %s starting as Windows service", version)
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
  - bare flags (e.g. -server ...) also run the client without the "run" subcommand
  - top-level install/start/stop/status still work but are deprecated; use "service ..."

Run '%s run -h' or '%s service -h' for command-specific flags.
`, exe, exe, exe, exe, exe, exe, exe)
}

func printServiceHelp() {
	exe := filepath.Base(os.Args[0])
	fmt.Printf(`Manage the %s system service (Windows service / systemd unit).

Usage:
  %s service <subcommand> [arguments]

Subcommands:
  install     Install and start the service
  uninstall   Stop and remove the service
  start       Start the service
  stop        Stop the service
  status      Show service status
  help        Show this help

Install flags:
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
  %s service start
  %s service status
  %s service uninstall
`, serviceName, exe, exe, exe, exe, exe)
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
		return service.Status(serviceName)
	default:
		fmt.Fprintf(os.Stderr, "unknown service subcommand %q\n\n", sub)
		printServiceHelp()
		return 2
	}
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

	server := fs.String("server", "ws://127.0.0.1:8080/api/ws/client", "server websocket url")
	token := fs.String("token", "", "client token")
	key := fs.String("key", "", "client key (default hostname)")
	shutdownCmd := fs.String("shutdown-cmd", defaultShutdownCmd(), "shutdown command")
	enableMDNS := fs.Bool("mdns", true, "enable mDNS publish")
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

	clientKey := *key
	if clientKey == "" {
		h, _ := os.Hostname()
		clientKey = h
	}
	hostname, _ := os.Hostname()

	var pub *mdns.Publisher
	if *enableMDNS {
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
		authed, err := session(ctx, *server, *token, clientKey, hostname, *shutdownCmd)
		if authed {
			// Successful session: reset backoff for next reconnect.
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
		log.Printf("session ended: %v; retry in %v", err, delay)
		timer := time.NewTimer(delay)
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
	// Dial with a short bound so stop is not blocked for too long.
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

	// Cancel closes the socket so blocked reads/writes unblock on service stop.
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
	log.Printf("connected as key=%s", key)

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
				log.Printf("exec shutdown: %s", shutdownCmd)
				err := runCommand(shutdownCmd)
				resp := map[string]any{"type": "shutdown_ack", "ok": true}
				if err != nil {
					log.Printf("shutdown error: %v", err)
					resp = map[string]any{
						"type":  "shutdown_err",
						"ok":    false,
						"error": err.Error(),
					}
				} else {
					log.Printf("shutdown command started ok")
				}
				if werr := writeJSON(resp); werr != nil {
					log.Printf("send shutdown result: %v", werr)
				}
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
		// Be lenient for older servers that might send plain text.
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
		// Skip down interfaces when possible
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
	fs.Usage = func() {
		printServiceHelp()
	}
	server := fs.String("server", "ws://127.0.0.1:8080/api/ws/client", "server websocket url")
	token := fs.String("token", "", "client token")
	key := fs.String("key", "", "client key (default hostname)")
	shutdownCmd := fs.String("shutdown-cmd", defaultShutdownCmd(), "shutdown command")
	mdnsFlag := fs.Bool("mdns", true, "enable mDNS publish")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	exe, err := os.Executable()
	if err != nil {
		log.Println(err)
		return 1
	}
	exe, _ = filepath.Abs(exe)
	bin := buildServiceCommand(exe, *server, *token, *key, *shutdownCmd, *mdnsFlag)
	if err := service.Install(serviceName, "WakeHub Client", bin); err != nil {
		log.Println(err)
		return 1
	}
	log.Printf("service installed: %s", serviceName)
	log.Printf("service command: %s", bin)
	return 0
}

func buildServiceCommand(exe, server, token, key, shutdownCmd string, mdns bool) string {
	if runtime.GOOS == "windows" {
		// sc.exe binPath= must be a single argument; quote each piece that needs it.
		parts := []string{
			winQuote(exe),
			"run",
			"-server", winQuote(server),
			"-token", winQuote(token),
			"-key", winQuote(key),
			"-shutdown-cmd", winQuote(shutdownCmd),
			fmt.Sprintf("-mdns=%v", mdns),
		}
		return strings.Join(parts, " ")
	}
	parts := []string{
		shellQuote(exe),
		"run",
		"-server", shellQuote(server),
		"-token", shellQuote(token),
		"-key", shellQuote(key),
		"-shutdown-cmd", shellQuote(shutdownCmd),
		fmt.Sprintf("-mdns=%v", mdns),
	}
	return strings.Join(parts, " ")
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
	return 0
}
