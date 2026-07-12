//go:build windows

package service

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
)

// IsWindowsService reports whether the current process was started by the SCM.
func IsWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// Install registers a Windows service and starts it.
// binCmd is the full command line (exe + args) for sc.exe binPath=.
func Install(name, display, binCmd string) error {
	// sc.exe requires: binPath= "C:\path\exe args..."
	args := []string{"create", name, "binPath=", binCmd, "start=", "auto", "DisplayName=", display}
	out, err := exec.Command("sc.exe", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(out))
	}
	_, _ = exec.Command("sc.exe", "description", name, "WakeHub shutdown client for wakehub-server").CombinedOutput()
	// Restart on failure so transient crashes recover automatically.
	_, _ = exec.Command("sc.exe", "failure", name, "reset=", "86400", "actions=", "restart/5000/restart/10000/restart/30000").CombinedOutput()
	_, _ = exec.Command("sc.exe", "failureflag", name, "1").CombinedOutput()
	if out, err := exec.Command("sc.exe", "start", name).CombinedOutput(); err != nil {
		// Create succeeded; surface start failure so the user can check status/logs.
		return fmt.Errorf("service created but start failed: %v: %s", err, string(out))
	}
	return nil
}

func Uninstall(name string) error {
	_, _ = exec.Command("sc.exe", "stop", name).CombinedOutput()
	// Brief wait so SCM can release the binary before delete.
	time.Sleep(500 * time.Millisecond)
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

// Run registers with the Windows SCM and keeps the service in Running until stop or run returns.
func Run(name string, run Runner) error {
	return svc.Run(name, &winService{run: run})
}

type winService struct {
	run Runner
}

func (ws *winService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- ws.run(ctx)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case err := <-done:
			if err != nil {
				return true, 1
			}
			return false, 0
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(20 * time.Second):
				}
				return false, 0
			default:
				// ignore other control codes
			}
		}
	}
}
