//go:build !android

// Package aegislib is the gomobile binding of the AegisVPN engine. The
// real implementation is built for GOOS=android only; on other platforms
// every call returns ErrNotAndroid (use cmd/aegisvpn instead).
package aegislib

import "errors"

// ErrNotAndroid reports that the binding is only functional on Android.
var ErrNotAndroid = errors.New("aegislib: the gomobile binding is only available on Android")

// Start is a no-op off Android.
func Start(fd int64, wgConfig string, obfsEnabled bool, obfsKey string) error {
	return ErrNotAndroid
}

// Stop is a no-op off Android.
func Stop() error { return ErrNotAndroid }

// StatusJSON returns a disconnected status off Android.
func StatusJSON() string { return `{"state":"disconnected"}` }

// Version returns the engine version.
func Version() string { return "1.0.0" }
