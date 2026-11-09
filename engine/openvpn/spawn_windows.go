//go:build windows

package openvpn

import (
	"os"
	"os/exec"
	"time"
)

// spawn starts openvpn with output captured to logPath. Windows has no
// process groups in the Go stdlib; openvpn.exe runs as a plain child.
func spawn(bin string, args []string, logPath string) (*exec.Cmd, *os.File, error) {
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return nil, nil, err
	}
	return cmd, log, nil
}

// terminate kills the child process (openvpn.exe removes its TAP/Wintun
// adapter state on exit).
func terminate(cmd *exec.Cmd, done <-chan struct{}) error {
	if cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return nil // process object is dead even if the reaper raced us
	}
}
