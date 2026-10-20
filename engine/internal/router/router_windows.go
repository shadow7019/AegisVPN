//go:build windows

package router

import (
        "fmt"
        "net"
        "net/netip"
        "strconv"
        "strings"
)

// windowsManager drives the host network via netsh, route.exe and PowerShell
// (Get-NetAdapter / New-NetFirewallRule). The kill switch uses Windows
// Defender Firewall rules: a block-all outbound rule plus targeted allow
// rules for loopback, the tunnel interface and the VPN endpoint UDP.
type windowsManager struct {
        journal journal // Up/Down lifecycle mutations
        ks      journal // kill switch mutations
        split   journal // split tunnel mutations
        dnsDone bool

        tun       string
        ifIndex   int
        endpoints []netip.AddrPort

        origGW    string // original IPv4 default gateway
        origIf    int    // original default-route interface index
        tunV4IP   string // first IPv4 address assigned to the tunnel
}

func newPlatform() Manager { return &windowsManager{} }

func (m *windowsManager) SetEndpoints(eps []netip.AddrPort) { m.endpoints = eps }

func (m *windowsManager) SetFwmark(uint32, bool) {} // Windows: endpoint /32 routes suffice

// interfaceIndex resolves the adapter index via PowerShell.
func (m *windowsManager) interfaceIndex(tunName string) (int, error) {
        out, err := run("powershell", "-NoProfile", "-Command",
                fmt.Sprintf(`(Get-NetAdapter -InterfaceAlias '%s').ifIndex`, tunName))
        if err != nil {
                return 0, fmt.Errorf("resolve ifIndex for %s: %w", tunName, err)
        }
        idx, err := strconv.Atoi(strings.TrimSpace(out))
        if err != nil {
                return 0, fmt.Errorf("ifIndex parse %q: %w", out, err)
        }
        return idx, nil
}

// origGateway parses `route print -4 0.0.0.0` for the active default route
// (lowest metric, not the tunnel interface).
func (m *windowsManager) origGateway(tunIdx int) (gw string, ifIdx int) {
        out, _ := run("route", "print", "-4", "0.0.0.0")
        best := -1
        for _, line := range strings.Split(out, "\n") {
                f := strings.Fields(line)
                if len(f) < 5 || f[0] != "0.0.0.0" || f[1] != "0.0.0.0" {
                        continue
                }
                idx, err1 := strconv.Atoi(f[3])
                metric, err2 := strconv.Atoi(f[4])
                if err1 != nil || err2 != nil || idx == tunIdx {
                        continue
                }
                if best < 0 || metric < best {
                        best, gw, ifIdx = metric, f[2], idx
                }
        }
        return gw, ifIdx
}

func (m *windowsManager) Up(tunName string, addrs []net.IPNet, routes []net.IPNet, mtu int) error {
        m.tun = tunName
        idx, err := m.interfaceIndex(tunName)
        if err != nil {
                return err
        }
        m.ifIndex = idx

        var v4 string
        for _, a := range addrs {
                if ip4 := a.IP.To4(); ip4 != nil {
                        if v4 == "" {
                                v4 = ip4.String()
                        }
                        args := []string{"interface", "ip", "set", "address", "name=" + tunName,
                                "source=static", "addr=" + ip4.String(), "mask=" + dotted(a.Mask)}
                        if _, err := run("netsh", args...); err != nil {
                                return fmt.Errorf("netsh set address %s: %w", ip4, err)
                        }
                        m.journal.pushCmd("netsh set address", "netsh", "interface", "ip",
                                "set", "address", "name="+tunName, "source=dhcp")
                } else {
                        if _, err := run("netsh", "interface", "ipv6", "set", "address",
                                tunName, a.IP.String()); err != nil {
                                return fmt.Errorf("netsh ipv6 set address %s: %w", a.IP, err)
                        }
                        m.journal.pushCmd("netsh ipv6 set address", "netsh", "interface", "ipv6",
                                "delete", "address", tunName, a.IP.String())
                }
        }
        m.tunV4IP = v4

        if mtu > 0 {
                runQuiet("netsh", "interface", "ipv4", "set", "subinterface", tunName,
                        "mtu="+strconv.Itoa(mtu), "store=active")
                runQuiet("netsh", "interface", "ipv6", "set", "subinterface", tunName,
                        "mtu="+strconv.Itoa(mtu), "store=active")
        }

        m.origGW, m.origIf = m.origGateway(idx)

        // Endpoint host routes: WG control traffic must bypass the tunnel.
        for _, ep := range m.endpoints {
                m.addEndpointRoute(netipToIP(ep.Addr()))
        }

        for _, r := range routes {
                if err := m.addTunnelRoute(r, idx); err != nil {
                        return err
                }
        }
        return nil
}

