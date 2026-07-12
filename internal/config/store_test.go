package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureClientToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Settings().ClientToken != "" {
		t.Fatalf("expected empty token initially, got %q", s.Settings().ClientToken)
	}
	tok, gen, err := s.EnsureClientToken()
	if err != nil {
		t.Fatal(err)
	}
	if !gen || tok == "" || len(tok) < 16 {
		t.Fatalf("expected generated token, gen=%v tok=%q", gen, tok)
	}
	tok2, gen2, err := s.EnsureClientToken()
	if err != nil {
		t.Fatal(err)
	}
	if gen2 || tok2 != tok {
		t.Fatalf("second ensure should not regenerate: gen=%v %q vs %q", gen2, tok2, tok)
	}
	// reload
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Settings().ClientToken != tok {
		t.Fatalf("persisted token mismatch")
	}
	_ = os.Remove(path)
}

func TestRandomToken(t *testing.T) {
	a := RandomToken(32)
	b := RandomToken(32)
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("len a=%d b=%d", len(a), len(b))
	}
	if a == b {
		t.Fatal("tokens should differ")
	}
}
