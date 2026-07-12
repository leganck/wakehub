// Package clientcfg stores wakehub-client connection settings on disk
// so Windows/Linux service binPath does not need to embed secrets.
package clientcfg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Config is persisted client settings.
type Config struct {
	Server      string `json:"server"`
	Token       string `json:"token,omitempty"`
	Key         string `json:"key,omitempty"`
	ShutdownCmd string `json:"shutdownCmd,omitempty"`
	// MDNS is nil = default true when running.
	MDNS *bool `json:"mdns,omitempty"`
}

// DefaultPath returns the platform config path.
// Windows: %ProgramData%\wakehub-client\config.json
// Others:  /etc/wakehub-client/config.json
func DefaultPath() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "wakehub-client", "config.json")
	}
	return "/etc/wakehub-client/config.json"
}

// LogDir is the directory for service logs (same parent as config on Windows).
func LogDir() string {
	return filepath.Dir(DefaultPath())
}

// Load reads config from path. Missing file returns empty Config and os.ErrNotExist.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}

// Save writes config atomically (temp + rename).
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// RedactToken returns a display-safe token hint.
func RedactToken(token string) string {
	if token == "" {
		return "(empty)"
	}
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "…" + token[len(token)-4:]
}

// MDNSEnabled returns whether mDNS should be on (default true).
func (c Config) MDNSEnabled() bool {
	if c.MDNS == nil {
		return true
	}
	return *c.MDNS
}

// BoolPtr is a helper for optional bool fields.
func BoolPtr(v bool) *bool { return &v }
