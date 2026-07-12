package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/leganck/wakehub/internal/clientapp"
	"github.com/leganck/wakehub/internal/clientcfg"
	"github.com/leganck/wakehub/internal/service"
)

// Set by GoReleaser ldflags.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// When started by Windows SCM, we must call StartServiceCtrlDispatcher.
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

func runAsWindowsService() int {
	clientapp.SetupServiceLog(version)
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	err := service.Run(clientapp.DefaultServiceName, func(ctx context.Context) error {
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
`, clientapp.DefaultServiceName, exe, cfgPath, exe, exe, exe, exe, exe, cfgPath, filepath.Join(clientcfg.LogDir(), "client.log"))
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
		if err := clientapp.UninstallService(""); err != nil {
			log.Println(err)
			return 1
		}
		return 0
	case "start":
		return service.Start(clientapp.DefaultServiceName)
	case "stop":
		return service.Stop(clientapp.DefaultServiceName)
	case "status":
		return runStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown service subcommand %q\n\n", sub)
		printServiceHelp()
		return 2
	}
}

func runStatus() int {
	code := service.Status(clientapp.DefaultServiceName)
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

	cfg, cfgFile, err := clientapp.LoadMerged(clientapp.Overrides{
		ConfigPath:  *configPath,
		Server:      *server,
		Token:       *token,
		Key:         *key,
		ShutdownCmd: *shutdownCmd,
		MDNS:        *mdnsFlag,
	})
	if err != nil {
		log.Printf("config: %v", err)
		return 2
	}
	if cfgFile != "" {
		log.Printf("using config %s (token=%s)", cfgFile, clientcfg.RedactToken(cfg.Token))
	}

	if err := clientapp.Run(ctx, clientapp.RunOptions{Config: cfg, Version: version}); err != nil {
		if clientapp.IsFatal(err) {
			return 1
		}
		log.Printf("run: %v", err)
		return 1
	}
	return 0
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

	if err := clientapp.InstallService(clientapp.InstallOptions{
		ConfigPath:  *configPath,
		Server:      *server,
		Token:       *token,
		Key:         *key,
		ShutdownCmd: *shutdownCmd,
		MDNS:        *mdnsFlag,
	}); err != nil {
		log.Println(err)
		return 1
	}
	return 0
}
