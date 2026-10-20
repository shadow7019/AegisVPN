// Package aegis is the public entry point of the AegisVPN engine library.
//
// Desktop GUIs (Wails) and privileged helpers import this package to
// establish real WireGuard tunnels via internal/tunnel. The Android
// binding lives in cmd/aegislib; the daemon CLI in cmd/aegisvpn.
package aegis

import (
	tunnel "github.com/shadow7019/AegisVPN/engine/internal/tunnel"
)

// Version is the engine semantic version.
const Version = tunnel.Version

// Tunnel states.
const (
	StateDisconnected  = tunnel.StateDisconnected
	StateConnecting    = tunnel.StateConnecting
	StateConnected     = tunnel.StateConnected
	StateDisconnecting = tunnel.StateDisconnecting
)

// Status is the public tunnel status snapshot.
type Status = tunnel.Status

// ConnectOpts configures Connect.
type ConnectOpts = tunnel.ConnectOpts

// Client is the engine library handle.
type Client = tunnel.Client

// New creates a Client rooted at appRoot.
var New = tunnel.New
