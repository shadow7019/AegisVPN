//go:build darwin

package router

import (
        "fmt"
        "net"
        "net/netip"
        "os"
        "strconv"
        "strings"
)

// darwinManager drives the host network via ifconfig, route and
// networksetup; the kill switch uses Packet Filter (pf) through the
// /etc/pf.anchors/aegisvpn anchor loaded from /etc/pf.conf.
type darwinManager struct {
        journal journal // Up/Down lifecycle mutations
        ks      journal // kill switch mutations
        split   journal // split tunnel mutations

        tun       string
        endpoints []netip.AddrPort

        defGW, defIface string
        defGW6, defIface6 string

        dnsService string
        dnsDone    bool

        pfWasEnabled bool
        pfActive     bool
        pfConfBackup []byte
}

const pfAnchorPath = "/etc/pf.anchors/aegisvpn"
const pfConfPath = "/etc/pf.conf"

func newPlatform() Manager { return &darwinManager{} }

func (m *darwinManager) SetEndpoints(eps []netip.AddrPort) { m.endpoints = eps }

func (m *darwinManager) SetFwmark(uint32, bool) {} // macOS: route-based only

// defaultGateway parses `route -n get default` (or -inet6) for gw + iface.
func (m *darwinManager) defaultGateway(v6 bool) (gw, iface string) {
        args := []string{"-n", "get", "default"}
        if v6 {
                args = []string{"-n", "-inet6", "get", "default"}
        }
        out, err := run("route", args...)
        if err != nil {
                return "", ""
        }
        for _, line := range strings.Split(out, "\n") {
                f := strings.Fields(strings.TrimSpace(line))
                if len(f) >= 2 {
                        switch f[0] {
                        case "gateway:":
                                gw = f[1]
                        case "interface:":
                                iface = f[1]
                        }
                }
        }
        return gw, iface
}

func (m *darwinManager) Up(tunName string, addrs []net.IPNet, routes []net.IPNet, mtu int) error {
        m.tun = tunName
        for _, a := range addrs {
                var err error
                if a.IP.To4() != nil {
                        // Point-to-point utun: use the local address as peer (self-as-peer)
                        // so the interface comes up; actual egress is driven by -interface
                        // routes added below.
                        args := []string{tunName, "inet", a.IP.String(), a.IP.String()}
                        if mtu > 0 {
                                args = append(args, "mtu", strconv.Itoa(mtu))
                        }
                        args = append(args, "up")
                        _, err = run("ifconfig", args...)
                } else {
                        _, err = run("ifconfig", tunName, "inet6", a.IP.String(), "up")
                }
                if err != nil {
                        return fmt.Errorf("ifconfig %s: %w", a.String(), err)
                }
        }

        m.defGW, m.defIface = m.defaultGateway(false)
        m.defGW6, m.defIface6 = m.defaultGateway(true)

        for _, ep := range m.endpoints {
                m.addEndpointRoute(netipToIP(ep.Addr()))
        }
        for _, r := range routes {
                if err := m.addTunnelRoute(r, tunName); err != nil {
                        return err
                }
        }
        return nil
}

// addTunnelRoute installs `route -n add [-inet6] -net <cidr> -interface <tun>`.
// A default route is split into two halves (0.0.0.0/1 + 128.0.0.0/1) because
// the BSD route daemon refuses an interface default route outright.
func (m *darwinManager) addTunnelRoute(r net.IPNet, tunName string) error {
        v6 := r.IP.To4() == nil
        famArgs := func(dst string) []string {
                if v6 {
                        return []string{"-n", "-inet6", "add", "-net", dst, "-interface", tunName}
                }
                return []string{"-n", "add", "-net", dst, "-interface", tunName}
        }
        install := func(dst string) error {
                if _, err := run("route", famArgs(dst)...); err != nil && !strings.Contains(err.Error(), "File exists") && !strings.Contains(err.Error(), "exists") {
                        return fmt.Errorf("route add %s: %w", dst, err)
                }
                m.journal.pushCmd("route add "+dst, append([]string{"route", "-n", "delete"}, func() []string {
                        if v6 {
                                return []string{"-inet6", dst}
                        }
                        return []string{dst}
                }()...)...)
                return nil
        }
        switch {
        case !v6 && isDefaultRoute(r):
                for _, half := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
                        if err := install(half); err != nil {
                                return err
                        }
                }
        case v6 && isDefaultRoute(r):
                for _, half := range []string{"::/1", "8000::/1"} {
                        if err := install(half); err != nil {
                                return err
                        }
                }
        default:
                return install(r.String())
        }
        return nil
}

func (m *darwinManager) addEndpointRoute(ip net.IP) {
        v6 := ip.To4() == nil
        gw, iface := m.defGW, m.defIface
        if v6 {
                gw, iface = m.defGW6, m.defIface6
        }
        if gw == "" && iface == "" {
                return
        }
        host := ip.String()
        args := []string{"-n"}
        if v6 {
                args = append(args, "-inet6")
        }
        args = append(args, "add", "-host", host)
        if gw != "" {
                args = append(args, gw)
        } else {
                args = append(args, "-interface", iface)
        }
        if _, err := run("route", args...); err != nil {
                return
        }
        del := []string{"-n"}
        if v6 {
                del = append(del, "-inet6")
        }
        del = append(del, "delete", "-host", host)
        m.journal.pushCmd("endpoint route "+host, append([]string{"route"}, del...)...)
}

func (m *darwinManager) Down() error {
        m.split.rollback()
        m.ks.rollback()
        errs := m.journal.rollback()
        m.tun = ""
        return firstErr(errs)
}

