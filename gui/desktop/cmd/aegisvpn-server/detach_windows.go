//go:build windows

package main

import (
	"os/exec"
)

// detach is a no-op on Windows: CREATE_NEW_PROCESS_GROUP semantics are not
// required for the fallback server, and syscall.SysProcAttr has no Setpgid.
func detach(_ *exec.Cmd) {}
