//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach lets the unprivileged fallback server outlive the launcher.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
