//go:build !linux && !android

package aegis

import (
	"errors"

	"golang.zx2c4.com/wireguard/tun"
)

// createTUNFromFile is unavailable off-linux: Windows/macOS create their
// tun devices by name via tun.CreateTUN.
func createTUNFromFile(fd int, mtu int) (tun.Device, error) {
	return nil, errors.New("tunnel: fd-based tun creation is only supported on linux/android")
}
