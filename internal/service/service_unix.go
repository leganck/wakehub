//go:build !windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsWindowsService is always false on non-Windows.
func IsWindowsService() bool { return false }

// Run is only used for Windows SCM integration.
func Run(name string, run Runner) error {
	return fmt.Errorf("windows service mode is not supported on this platform")
}

func unitPath(name string) string {
	return filepath.Join("/etc/systemd/system", name+".service")
}

func Install(name, display, binCmd string) error {
	content := fmt.Sprintf(`[Unit]
Description=%s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
`, display, binCmd)
	path := unitPath(name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload: %v: %s", err, out)
	}
	if out, err := exec.Command("systemctl", "enable", "--now", name).CombinedOutput(); err != nil {
		return fmt.Errorf("enable: %v: %s", err, out)
	}
	return nil
}

func Uninstall(name string) error {
	_, _ = exec.Command("systemctl", "disable", "--now", name).CombinedOutput()
	_ = os.Remove(unitPath(name))
	_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()
	return nil
}

func Start(name string) int {
	out, err := exec.Command("systemctl", "start", name).CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		return 1
	}
	return 0
}

func Stop(name string) int {
	out, err := exec.Command("systemctl", "stop", name).CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		return 1
	}
	return 0
}

func Status(name string) int {
	out, err := exec.Command("systemctl", "is-active", name).CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		return 1
	}
	if strings.TrimSpace(string(out)) != "active" {
		return 2
	}
	return 0
}
