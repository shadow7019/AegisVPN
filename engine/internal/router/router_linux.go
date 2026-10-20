//go:build linux

package router

import (
        "fmt"
        "net"
        "net/netip"
        "os"
        "os/exec"
        "strconv"
        "strings"
)

// linuxManager drives the host network via `ip` (iproute2), iptables (with an
// nftables fallback), and /etc/resolv.conf (+ optional resolvectl).
//
// Route design (mirrors wg-quick):
//   - If the WireGuard bind applied fwmark <mark>, add policy routing:
//     "ip rule add not fwmark <mark> table <mark>" plus a default route via
//     the tunnel in table <mark>. All normal (unmarked) traffic is then
//     attracted into the tunnel, while the WG process's own encrypted UDP
//     (marked sockets) keeps using the main table and escapes via the
//     original gateway.
//   - Endpoint host routes (/32 via the original gateway) are always added to
//     the main table as a belt-and-braces escape hatch.
//   - If policy routing is unavailable (no CAP_NET_ADMIN), fall back to plain
//     default routes via the tunnel with a high metric, still protected by
//     the endpoint host routes.
type linuxManager struct {
        journal  journal // Up/Down lifecycle mutations
        ks       journal // kill switch mutations
        split    journal // split tunnel mutations
        dnsSaved []byte
        dnsDone  bool
        dnsIface string

        tun         string
        endpoints   []netip.AddrPort
        fwmark      uint32
        fwmarkOn    bool
        defGW4, dev4 string
        defGW6, dev6 string
}

func newPlatform() Manager { return &linuxManager{} }

func (m *linuxManager) SetEndpoints(eps []netip.AddrPort) { m.endpoints = eps }

func (m *linuxManager) SetFwmark(mark uint32, enabled bool) {
        m.fwmark, m.fwmarkOn = mark, enabled
}

// defaultRoute parses `ip -4/-6 route show default` into (gateway, dev).
func defaultRoute(v6 bool) (gw, dev string) {
        args := []string{"-4", "route", "show", "default"}
        if v6 {
                args = []string{"-6", "route", "show", "default"}
        }
        out, err := run("ip", args...)
        if err != nil {
                return "", ""
        }
        fields := strings.Fields(out)
        for i, f := range fields {
                if f == "via" && i+1 < len(fields) {
                        gw = fields[i+1]
                }
                if f == "dev" && i+1 < len(fields) {
                        dev = fields[i+1]
                }
        }
        return gw, dev
}

