package main

import (
	"encoding/json"
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
	"github.com/leganck/wol/internal/config"
	"github.com/leganck/wol/internal/mdns"
	"github.com/leganck/wol/internal/service"
)

// Set by GoReleaser ldflags.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			os.Exit(runInstall(os.Args[2:]))
		case "uninstall":
			os.Exit(runUninstall())
		case "start":
			os.Exit(service.Start("wol-client"))
		case "stop":
			os.Exit(service.Stop("wol-client"))
		case "status":
			os.Exit(service.Status("wol-client"))
		case "run":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		case "version", "-version", "--version":
			fmt.Printf("wol-client %s (built %s)\n", version, buildTime)
			return
		}
	}
	runClient()
}

func runClient() {
	server := flag.String("server", "ws://127.0.0.1:8080/api/ws/client", "server websocket url")
	token := flag.String("token", "", "client token")
	key := flag.String("key", "", "client key (default hostname)")
	shutdownCmd := flag.String("shutdown-cmd", defaultShutdownCmd(), "shutdown command")
	enableMDNS := flag.Bool("mdns", true, "enable mDNS publish")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("wol-client %s (built %s)\n", version, buildTime)
		return
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
		p, err := mdns.Publish("wol-"+sanitizeInstance(clientKey), clientKey, hostname, mac, 9)
		if err != nil {
			log.Printf("mdns publish failed: %v", err)
		} else {
			pub = p
			log.Printf("mdns published as wol-%s", clientKey)
		}
	}
	if pub != nil {
		defer pub.Shutdown()
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	for {
		if err := session(*server, *token, clientKey, hostname, *shutdownCmd); err != nil {
			log.Printf("session ended: %v", err)
		}
		select {
		case <-interrupt:
			return
		case <-time.After(3 * time.Second):
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

func session(server, token, key, hostname, shutdownCmd string) error {
	u, err := normalizeWS(server)
	if err != nil {
		return err
	}
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	hello := map[string]any{
		"type":     "hello",
		"key":      key,
		"token":    token,
		"hostname": hostname,
		"nics":     collectNICs(),
	}
	if err := conn.WriteJSON(hello); err != nil {
		return err
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	log.Printf("server: %s", string(msg))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
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
				if err := runCommand(shutdownCmd); err != nil {
					log.Printf("shutdown error: %v", err)
				}
			case "pong":
			default:
				log.Printf("msg: %s", string(data))
			}
		}
	}()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return fmt.Errorf("connection closed")
		case <-ticker.C:
			_ = conn.WriteJSON(map[string]any{
				"type":     "ping",
				"hostname": hostname,
				"nics":     collectNICs(),
			})
		}
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
	return "systemctl poweroff"
}

func runCommand(cmdline string) error {
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return fmt.Errorf("empty command")
	}
	cmd := exec.Command(fields[0], fields[1:]...)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log.Printf("cmd output: %s", string(out))
	}
	return err
}

func runInstall(args []string) int {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	server := fs.String("server", "ws://127.0.0.1:8080/api/ws/client", "server url")
	token := fs.String("token", "", "token")
	key := fs.String("key", "", "key")
	shutdownCmd := fs.String("shutdown-cmd", defaultShutdownCmd(), "shutdown command")
	mdnsFlag := fs.Bool("mdns", true, "enable mdns")
	_ = fs.Parse(args)
	exe, err := os.Executable()
	if err != nil {
		log.Println(err)
		return 1
	}
	exe, _ = filepath.Abs(exe)
	// sc.exe binPath= must be one argument; quote exe path on Windows
	var bin string
	if runtime.GOOS == "windows" {
		bin = fmt.Sprintf("\"%s\" run -server %s -token %s -key %s -shutdown-cmd \"%s\" -mdns=%v",
			exe, *server, *token, *key, *shutdownCmd, *mdnsFlag)
	} else {
		bin = fmt.Sprintf("%s run -server %s -token %s -key %s -shutdown-cmd %s -mdns=%v",
			exe, shellQuote(*server), shellQuote(*token), shellQuote(*key), shellQuote(*shutdownCmd), *mdnsFlag)
	}
	if err := service.Install("wol-client", "WOL Client", bin); err != nil {
		log.Println(err)
		return 1
	}
	log.Println("service installed: wol-client")
	return 0
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func runUninstall() int {
	if err := service.Uninstall("wol-client"); err != nil {
		log.Println(err)
		return 1
	}
	log.Println("service uninstalled")
	return 0
}
