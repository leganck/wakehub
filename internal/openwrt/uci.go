// Package openwrt helpers for syncing WakeHub globals to UCI (OpenWrt/Kwrt).
package openwrt

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/leganck/wakehub/internal/config"
)

// UCIAvailable reports whether the uci binary is on PATH.
func UCIAvailable() bool {
	_, err := exec.LookPath("uci")
	return err == nil
}

// WriteGlobals writes global settings into /etc/config/wakehub and commits.
func WriteGlobals(st config.Settings) error {
	if !UCIAvailable() {
		return fmt.Errorf("uci not found; cannot writeback globals")
	}
	port := st.ListenPort()
	auth := "0"
	if st.BasicAuthEnable {
		auth = "1"
	}
	user := strings.TrimSpace(st.BasicAuthUser)
	if user == "" {
		user = config.DefaultAuthUser
	}
	pass := st.BasicAuthPassword
	if strings.TrimSpace(pass) == "" {
		pass = config.DefaultAuthPassword
	}
	ws := strings.TrimSpace(st.WSPath)
	if ws == "" {
		ws = config.DefaultWSPath
	}

	sets := [][2]string{
		{"wakehub.main.listen_port", port},
		{"wakehub.main.bemfa_uid", st.BemfaUID},
		{"wakehub.main.client_token", st.ClientToken},
		{"wakehub.main.ws_path", ws},
		{"wakehub.main.basic_auth_enable", auth},
		{"wakehub.main.basic_auth_user", user},
		{"wakehub.main.basic_auth_password", pass},
	}
	for _, kv := range sets {
		if err := runUCI("set", kv[0]+"="+kv[1]); err != nil {
			return fmt.Errorf("uci set %s: %w", kv[0], err)
		}
	}
	if err := runUCI("commit", "wakehub"); err != nil {
		return fmt.Errorf("uci commit: %w", err)
	}
	// Best-effort re-sync config.json from UCI so devices[] merge stays consistent.
	if sync := os.Getenv("WAKEHUB_UCI_SYNC"); sync != "" {
		_ = exec.Command(sync).Run()
	} else if _, err := os.Stat("/usr/libexec/wakehub-uci-sync"); err == nil {
		_ = exec.Command("/usr/libexec/wakehub-uci-sync").Run()
	}
	return nil
}

func runUCI(args ...string) error {
	cmd := exec.Command("uci", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
