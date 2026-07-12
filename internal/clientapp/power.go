package clientapp

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
)

// DefaultShutdownCmd returns a platform-appropriate power-off command.
func DefaultShutdownCmd() string {
	if runtime.GOOS == "windows" {
		return "shutdown /s /t 0"
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		return "systemctl poweroff"
	}
	if _, err := exec.LookPath("poweroff"); err == nil {
		return "poweroff"
	}
	if _, err := exec.LookPath("shutdown"); err == nil {
		return "shutdown -h now"
	}
	return "systemctl poweroff"
}

// RunCommand runs a shell command line so paths/args with spaces work.
// Overridable in tests via CommandRunner.
var CommandRunner = runCommand

func runCommand(cmdline string) error {
	cmdline = strings.TrimSpace(cmdline)
	if cmdline == "" {
		return fmt.Errorf("empty command")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", cmdline)
	} else {
		cmd = exec.Command("sh", "-c", cmdline)
	}
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log.Printf("cmd output: %s", string(out))
	}
	return err
}
