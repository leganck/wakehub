package clientcfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	in := Config{
		Server:      "ws://h:1/api/ws/client",
		Token:       "secret-token-value",
		Key:         "pc1",
		ShutdownCmd: "poweroff",
		MDNS:        BoolPtr(false),
	}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Server != in.Server || out.Token != in.Token || out.Key != in.Key {
		t.Fatalf("got %+v", out)
	}
	if out.MDNSEnabled() {
		t.Fatal("mdns should be false")
	}
	// permissions: owner-only preferred (may be no-op on Windows)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() == 0 {
		t.Fatal("empty file")
	}
}

func TestRedactToken(t *testing.T) {
	if RedactToken("") != "(empty)" {
		t.Fatal(RedactToken(""))
	}
	if RedactToken("abcdefghij") == "abcdefghij" {
		t.Fatal("should redact")
	}
}
