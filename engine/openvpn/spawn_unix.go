//go:build !windows

package openvpn

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// spawn starts openvpn in its own process group so a disconnect tears down
// everything it created, with stdout/stderr captured to logPath.
func spawn(bin string, args []string, logPath string) (*exec.Cmd, *os.File, error) {
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return nil, nil, err
	}
	return cmd, log, nil
}

// terminate SIGTERMs the whole process group, escalating to SIGKILL if the
// child survives the grace period.
func terminate(cmd *exec.Cmd, done <-chan struct{}) error {
	if cmd.Process == nil {
		return nil
	}
	pgid := -cmd.Process.Pid // negative pid = every process in the group
	if err := syscall.Kill(pgid, syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(pgid, syscall.SIGKILL)
		select {
		case <-done:
			return nil
		case <-time.After(3 * time.Second):
			return errors.New("openvpn did not exit after SIGKILL")
		}
	}
}
