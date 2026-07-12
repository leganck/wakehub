package clientapp

import (
	"path/filepath"
	"testing"

	"github.com/leganck/wakehub/internal/clientcfg"
)

func TestLoadMerged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	mdnsOff := false
	if err := clientcfg.Save(path, clientcfg.Config{
		Server: "ws://from-file/api/ws/client",
		Token:  "file-token",
		Key:    "file-key",
		MDNS:   &mdnsOff,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, used, err := LoadMerged(Overrides{
		ConfigPath: path,
		Server:     "ws://cli/api/ws/client",
	})
	if err != nil {
		t.Fatal(err)
	}
	if used != path {
		t.Fatalf("used=%q", used)
	}
	if cfg.Server != "ws://cli/api/ws/client" {
		t.Fatalf("server override: %s", cfg.Server)
	}
	if cfg.Token != "file-token" || cfg.Key != "file-key" {
		t.Fatalf("file fields lost: %+v", cfg)
	}
	if cfg.MDNSEnabled() {
		t.Fatal("mdns should stay false from file")
	}
}

func TestMergeInstallConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	cfg, err := MergeInstallConfig(path, Overrides{Server: "ws://x/api/ws/client", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "ws://x/api/ws/client" || cfg.Token != "t" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Key == "" || cfg.ShutdownCmd == "" {
		t.Fatalf("defaults missing: %+v", cfg)
	}
	if !cfg.MDNSEnabled() {
		t.Fatal("mdns default true")
	}
}
