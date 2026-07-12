package clientapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotateLogIfNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.log")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	RotateLogIfNeeded(path, 1000)
	if _, err := os.Stat(path); err != nil {
		t.Fatal("should not rotate small file")
	}
	big := make([]byte, 200)
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}
	RotateLogIfNeeded(path, 100)
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatal("expected rotated file")
	}
}
