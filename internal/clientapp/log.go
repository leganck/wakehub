package clientapp

import (
	"log"
	"os"
	"path/filepath"

	"github.com/leganck/wakehub/internal/clientcfg"
)

// SetupServiceLog redirects the standard logger to the client data directory.
func SetupServiceLog(version string) {
	dir := clientcfg.LogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, "client.log")
	RotateLogIfNeeded(path, 5<<20) // 5 MiB
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("wakehub-client %s starting as Windows service", version)
}

// RotateLogIfNeeded renames path to path.1 when larger than maxBytes.
func RotateLogIfNeeded(path string, maxBytes int64) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < maxBytes {
		return
	}
	bak := path + ".1"
	_ = os.Remove(bak)
	_ = os.Rename(path, bak)
}