func (m *linuxManager) Up(tunName string, addrs []net.IPNet, routes []net.IPNet, mtu int) error {
        m.tun = tunName
        if mtu > 0 {
                runQuiet("ip", "link", "set", "dev", tunName, "mtu", strconv.Itoa(mtu))
        }
        for _, a := range addrs {
                cidr := a.String()
                if _, err := run("ip", "addr", "add", cidr, "dev", tunName); err != nil && !strings.Contains(err.Error(), "File exists") {
                        return fmt.Errorf("ip addr add %s: %w", cidr, err)
                }
                m.journal.pushCmd("ip addr add "+cidr, "ip", "addr", "del", cidr, "dev", tunName)
        }
        runQuiet("ip", "link", "set", "dev", tunName, "up")

        m.defGW4, m.dev4 = defaultRoute(false)
        m.defGW6, m.dev6 = defaultRoute(true)

        // Endpoint host routes: encrypted WG traffic must bypass the tunnel.
        for _, ep := range m.endpoints {
                m.addEndpointHostRoute(netipToIP(ep.Addr()), tunName)
        }

        // AllowedIPs routes into the tunnel.
        policyOK := false
        defaults := false
        for _, r := range routes {
                if isDefaultRoute(r) {
                        defaults = true
                        continue
                }
                if _, err := run("ip", "route", "add", r.String(), "dev", tunName); err != nil && !strings.Contains(err.Error(), "File exists") {
                        return fmt.Errorf("ip route add %s: %w", r.String(), err)
                }
                m.journal.pushCmd("ip route add "+r.String(), "ip", "route", "del", r.String())
        }
        if m.fwmarkOn && m.fwmark > 0 {
                mark := strconv.FormatUint(uint64(m.fwmark), 10)
                if _, err := run("ip", "-4", "rule", "add", "not", "fwmark", mark, "table", mark); err == nil {
                        runQuiet("ip", "-6", "rule", "add", "not", "fwmark", mark, "table", mark)
                        if _, err := run("ip", "-4", "route", "add", "0.0.0.0/0", "dev", tunName, "table", mark); err == nil {
                                runQuiet("ip", "-6", "route", "add", "::/0", "dev", tunName, "table", mark)
                                policyOK = true
                                undo := func() error {
                                        runQuiet("ip", "-6", "route", "del", "::/0", "dev", tunName, "table", mark)
                                        runQuiet("ip", "-4", "route", "del", "0.0.0.0/0", "dev", tunName, "table", mark)
                                        runQuiet("ip", "-6", "rule", "del", "not", "fwmark", mark, "table", mark)
                                        _, err := run("ip", "-4", "rule", "del", "not", "fwmark", mark, "table", mark)
                                        return err
                                }
                                m.journal.push(undo)
                        } else {
                                // Roll the rule back; fall through to plain routing.
                                runQuiet("ip", "-4", "rule", "del", "not", "fwmark", mark, "table", mark)
                                runQuiet("ip", "-6", "rule", "del", "not", "fwmark", mark, "table", mark)
                        }
                }
        }
        if defaults && !policyOK {
                // Fallback: plain default route via the tunnel. Endpoint host routes
                // above keep the WG control traffic out of the tunnel.
                for _, r := range routes {
                        if !isDefaultRoute(r) {
                                continue
                        }
                        fam := "-4"
                        nn := "0.0.0.0/0"
                        if r.IP.To4() == nil {
                                fam = "-6"
                                nn = "::/0"
                        }
                        if _, err := run("ip", fam, "route", "add", nn, "dev", tunName, "metric", "4242"); err != nil && !strings.Contains(err.Error(), "File exists") {
                                return fmt.Errorf("ip %s route add %s: %w", fam, nn, err)
                        }
                        m.journal.pushCmd("ip "+fam+" route add "+nn, "ip", fam, "route", "del", nn)
                }
        }
        return nil
}

func (m *linuxManager) addEndpointHostRoute(ip net.IP, tunName string) {
        fam := "-4"
        gw, dev := m.defGW4, m.dev4
        if ip.To4() == nil {
                fam = "-6"
                gw, dev = m.defGW6, m.dev6
        }
        if dev == "" || dev == tunName {
                return // cannot escape through the tunnel; nothing to anchor to
        }
        host := ip.String() + "/32"
        if ip.To4() == nil {
                host = ip.String() + "/128"
        }
        args := []string{fam, "route", "add", host}
        if gw != "" {
                args = append(args, "via", gw)
        }
        args = append(args, "dev", dev)
        if _, err := run("ip", args...); err != nil && !strings.Contains(err.Error(), "File exists") {
                return
        }
        del := []string{fam, "route", "del", host}
        if gw != "" {
                del = append(del, "via", gw)
        }
        del = append(del, "dev", dev)
        m.journal.pushCmd("ip "+fam+" route add "+host, append([]string{"ip"}, del...)...)
}

func (m *linuxManager) Down() error {
        m.split.rollback()
        m.ks.rollback()
        errs := m.journal.rollback()
        m.tun = ""
        return firstErr(errs)
}

