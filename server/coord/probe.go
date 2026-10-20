package main

import (
        "encoding/binary"
        "log"
        "net"
        "strconv"
        "sync"
        "time"
)

// UDP status-probe wire format (little datagram, big-endian fields):
//
//      request: "AE1D" | nonce uint32                                  (8 bytes)
//      reply:   "AE1R" | nonce uint32 | load uint8 | sessions uint32    (11 bytes)
//
// load is 0..100 (percent). The responder is cmd/aegisprobe, deployed next to
// every WireGuard node (linuxserver/wireguard custom-services.d) and it
// reports machine load plus active WireGuard peer sessions. Nodes that do not
// answer keep their previously known load value.
var (
        probeReqMagic = [4]byte{'A', 'E', '1', 'D'}
        probeRepMagic = [4]byte{'A', 'E', '1', 'R'}
)

type prober struct {
        store   *store
        timeout time.Duration
        nonce   uint32
        mu      sync.Mutex
}

func (p *prober) loop(interval time.Duration) {
        // Probe once immediately so /v1/nodes has fresh data fast, then tick.
        p.round()
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for range ticker.C {
                p.round()
        }
}

func (p *prober) round() {
        nodes := p.store.probedNodes()
        if len(nodes) == 0 {
                return
        }
        var wg sync.WaitGroup
        for _, n := range nodes {
                wg.Add(1)
                go func(n Node) {
                        defer wg.Done()
                        p.probeOne(n)
                }(n)
        }
        wg.Wait()
}

func (p *prober) probeOne(n Node) {
        p.mu.Lock()
        p.nonce++
        nonce := p.nonce
        p.mu.Unlock()

        req := make([]byte, 8)
        copy(req, probeReqMagic[:])
        binary.BigEndian.PutUint32(req[4:], nonce)

        addr := net.JoinHostPort(n.Host, strconv.Itoa(n.ProbePort))
        udpAddr, err := net.ResolveUDPAddr("udp", addr)
        if err != nil {
                log.Printf("probe: resolve %s: %v", addr, err)
                return
        }

        conn, err := net.DialUDP("udp", nil, udpAddr)
        if err != nil {
                log.Printf("probe: dial %s: %v", addr, err)
                return
        }
        defer conn.Close()
        _ = conn.SetDeadline(time.Now().Add(p.timeout))

        start := time.Now()
        if _, err := conn.Write(req); err != nil {
                log.Printf("probe: write %s: %v", addr, err)
                return
        }

        buf := make([]byte, 64)
        read, err := conn.Read(buf)
        rttMs := int(time.Since(start) / time.Millisecond)
        if err != nil {
                // Unanswered probe (timeout or ICMP unreachable): node keeps the
                // last known load. Nothing is invented here.
                return
        }
        if read < 11 || buf[0] != probeRepMagic[0] || buf[1] != probeRepMagic[1] ||
                buf[2] != probeRepMagic[2] || buf[3] != probeRepMagic[3] {
                return
        }
        if binary.BigEndian.Uint32(buf[4:8]) != nonce {
                return
        }
        load := float64(buf[8]) / 100.0
        sessions := binary.BigEndian.Uint32(buf[9:13])
        p.store.updateLoad(n.ID, load, rttMs, int(sessions))
}