// primaryService maps the default-route interface to a networksetup service
// name via `networksetup -listallhardwareports`.
func (m *darwinManager) primaryService() (string, error) {
        if m.defIface == "" {
                return "", fmt.Errorf("no default route interface")
        }
        out, err := run("networksetup", "-listallhardwareports")
        if err != nil {
                return "", err
        }
        service, device := "", ""
        for _, line := range strings.Split(out, "\n") {
                line = strings.TrimSpace(line)
                if strings.HasPrefix(line, "Hardware Port:") {
                        service = strings.TrimSpace(strings.TrimPrefix(line, "Hardware Port:"))
                } else if strings.HasPrefix(line, "Device:") {
                        device = strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
                        if device == m.defIface && service != "" {
                                return service, nil
                        }
                }
        }
        return "", fmt.Errorf("no networksetup service for %s", m.defIface)
}

func (m *darwinManager) SetDNS(servers []string) error {
        if len(servers) == 0 {
                return nil
        }
        svc, err := m.primaryService()
        if err != nil {
                return fmt.Errorf("DNS takeover: %w", err)
        }
        args := append([]string{"-setdnsservers", svc}, servers...)
        if _, err := run("networksetup", args...); err != nil {
                return err
        }
        m.dnsService = svc
        m.dnsDone = true
        return nil
}

func (m *darwinManager) ResetDNS() error {
        if !m.dnsDone || m.dnsService == "" {
                return nil
        }
        _, err := run("networksetup", "-setdnsservers", m.dnsService, "Empty")
        m.dnsDone = false
        return err
}

// KillSwitch toggles the pf kill switch:
//   - writes /etc/pf.anchors/aegisvpn (block all except utun + endpoint UDP),
//   - wires the anchor into /etc/pf.conf (original saved in memory),
//   - `pfctl -f /etc/pf.conf` then `pfctl -E` to enable,
//   - on disable: restores pf.conf, reloads, and `pfctl -d` if we enabled pf.
func (m *darwinManager) KillSwitch(on bool, tunName string) error {
        if !on {
                if !m.pfActive {
                        return nil
                }
                var errs []error
                if len(m.pfConfBackup) > 0 {
                        if err := os.WriteFile(pfConfPath, m.pfConfBackup, 0o644); err != nil {
                                errs = append(errs, err)
                        }
                        runQuiet("pfctl", "-f", pfConfPath)
                }
                if !m.pfWasEnabled {
                        runQuiet("pfctl", "-d")
                }
                m.pfActive = false
                return firstErr(errs)
        }

        // Rules are evaluated last-match-wins in pf: pass rules first, block last.
        var b strings.Builder
        b.WriteString("# AegisVPN kill switch anchor\n")
        b.WriteString("set skip on lo0\n")
        for _, ep := range m.endpoints {
                if netipToIP(ep.Addr()).To4() != nil {
                        fmt.Fprintf(&b, "pass out proto udp to %s port %d\n", ep.Addr(), ep.Port())
                } else {
                        fmt.Fprintf(&b, "pass out proto udp6 to %s port %d\n", ep.Addr(), ep.Port())
                }
        }
        fmt.Fprintf(&b, "pass out on %s all\n", tunName)
        fmt.Fprintf(&b, "pass in on %s all\n", tunName)
        b.WriteString("block drop out all\n")

        if err := os.WriteFile(pfAnchorPath, []byte(b.String()), 0o644); err != nil {
                return fmt.Errorf("write %s: %w", pfAnchorPath, err)
        }
        // Enable pf early if it is off, so `pfctl -f` activates the rules.
        info, _ := run("pfctl", "-s", "info")
        m.pfWasEnabled = strings.Contains(info, "Status: Enabled")
        if len(m.pfConfBackup) == 0 {
                if data, err := os.ReadFile(pfConfPath); err == nil {
                        m.pfConfBackup = data
                }
        }
        conf := string(m.pfConfBackup)
        if !strings.Contains(conf, `anchor "aegisvpn"`) {
                conf += "\nanchor \"aegisvpn\"\nload anchor \"aegisvpn\" from \"" + pfAnchorPath + "\"\n"
                if err := os.WriteFile(pfConfPath, []byte(conf), 0o644); err != nil {
                        return fmt.Errorf("write %s: %w", pfConfPath, err)
                }
        }
        if _, err := run("pfctl", "-f", pfConfPath); err != nil {
                return fmt.Errorf("pfctl -f: %w", err)
        }
        if _, err := run("pfctl", "-E"); err != nil {
                return fmt.Errorf("pfctl -E: %w", err)
        }
        m.pfActive = true
        return nil
}

func (m *darwinManager) SplitTunnel(exclude, include []net.IPNet) error {
        for _, r := range exclude {
                v6 := r.IP.To4() == nil
                gw := m.defGW
                if v6 {
                        gw = m.defGW6
                }
                if gw == "" {
                        return fmt.Errorf("split tunnel exclude: no default gateway")
                }
                args := []string{"-n"}
                if v6 {
                        args = append(args, "-inet6")
                }
                args = append(args, "add", "-net", r.String(), gw)
                if _, err := run("route", args...); err != nil && !strings.Contains(err.Error(), "exists") {
                        return err
                }
                del := []string{"-n"}
                if v6 {
                        del = append(del, "-inet6")
                }
                del = append(del, "delete", "-net", r.String())
                m.split.pushCmd("exclude route "+r.String(), append([]string{"route"}, del...)...)
        }
        for _, r := range include {
                if err := m.addTunnelRoute(r, m.tun); err != nil {
                        return err
                }
        }
        return nil
}
