//go:build windows

package openvpn

import (
        "fmt"
        "os/exec"
        "strings"
)

// killPID force-kills a process tree by id via taskkill.
func killPID(pid int) error {
        out, err := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid)).CombinedOutput()
        if err != nil && !strings.Contains(strings.ToLower(string(out)), "not found") {
                return err
        }
        return nil
}

// pidAlive checks the task list for the pid (os.FindProcess always succeeds
// on Windows, so it cannot be used as an existence probe).
func pidAlive(pid int) bool {
        out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").Output()
        if err != nil {
                return false
        }
        return strings.Contains(strings.ToLower(string(out)), fmt.Sprintf("%d", pid))
}
