// Package aegislib is the gomobile binding of the AegisVPN engine for
// Android. Android's VpnService provides the tun fd; this package owns the
// real WireGuard device on top of it (docs/PROTOCOL.md).
//go:build android

package aegislib

import (
	"encoding/json"
	"fmt"
	"sync"

	aegis "github.com/shadow7019/AegisVPN/engine/internal/tunnel"
)

var (
	mu     sync.Mutex
	client *aegis.Client
)

// Start establishes the tunnel over the VpnService fd. wgConfig is the full
// wg-quick profile text (AegisObfs extensions supported). Android owns
// routes/DNS via the Builder; Go owns the WireGuard device.
func Start(fd int64, wgConfig string, obfsEnabled bool, obfsKey string) error {
	mu.Lock()
	defer mu.Unlock()
	if fd <= 0 {
		return fmt.Errorf("aegislib: invalid tun fd %d", fd)
	}
	if client == nil {
		c, err := aegis.New(androidRoot())
		if err != nil {
			return err
		}
		client = c
	}
	prof, err := aegis.ParseProfile(wgConfig)
	if err != nil {
		return err
	}
	return aegis.StartWithFD(client, int(fd), prof, obfsEnabled, obfsKey)
}

// Stop tears the tunnel down.
func Stop() error {
	mu.Lock()
	defer mu.Unlock()
	if client == nil {
		return nil
	}
	err := client.Disconnect()
	return err
}

// StatusJSON returns the serialized aegis.Status.
func StatusJSON() string {
	mu.Lock()
	c := client
	mu.Unlock()
	if c == nil {
		b, _ := json.Marshal(aegis.Status{State: aegis.StateDisconnected})
		return string(b)
	}
	b, err := json.Marshal(c.Status())
	if err != nil {
		return "{}"
	}
	return string(b)
}

// Version returns the engine version string.
func Version() string { return aegis.Version }
