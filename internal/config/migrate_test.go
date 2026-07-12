package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyConfigMigratesBasicAuth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Pre-auth config: no basicAuthEnable
	legacy := map[string]any{
		"settings": map[string]any{
			"listen":      ":9090",
			"bemfaUID":    "",
			"clientToken": "abc",
			"wsPath":      "/api/ws/client",
		},
		"devices": []any{},
	}
	b, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Settings()
	if !st.BasicAuthEnable {
		t.Fatal("expected basic auth enabled after migration")
	}
	if st.BasicAuthUser != DefaultAuthUser || st.BasicAuthPassword != DefaultAuthPassword {
		t.Fatalf("defaults: user=%q pass=%q", st.BasicAuthUser, st.BasicAuthPassword)
	}
	if st.Listen != ":9090" {
		t.Fatalf("listen preserved: %q", st.Listen)
	}
	if s.Snapshot().Version < CurrentConfigVersion {
		t.Fatalf("version not bumped: %d", s.Snapshot().Version)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup: %v", err)
	}

	// Second open should not re-migrate / break explicit disable
	_ = s.UpdateSettings(func(st *Settings) error {
		st.BasicAuthEnable = false
		st.BasicAuthPassword = "secret"
		return nil
	})
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Settings().BasicAuthEnable {
		t.Fatal("explicit disable should stick after version field exists")
	}
	if s2.Settings().BasicAuthPassword != "secret" {
		t.Fatalf("password: %q", s2.Settings().BasicAuthPassword)
	}
}

func TestPublicSettingsMasksPassword(t *testing.T) {
	st := Settings{
		Listen:            ":8080",
		BasicAuthEnable:   true,
		BasicAuthUser:     "admin",
		BasicAuthPassword: "s3cret",
		ClientToken:       "tok",
		WSPath:            DefaultWSPath,
	}
	pub := st.Public()
	if _, ok := pub["basicAuthPassword"]; ok {
		t.Fatal("password must not appear in public map")
	}
	if pub["basicAuthPasswordSet"] != true {
		t.Fatal("expected passwordSet true")
	}
	if pub["basicAuthUser"] != "admin" {
		t.Fatalf("user: %v", pub["basicAuthUser"])
	}
}

func TestNeedsBasicAuthMigration(t *testing.T) {
	if !needsBasicAuthMigration([]byte(`{"settings":{},"devices":[]}`)) {
		t.Fatal("empty settings should migrate")
	}
	if needsBasicAuthMigration([]byte(`{"settings":{"basicAuthEnable":false}}`)) {
		t.Fatal("explicit key should not migrate")
	}
}