func (m *linuxManager) SetDNS(servers []string) error {
        if len(servers) == 0 {
                return nil
        }
        // resolvectl attempt (systemd-resolved): scoped to the tunnel interface.
        if m.tun != "" {
                if _, err := exec.LookPath("resolvectl"); err == nil {
                        args := append([]string{"dns", m.tun}, servers...)
                        runQuiet("resolvectl", args...)
                }
        }
        data, err := os.ReadFile("/etc/resolv.conf")
        if err == nil {
                m.dnsSaved = data
        }
        var b strings.Builder
        b.WriteString("# Generated by AegisVPN — original preserved in memory\n")
        for _, s := range servers {
                b.WriteString("nameserver " + s + "\n")
        }
        if err := os.WriteFile("/etc/resolv.conf", []byte(b.String()), 0o644); err != nil {
                return fmt.Errorf("write /etc/resolv.conf: %w", err)
        }
        m.dnsDone = true
        m.dnsIface = m.tun
        return nil
}

func (m *linuxManager) ResetDNS() error {
        if m.dnsIface != "" {
                runQuiet("resolvectl", "revert", m.dnsIface)
        }
        if m.dnsDone && len(m.dnsSaved) > 0 {
                if err := os.WriteFile("/etc/resolv.conf", m.dnsSaved, 0o644); err != nil {
                        return fmt.Errorf("restore /etc/resolv.conf: %w", err)
                }
        }
        m.dnsDone = false
        return nil
}

// KillSwitch installs (on=true) or removes (on=false) firewall rules that
// drop all OUTPUT/FORWARD traffic except: loopback, the tunnel interface,
// established/related flows, and UDP to the VPN endpoints. Uses iptables
// when present, else nftables.
func (m *linuxManager) KillSwitch(on bool, tunName string) error {
        if !on {
                errs := m.ks.rollback()
                return firstErr(errs)
        }
        if _, err := exec.LookPath("iptables"); err == nil {
                return m.killSwitchIPTables(tunName)
        }
        if _, err := exec.LookPath("nft"); err == nil {
                return m.killSwitchNFT(tunName)
        }
        return fmt.Errorf("kill switch: neither iptables nor nft available")
}

func (m *linuxManager) ksChain(tunName string, v6 bool) error {
        ipt := "iptables"
        if v6 {
                ipt = "ip6tables"
        }
        // Dedicated chain keeps removal trivial and idempotent.
        runQuiet(ipt, "-N", "AEGISVPN-KS")
        runQuiet(ipt, "-F", "AEGISVPN-KS")
        accepts := [][]string{
                {"-o", "lo", "-j", "ACCEPT"},
                {"-o", tunName, "-j", "ACCEPT"},
                {"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"},
        }
        for _, ep := range m.endpoints {
                if (netipToIP(ep.Addr()).To4() == nil) != v6 {
                        continue
                }
                accepts = append(accepts, []string{"-p", "udp", "-d", ep.Addr().String(), "--dport", strconv.Itoa(int(ep.Port())), "-j", "ACCEPT"})
        }
        accepts = append(accepts, []string{"-j", "DROP"})
        for _, a := range accepts {
                args := append([]string{"-A", "AEGISVPN-KS"}, a...)
                if _, err := run(ipt, args...); err != nil {
                        return err
                }
        }
        // Hook the chain into OUTPUT and FORWARD.
        for _, hook := range []string{"OUTPUT", "FORWARD"} {
                if _, err := run(ipt, "-C", hook, "-j", "AEGISVPN-KS"); err != nil {
                        if _, err := run(ipt, "-I", hook, "1", "-j", "AEGISVPN-KS"); err != nil {
                                return fmt.Errorf("%s -I %s: %w", ipt, hook, err)
                        }
                        h := hook
                        m.ks.push(func() error {
                                _, err := run(ipt, "-D", h, "-j", "AEGISVPN-KS")
                                return err
                        })
                }
        }
        m.ks.push(func() error {
                runQuiet(ipt, "-F", "AEGISVPN-KS")
                _, err := run(ipt, "-X", "AEGISVPN-KS")
                return err
        })
        return nil
}

func (m *linuxManager) killSwitchIPTables(tunName string) error {
        if err := m.ksChain(tunName, false); err != nil {
                return err
        }
        if err := m.ksChain(tunName, true); err != nil {
                return err
        }
        return nil
}

