package clientapp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestWinQuote(t *testing.T) {
	if WinQuote(`C:\a.exe`) != `C:\a.exe` {
		t.Fatal(WinQuote(`C:\a.exe`))
	}
	if WinQuote(`C:\Program Files\a.exe`) != `"C:\Program Files\a.exe"` {
		t.Fatal(WinQuote(`C:\Program Files\a.exe`))
	}
}

func TestBuildServiceCommandUsesConfigOnly(t *testing.T) {
	cfg := filepath.Join("ProgramData", "wakehub-client", "config.json")
	cmd := BuildServiceCommand(`/opt/wakehub-client`, cfg)
	for _, frag := range []string{"run", "-config"} {
		if !strings.Contains(cmd, frag) {
			t.Fatalf("missing %q in %q", frag, cmd)
		}
	}
	for _, bad := range []string{"-token", "-server", "-key", "-shutdown-cmd"} {
		if strings.Contains(cmd, bad) {
			t.Fatalf("service command should not contain %q: %s", bad, cmd)
		}
	}
}
