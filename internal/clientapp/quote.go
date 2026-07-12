package clientapp

import (
	"runtime"
	"strings"
)

// BuildServiceCommand returns binPath/ExecStart that only points at a config file.
func BuildServiceCommand(exe, configPath string) string {
	if runtime.GOOS == "windows" {
		return strings.Join([]string{
			WinQuote(exe),
			"run",
			"-config", WinQuote(configPath),
		}, " ")
	}
	return strings.Join([]string{
		ShellQuote(exe),
		"run",
		"-config", ShellQuote(configPath),
	}, " ")
}

// WinQuote quotes for sc.exe binPath= argument pieces.
func WinQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// ShellQuote quotes for systemd ExecStart.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
