//go:build windows

package service

import (
	"fmt"
	"os/exec"
	"strings"
)

func Install(name, display, binCmd string) error {
	// sc.exe requires: binPath= "C:\path\exe args..."
	args := []string{"create", name, "binPath=", binCmd, "start=", "auto", "DisplayName=", display}
	out, err := exec.Command("sc.exe", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(out))
	}
	_, _ = exec.Command("sc.exe", "description", name, "WakeHub shutdown client for wakehub-server").CombinedOutput()
	_, _ = exec.Command("sc.exe", "start", name).CombinedOutput()
	return nil
}

func Uninstall(name string) error {
	_, _ = exec.Command("sc.exe", "stop", name).CombinedOutput()
	out, err := exec.Command("sc.exe", "delete", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(out))
	}
	return nil
}

func Start(name string) int {
	out, err := exec.Command("sc.exe", "start", name).CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		return 1
	}
	return 0
}

func Stop(name string) int {
	out, err := exec.Command("sc.exe", "stop", name).CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		return 1
	}
	return 0
}

func Status(name string) int {
	out, err := exec.Command("sc.exe", "query", name).CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		return 1
	}
	if !strings.Contains(string(out), "RUNNING") {
		return 2
	}
	return 0
}
