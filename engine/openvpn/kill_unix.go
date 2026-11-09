//go:build !windows

package openvpn

import (
	"syscall"
	"time"
)

// killPID terminates a process by id: SIGTERM first, SIGKILL after a grace
// period.
func killPID(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
			return nil // already gone
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return nil
}

// pidAlive probes a pid with signal 0.
func pidAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
