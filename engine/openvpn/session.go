package openvpn

import (
	"encoding/json"
	"os"
	"time"
)

// PersistedSession lets a fresh CLI process stop a relay session started by
// an earlier invocation (the manager itself is in-memory only).
type PersistedSession struct {
	SessionInfo
}

// SaveSession atomically writes the session file.
func SaveSession(path string, info SessionInfo) error {
	b, err := json.MarshalIndent(PersistedSession{SessionInfo: info}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadSession reads the session file, if present.
func LoadSession(path string) (SessionInfo, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SessionInfo{}, err
	}
	var s PersistedSession
	if err := json.Unmarshal(b, &s); err != nil {
		return SessionInfo{}, err
	}
	return s.SessionInfo, nil
}

// ClearSession removes the session file (best effort).
func ClearSession(path string) { _ = os.Remove(path) }

// KillPersisted stops the PID recorded in the session file and clears it.
func KillPersisted(path string) (SessionInfo, error) {
	info, err := LoadSession(path)
	if err != nil {
		return SessionInfo{}, err
	}
	if err := killPID(info.PID); err != nil {
		return info, err
	}
	ClearSession(path)
	return info, nil
}

// PIDAlive reports whether a process id still exists (best effort).
func PIDAlive(pid int) bool { return pidAlive(pid) }

// WaitForHandshake polls a relay log for the OpenVPN success marker.
func WaitForHandshake(logPath string, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(logPath); err == nil && handshaked(string(b)) {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// handshaked reports whether the OpenVPN log shows a completed handshake.
func handshaked(log string) bool {
	return contains(log, "Initialization Sequence Completed")
}

// contains is a tiny substring helper (avoids strings import in hot paths).
func contains(s, sub string) bool {
	n, m := len(s), len(sub)
	if m == 0 || m > n {
		return false
	}
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == sub {
			return true
		}
	}
	return false
}