func (m *windowsManager) addTunnelRoute(r net.IPNet, tunIdx int) error {
        ip4 := r.IP.To4()
        fam := ""
        mask := dotted(r.Mask)
        dst := r.IP.String()
        if ip4 == nil {
                fam = "-6"
                dst = r.String() // route.exe v6 wants prefix notation
                mask = strconv.Itoa(maskOnes(r.Mask))
        }
        gw := m.tunV4IP
        if gw == "" {
                gw = "0.0.0.0"
        }
        args := []string{"add", dst}
        if mask != "" {
                args = append(args, "mask", mask)
        }
        args = append(args, gw, "metric", "1", "IF", strconv.Itoa(tunIdx))
        if fam != "" {
                args = append([]string{fam}, args...)
        }
        if _, err := run("route", args...); err != nil && !strings.Contains(err.Error(), "already") {
                return fmt.Errorf("route add %s: %w", r.String(), err)
        }
        del := []string{"delete", dst}
        if fam != "" {
                del = append([]string{fam}, del...)
        }
        m.journal.pushCmd("route add "+r.String(), append([]string{"route"}, del...)...)
        return nil
}

func (m *windowsManager) addEndpointRoute(ip net.IP) {
        host := ip.String()
        mask := "255.255.255.255"
        fam := []string{}
        if ip.To4() == nil {
                fam = []string{"-6"}
                host = ip.String() + "/128"
                mask = ""
        }
        if m.origGW == "" {
                return
        }
        args := append([]string{}, fam...)
        args = append(args, "add", host)
        if mask != "" {
                args = append(args, "mask", mask)
        }
        args = append(args, m.origGW, "metric", "1")
        if m.origIf > 0 {
                args = append(args, "IF", strconv.Itoa(m.origIf))
        }
        if _, err := run("route", args...); err != nil {
                return
        }
        del := append(append([]string{}, fam...), "delete", host)
        m.journal.pushCmd("endpoint route "+host, append([]string{"route"}, del...)...)
}

func maskOnes(mask net.IPMask) int {
        ones, _ := mask.Size()
        return ones
}

func (m *windowsManager) Down() error {
        m.split.rollback()
        m.ks.rollback()
        errs := m.journal.rollback()
        m.tun = ""
        return firstErr(errs)
}

func (m *windowsManager) SetDNS(servers []string) error {
        if len(servers) == 0 || m.tun == "" {
                return nil
        }
        if _, err := run("netsh", "interface", "ip", "set", "dns", "name="+m.tun,
                "static", servers[0], "primary"); err != nil {
                return fmt.Errorf("netsh set dns: %w", err)
        }
        for i, s := range servers[1:] {
                runQuiet("netsh", "interface", "ip", "add", "dns", "name="+m.tun, s,
                        "index="+strconv.Itoa(i+2))
        }
        runQuiet("netsh", "interface", "ipv6", "set", "dns", "name="+m.tun,
                "static", servers[0], "primary")
        m.dnsDone = true
        return nil
}

func (m *windowsManager) ResetDNS() error {
        if !m.dnsDone || m.tun == "" {
                return nil
        }
        runQuiet("netsh", "interface", "ip", "set", "dnsservers", "name="+m.tun, "source=dhcp")
        runQuiet("netsh", "interface", "ipv6", "set", "dnsservers", "name="+m.tun, "source=dhcp")
        m.dnsDone = false
        return nil
}

