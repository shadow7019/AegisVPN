// Package openvpn manages a bundled or system OpenVPN client process so the
// AegisVPN apps can connect to public relays in one click — no profile
// download, no third-party client, no manual import.
//
// Binary lookup order (first hit wins):
//
//  1. AEGIS_OPENVPN environment variable
//  2. "openvpn" next to the running executable (app-bundled core)
//  3. PATH (exec.LookPath)
//  4. well-known install locations per OS
package openvpn

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// SessionInfo describes a managed OpenVPN child process.
type SessionInfo struct {
	PID        int       `json:"pid"`
	Hostname   string    `json:"hostname"`
	Endpoint   string    `json:"endpoint"`
	ConfigPath string    `json:"config_path"`
	LogPath    string    `json:"log_path"`
	StartedAt  time.Time `json:"started_at"`
	Binary     string    `json:"binary"`
}

// ErrNotFound is returned when no OpenVPN binary can be located.
var ErrNotFound = errors.New("openvpn binary not found — install it or set AEGIS_OPENVPN")

// FindBinary locates an OpenVPN client executable.
func FindBinary() (string, error) {
	if p := os.Getenv("AEGIS_OPENVPN"); p != "" {
		if isExec(p) {
			return p, nil
		}
	}
	if self, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(self), "openvpn"+ext())
		if isExec(cand) {
			return cand, nil
		}
	}
	if p, err := exec.LookPath("openvpn"); err == nil {
		return p, nil
	}
	for _, p := range wellKnown() {
		if isExec(p) {
			return p, nil
		}
	}
	return "", ErrNotFound
}

func ext() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

func wellKnown() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/sbin/openvpn",
			"/usr/local/sbin/openvpn",
			"/opt/homebrew/bin/openvpn",
			"/usr/local/bin/openvpn",
		}
	case "windows":
		var pf string
		if v := os.Getenv("ProgramFiles"); v != "" {
			pf = v
		} else {
			pf = `C:\Program Files`
		}
		return []string{filepath.Join(pf, "OpenVPN", "bin", "openvpn.exe")}
	default:
		return []string{"/usr/sbin/openvpn", "/sbin/openvpn", "/usr/bin/openvpn", "/usr/local/bin/openvpn"}
	}
}

// Manager owns at most one OpenVPN child process.
type Manager struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	log  *os.File
	info SessionInfo
	done chan struct{} // closed by the reaper once the child exited
}

// Start launches openvpn with cfgPath, logging to logDir/relay-<hostname>.log.
func (m *Manager) Start(cfgPath, logDir, hostname, endpoint string) (SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil {
		return SessionInfo{}, fmt.Errorf("a relay session is already running (pid %d) — disconnect first", m.info.PID)
	}
	bin, err := FindBinary()
	if err != nil {
		return SessionInfo{}, err
	}
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return SessionInfo{}, err
	}
	logPath := filepath.Join(logDir, "relay-"+sanitize(hostname)+".log")

	args := []string{
		"--config", cfgPath,
		"--verb", "3",
		"--connect-retry-max", "2",
		"--connect-timeout", "12",
	}
	cmd, log, err := spawn(bin, args, logPath)
	if err != nil {
		return SessionInfo{}, err
	}
	info := SessionInfo{
		PID:        cmd.Process.Pid,
		Hostname:   hostname,
		Endpoint:   endpoint,
		ConfigPath: cfgPath,
		LogPath:    logPath,
		StartedAt:  time.Now().UTC(),
		Binary:     bin,
	}
	m.cmd, m.log, m.info = cmd, log, info
	m.done = make(chan struct{})
	go func(c *exec.Cmd, d chan struct{}) {
		_ = c.Wait() // single reaper; Stop() signals via terminate()
		_ = log.Close()
		close(d)
		m.mu.Lock()
		if m.cmd == c {
			m.cmd = nil
		}
		m.mu.Unlock()
	}(cmd, m.done)
	return info, nil
}

// Stop terminates the managed child, if any.
func (m *Manager) Stop() error {
	m.mu.Lock()
	cmd, done := m.cmd, m.done
	m.mu.Unlock()
	if cmd == nil {
		return nil
	}
	if err := terminate(cmd, done); err != nil {
		return err
	}
	m.mu.Lock()
	if m.cmd == cmd {
		m.cmd = nil
	}
	m.mu.Unlock()
	return nil
}

// Active reports the current session, if one is still alive.
func (m *Manager) Active() (SessionInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil {
		return SessionInfo{}, false
	}
	select {
	case <-m.done: // exited but not yet reaped
		return SessionInfo{}, false
	default:
	}
	return m.info, true
}

// LogPath returns the active session's log file, if any.
func (m *Manager) LogPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.info.LogPath
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
			return r
		default:
			return '-'
		}
	}, s)
}