func (m *linuxManager) killSwitchNFT(tunName string) error {
        var epV4, epV6 []string
        for _, ep := range m.endpoints {
                s := fmt.Sprintf("udp daddr %s dport %d accept", ep.Addr().String(), ep.Port())
                if netipToIP(ep.Addr()).To4() != nil {
                        epV4 = append(epV4, s)
                } else {
                        epV6 = append(epV6, s)
                }
        }
        ruleset := func(fam, eps, iface string) string {
                return fmt.Sprintf(`table %s aegisvpn {
  chain output {
    type filter hook output priority 0; policy drop;
    oifname "lo" accept
    oifname "%s" accept
    ct state established,related accept
    %s
  }
  chain forward {
    type filter hook forward priority 0; policy drop;
    oifname "lo" accept
    oifname "%s" accept
    ct state established,related accept
  }
}
`, fam, iface, eps, iface)
        }
        runQuiet("nft", "delete", "table", "ip", "aegisvpn")
        if err := nftApply(ruleset("ip", strings.Join(epV4, "\n    "), tunName)); err != nil {
                return err
        }
        m.ks.push(func() error {
                _, err := run("nft", "delete", "table", "ip", "aegisvpn")
                return err
        })
        runQuiet("nft", "delete", "table", "ip6", "aegisvpn")
        if err := nftApply(ruleset("ip6", strings.Join(epV6, "\n    "), tunName)); err != nil {
                return err
        }
        m.ks.push(func() error {
                _, err := run("nft", "delete", "table", "ip6", "aegisvpn")
                return err
        })
        return nil
}

func nftApply(ruleset string) error {
        f, err := os.CreateTemp("", "aegisvpn-nft-*.conf")
        if err != nil {
                return err
        }
        path := f.Name()
        if _, err := f.WriteString(ruleset); err != nil {
                f.Close()
                os.Remove(path)
                return err
        }
        f.Close()
        defer os.Remove(path)
        _, err = run("nft", "-f", path)
        return err
}

func (m *linuxManager) SplitTunnel(exclude, include []net.IPNet) error {
        for _, r := range exclude {
                gw, dev := m.defGW4, m.dev4
                fam := "-4"
                if r.IP.To4() == nil {
                        gw, dev = m.defGW6, m.dev6
                        fam = "-6"
                }
                if dev == "" {
                        return fmt.Errorf("split tunnel exclude: no original default route detected")
                }
                args := []string{fam, "route", "add", r.String()}
                if gw != "" {
                        args = append(args, "via", gw)
                }
                args = append(args, "dev", dev)
                if _, err := run("ip", args...); err != nil && !strings.Contains(err.Error(), "File exists") {
                        return err
                }
                undo := []string{"ip", fam, "route", "del", r.String()}
                m.split.pushCmd("exclude route "+r.String(), undo...)
                // If policy routing is active, the exclude must also exist in the
                // fwmark table (longest-prefix beats the default-via-tun there).
                if m.fwmarkOn && m.fwmark > 0 {
                        mark := strconv.FormatUint(uint64(m.fwmark), 10)
                        args2 := append([]string{fam, "route", "add", r.String(), "table", mark}, func() []string {
                                if gw != "" {
                                        return []string{"via", gw, "dev", dev}
                                }
                                return []string{"dev", dev}
                        }()...)
                        runQuiet("ip", args2...)
                        undo2 := []string{"ip", fam, "route", "del", r.String(), "table", mark}
                        m.split.pushCmd("exclude route (table) "+r.String(), undo2...)
                }
        }
        for _, r := range include {
                if _, err := run("ip", "route", "add", r.String(), "dev", m.tun); err != nil && !strings.Contains(err.Error(), "File exists") {
                        return err
                }
                m.split.pushCmd("include route "+r.String(), "ip", "route", "del", r.String())
        }
        return nil
}
