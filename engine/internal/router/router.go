// Package router implements the per-OS network takeover layer: interface
// addressing, MTU, routes, DNS, kill switch and split tunneling.
//
// Every platform file (router_linux.go, router_windows.go, router_darwin.go)
// implements the Manager interface by shelling out to the platform's real
// network configuration tools (ip/iptables/nft, netsh/route/powershell,
// ifconfig/route/networksetup/pfctl). All mutations are recorded in a journal
// so Down() can restore the pre-connect state exactly, in reverse order.
package router

import (
        "fmt"
        "net"
        "net/netip"
        "os/exec"
        "strings"
)

// Manager owns the host network configuration while a tunnel is up.
type Manager interface {
        // Up configures addressing (addrs), MTU and routes (routes) on the given
        // tunnel interface.
        Up(tunName string, addrs []net.IPNet, routes []net.IPNet, mtu int) error
        // Down removes everything applied by Up (in reverse order) plus DNS,
        // kill switch and split tunnel changes.
        Down() error
        // SetDNS performs system DNS takeover with the given resolvers.
        SetDNS(servers []string) error
        // ResetDNS restores the pre-takeover resolver configuration.
        ResetDNS() error
        // KillSwitch enables or disables the firewall kill switch (block all
        // egress except the tunnel interface, its DNS and the VPN endpoint).
        KillSwitch(on bool, tunName string) error
        // SplitTunnel installs exclude routes (traffic bypassing the tunnel,
        // via the original gateway) and include routes (traffic forced through
        // the tunnel interface).
        SplitTunnel(exclude, include []net.IPNet) error
}

// EndpointSetter is an optional capability allowing the client to report the
// resolved VPN endpoint addresses before Up/KillSwitch so the router can
// punch exceptions for the (encrypted) control traffic.
type EndpointSetter interface {
        SetEndpoints(eps []netip.AddrPort)
}

// FwmarkSetter is an optional capability telling the router whether the
// WireGuard bind successfully set a socket fwmark; Linux policy routing
// ("ip rule ... fwmark") is only safe when the mark is actually applied.
type FwmarkSetter interface {
        SetFwmark(mark uint32, enabled bool)
}

// New returns the platform manager (build-tagged per OS).
func New() Manager {
        return newPlatform()
}

// netipToIP converts a netip.Addr into a plain net.IP (unmapping IPv4).
func netipToIP(a netip.Addr) net.IP {
        if !a.IsValid() {
                return nil
        }
        return net.IP(a.Unmap().AsSlice())
}

// journal records undo actions; Down() replays them LIFO.
type journal struct {
        undos []func() error
}

func (j *journal) push(fn func() error) {
        j.undos = append(j.undos, fn)
}

func (j *journal) pushCmd(desc string, undoArgs ...string) {
        j.push(func() error {
                if len(undoArgs) == 0 {
                        return nil
                }
                _, err := run(undoArgs[0], undoArgs[1:]...)
                if err != nil {
                        return fmt.Errorf("undo %s: %w", desc, err)
                }
                return nil
        })
}

func (j *journal) rollback() []error {
        var errs []error
        for i := len(j.undos) - 1; i >= 0; i-- {
                if err := j.undos[i](); err != nil {
                        errs = append(errs, err)
                }
        }
        j.undos = nil
        return errs
}

// run executes a command and returns its trimmed combined output.
func run(name string, args ...string) (string, error) {
        cmd := exec.Command(name, args...)
        out, err := cmd.CombinedOutput()
        trimmed := strings.TrimSpace(string(out))
        if err != nil {
                return trimmed, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, trimmed)
        }
        return trimmed, nil
}

// runQuiet executes a command, swallowing "already exists"-style errors.
func runQuiet(name string, args ...string) {
        _, _ = run(name, args...)
}

// isDefaultRoute reports whether n is 0.0.0.0/0 or ::/0.
func isDefaultRoute(n net.IPNet) bool {
        if n.IP == nil || n.Mask == nil {
                return false
        }
        ones, bits := n.Mask.Size()
        return ones == 0 && bits > 0
}

// cidrs parses strings into net.IPNet values, skipping invalid entries.
func cidrs(list []string) []net.IPNet {
        var out []net.IPNet
        for _, s := range list {
                if _, n, err := net.ParseCIDR(s); err == nil {
                        out = append(out, *n)
                }
        }
        return out
}

// dotted converts a netmask into dotted-quad notation (Windows netsh/route).
func dotted(mask net.IPMask) string {
        return net.IP(mask).String()
}

// firstErr returns the first error in a list (or nil).
func firstErr(errs []error) error {
        for _, e := range errs {
                if e != nil {
                        return e
                }
        }
        return nil
}