// KillSwitch toggles Windows Defender Firewall rules. A block-all outbound
// rule is installed together with allow rules for loopback, the tunnel
// interface (via InterfaceAlias) and the VPN endpoint UDP ports.
func (m *windowsManager) KillSwitch(on bool, tunName string) error {
        if !on {
                errs := m.ks.rollback()
                return firstErr(errs)
        }
        if _, err := run("netsh", "advfirewall", "firewall", "add", "rule",
                "name=\"AegisVPN-KS-Block\"", "dir=out", "action=block"); err != nil {
                return fmt.Errorf("firewall block-all rule: %w", err)
        }
        m.ks.pushCmd("remove block rule", "netsh", "advfirewall", "firewall", "delete",
                "rule", "name=\"AegisVPN-KS-Block\"")

        allow := []struct {
                name string
                args []string
        }{
                {"AegisVPN-KS-Allow-Loopback", []string{"dir=out", "action=allow",
                        "remoteip=127.0.0.0/8,::1"}},
        }
        for _, ep := range m.endpoints {
                allow = append(allow, struct {
                        name string
                        args []string
                }{
                        "AegisVPN-KS-Allow-Endpoint",
                        []string{"dir=out", "action=allow", "protocol=udp",
                                "remoteip=" + ep.Addr().String(), "remoteport=" + strconv.Itoa(int(ep.Port()))},
                })
        }
        for _, a := range allow {
                args := append([]string{"advfirewall", "firewall", "add", "rule",
                        "name=\"" + a.name + "\""}, a.args...)
                if _, err := run("netsh", args...); err != nil {
                        return err
                }
                m.ks.pushCmd("remove "+a.name, append([]string{"netsh", "advfirewall",
                        "firewall", "delete", "rule", "name=\"" + a.name + "\""})...)
        }
        // Allow everything on the tunnel interface (needs PowerShell).
        if _, err := run("powershell", "-NoProfile", "-Command",
                fmt.Sprintf(`New-NetFirewallRule -DisplayName 'AegisVPN-KS-Allow-Tunnel' -Direction Outbound -Action Allow -InterfaceAlias '%s' | Out-Null`, tunName)); err != nil {
                return fmt.Errorf("tunnel allow rule: %w", err)
        }
        m.ks.pushCmd("remove tunnel allow rule", "powershell", "-NoProfile", "-Command",
                `Remove-NetFirewallRule -DisplayName 'AegisVPN-KS-Allow-Tunnel' -ErrorAction SilentlyContinue`)
        return nil
}

func (m *windowsManager) SplitTunnel(exclude, include []net.IPNet) error {
        for _, r := range exclude {
                if m.origGW == "" {
                        return fmt.Errorf("split tunnel exclude: no original gateway detected")
                }
                ip4 := r.IP.To4()
                fam := ""
                dst := r.IP.String()
                mask := dotted(r.Mask)
                if ip4 == nil {
                        fam = "-6"
                        dst = r.String()
                        mask = strconv.Itoa(maskOnes(r.Mask))
                }
                args := []string{}
                if fam != "" {
                        args = append(args, fam)
                }
                args = append(args, "add", dst, "mask", mask, m.origGW, "metric", "1")
                if m.origIf > 0 {
                        args = append(args, "IF", strconv.Itoa(m.origIf))
                }
                if _, err := run("route", args...); err != nil && !strings.Contains(err.Error(), "already") {
                        return err
                }
                del := []string{}
                if fam != "" {
                        del = append(del, fam)
                }
                del = append(del, "delete", dst)
                m.split.pushCmd("exclude route "+r.String(), append([]string{"route"}, del...)...)
        }
        for _, r := range include {
                if err := m.addTunnelRoute(r, m.ifIndex); err != nil {
                        return err
                }
        }
        return nil
}
