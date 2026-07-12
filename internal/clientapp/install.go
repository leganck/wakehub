package clientapp

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/leganck/wakehub/internal/clientcfg"
	"github.com/leganck/wakehub/internal/service"
)

// DefaultServiceName is the Windows/systemd unit name.
const DefaultServiceName = "wakehub-client"

// InstallOptions for service install/update.
type InstallOptions struct {
	ConfigPath  string
	Server      string
	Token       string
	Key         string
	ShutdownCmd string
	MDNS        string // "", "true", "false"
	// Exe empty => os.Executable()
	Exe         string
	ServiceName string
	DisplayName string
}

// InstallService writes config and registers/updates the system service.
func InstallService(opt InstallOptions) error {
	if opt.ConfigPath == "" {
		opt.ConfigPath = clientcfg.DefaultPath()
	}
	if opt.ServiceName == "" {
		opt.ServiceName = DefaultServiceName
	}
	if opt.DisplayName == "" {
		opt.DisplayName = "WakeHub Client"
	}

	cfg, err := MergeInstallConfig(opt.ConfigPath, Overrides{
		Server:      opt.Server,
		Token:       opt.Token,
		Key:         opt.Key,
		ShutdownCmd: opt.ShutdownCmd,
		MDNS:        opt.MDNS,
	})
	if err != nil {
		return fmt.Errorf("merge config: %w", err)
	}
	if err := clientcfg.Save(opt.ConfigPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	log.Printf("wrote config %s (server=%s key=%s token=%s)",
		opt.ConfigPath, cfg.Server, cfg.Key, clientcfg.RedactToken(cfg.Token))

	exe := opt.Exe
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return err
		}
		exe, _ = filepath.Abs(exe)
	}
	bin := BuildServiceCommand(exe, opt.ConfigPath)
	existed := service.Exists(opt.ServiceName)
	if err := service.Install(opt.ServiceName, opt.DisplayName, bin); err != nil {
		return err
	}
	if existed {
		log.Printf("service updated: %s", opt.ServiceName)
	} else {
		log.Printf("service installed: %s", opt.ServiceName)
	}
	log.Printf("service command: %s", bin)
	return nil
}

// UninstallService removes the system service (config file is kept).
func UninstallService(name string) error {
	if name == "" {
		name = DefaultServiceName
	}
	if err := service.Uninstall(name); err != nil {
		return err
	}
	log.Println("service uninstalled")
	log.Printf("note: config left at %s (delete manually if desired)", clientcfg.DefaultPath())
	return nil
}
