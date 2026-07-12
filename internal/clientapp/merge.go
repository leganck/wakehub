package clientapp

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/leganck/wakehub/internal/clientcfg"
)

// Overrides are optional CLI values applied on top of a config file.
// Empty strings mean "do not override".
type Overrides struct {
	ConfigPath  string
	Server      string
	Token       string
	Key         string
	ShutdownCmd string
	// MDNS is "", "true"/"1", or "false"/"0".
	MDNS string
}

// LoadMerged loads config (optional default path) and applies overrides.
// usedPath is the file that was loaded, or empty if none.
func LoadMerged(o Overrides) (cfg clientcfg.Config, usedPath string, err error) {
	path := strings.TrimSpace(o.ConfigPath)
	tryDefault := path == ""
	if path == "" {
		path = clientcfg.DefaultPath()
	}
	loaded, loadErr := clientcfg.Load(path)
	if loadErr == nil {
		cfg = loaded
		usedPath = path
	} else if !tryDefault {
		return cfg, "", fmt.Errorf("load %s: %w", path, loadErr)
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		log.Printf("warning: ignore config %s: %v", path, loadErr)
	}

	applyOverrides(&cfg, o)

	if cfg.Server == "" {
		cfg.Server = "ws://127.0.0.1:8080/api/ws/client"
	}
	if cfg.ShutdownCmd == "" {
		cfg.ShutdownCmd = DefaultShutdownCmd()
	}
	return cfg, usedPath, nil
}

// MergeInstallConfig starts from an existing file (if any) and applies install flags.
func MergeInstallConfig(path string, o Overrides) (clientcfg.Config, error) {
	cfg, err := clientcfg.Load(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	applyOverrides(&cfg, o)
	if cfg.Server == "" {
		cfg.Server = "ws://127.0.0.1:8080/api/ws/client"
	}
	if cfg.ShutdownCmd == "" {
		cfg.ShutdownCmd = DefaultShutdownCmd()
	}
	if cfg.Key == "" {
		h, _ := os.Hostname()
		cfg.Key = h
	}
	if cfg.MDNS == nil {
		cfg.MDNS = clientcfg.BoolPtr(true)
	}
	return cfg, nil
}

func applyOverrides(cfg *clientcfg.Config, o Overrides) {
	if o.Server != "" {
		cfg.Server = o.Server
	}
	if o.Token != "" {
		cfg.Token = o.Token
	}
	if o.Key != "" {
		cfg.Key = o.Key
	}
	if o.ShutdownCmd != "" {
		cfg.ShutdownCmd = o.ShutdownCmd
	}
	if o.MDNS != "" {
		v := strings.EqualFold(o.MDNS, "true") || o.MDNS == "1"
		cfg.MDNS = clientcfg.BoolPtr(v)
	}
}
