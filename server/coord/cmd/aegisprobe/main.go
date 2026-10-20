// Command aegisprobe is the node-side status responder for the AegisVPN
// coordination server's UDP load probe (see server/coord/probe.go).
//
// It is deployed next to every WireGuard node (on the linuxserver/wireguard
// containers it runs from /config/custom-services.d via the s6 custom service
// hook) and answers coordination probes with:
//
//      "AE1R" | nonce uint32 BE | load uint8 (0-100) | sessions uint32 BE
//
// load  = 1-minute load average normalized by CPU count, clamped to 0-100.
// sessions = number of WireGuard peers currently registered on wg0.
package main

import (
        "flag"
        "log"
        "net"
        "os"
        "os/exec"
        "runtime"
        "strconv"
        "strings"
        "sync"
        "time"
)

var (
        flagListen   = flag.String("listen", envOr("AEGIS_PROBE_LISTEN", ":51823"), "UDP listen address for status probes")
        flagWGIfName = flag.String("wg-iface", envOr("AEGIS_PROBE_WG_IFACE", "wg0"), "WireGuard interface to count sessions on")
        flagWGPath   = flag.String("wg-tool", "/usr/bin/wg", "path to the wg userspace tool")
)

func envOr(key, def string) string {
        if v := os.Getenv(key); v != "" {
                return v
        }
        return def
}

func main() {
        flag.Parse()

        udpAddr, err := net.ResolveUDPAddr("udp", *flagListen)
        if err != nil {
                log.Fatalf("aegisprobe: resolve %s: %v", *flagListen, err)
        }
        conn, err := net.ListenUDP("udp", udpAddr)
        if err != nil {
                log.Fatalf("aegisprobe: listen %s: %v", *flagListen, err)
        }
        log.Printf("aegisprobe: answering status probes on %s (wg iface %s)", *flagListen, *flagWGIfName)

        cache := &sessionCache{iface: *flagWGIfName, wgPath: *flagWGPath, ttl: 10 * time.Second}

        buf := make([]byte, 64)
        for {
                n, remote, err := conn.ReadFromUDP(buf)
                if err != nil {
                        log.Printf("aegisprobe: read: %v", err)
                        continue
                }
                // Valid probe: "AE1D" | nonce uint32 BE (8 bytes total).
                if n < 8 || buf[0] != 'A' || buf[1] != 'E' || buf[2] != '1' || buf[3] != 'D' {
                        continue
                }
                reply := make([]byte, 13)
                copy(reply[0:4], []byte{'A', 'E', '1', 'R'})
                copy(reply[4:8], buf[4:8])
                reply[8] = byte(loadPercent())
                reply[9], reply[10], reply[11], reply[12] = cache.sessionsBE()
                if _, err := conn.WriteToUDP(reply, remote); err != nil {
                        log.Printf("aegisprobe: reply to %s: %v", remote, err)
                }
        }
}

// loadPercent returns the normalized 1-minute load as an integer percent.
func loadPercent() int {
        data, err := os.ReadFile("/proc/loadavg")
        if err != nil {
                return 0
        }
        fields := strings.Fields(string(data))
        if len(fields) == 0 {
                return 0
        }
        l1, err := strconv.ParseFloat(fields[0], 64)
        if err != nil {
                return 0
        }
        pct := int(l1 / float64(runtime.NumCPU()) * 100)
        if pct < 0 {
                pct = 0
        }
        if pct > 100 {
                pct = 100
        }
        return pct
}

// sessionCache memoizes `wg show <iface> peers` output so bursts of probes do
// not shell out repeatedly.
type sessionCache struct {
        mu      sync.Mutex
        iface   string
        wgPath  string
        ttl     time.Duration
        value   uint32
        expires time.Time
}

func (c *sessionCache) sessionsBE() (byte, byte, byte, byte) {
        c.mu.Lock()
        defer c.mu.Unlock()
        if time.Now().After(c.expires) {
                c.value = c.refresh()
                c.expires = time.Now().Add(c.ttl)
        }
        v := c.value
        return byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)
}

func (c *sessionCache) refresh() uint32 {
        out, err := exec.Command(c.wgPath, "show", c.iface, "peers").Output()
        if err != nil {
                // No wg tool, no interface yet, or not permitted: report 0 sessions
                // rather than failing the whole probe.
                return 0
        }
        count := 0
        for _, line := range strings.Split(string(out), "\n") {
                if strings.TrimSpace(line) != "" {
                        count++
                }
        }
        if count < 0 || count > 1<<31-1 {
                count = 0
        }
        return uint32(count) & 0xFFFFFFFF
}
