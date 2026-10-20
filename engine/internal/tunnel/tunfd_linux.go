//go:build linux || android

package aegis

import (
	"fmt"
	"os"

	"golang.zx2c4.com/wireguard/tun"
)

// createTUNFromFile wraps tun.CreateTUNFromFile (available on linux and
// android — the Android VpnService fd path).
func createTUNFromFile(fd int, mtu int) (tun.Device, error) {
	return tun.CreateTUNFromFile(os.NewFile(uintptr(fd), "aegisvpn-tun"), mtu)
}

var _ = fmt.Sprintf
