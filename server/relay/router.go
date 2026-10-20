package main

import (
        "encoding/binary"
        "net/netip"
        "sync"
        "sync/atomic"
)

// router is a minimal userspace IPv4/IPv6 router. It holds a routing table of
// (prefix -> usTun) entries, performs longest-prefix matching on the packet
// destination, decrements TTL/hop-limit and forwards the packet to the
// selected device. Replies for the relay's own addresses (ICMP echo) are
// answered locally so clients can ping their gateway.
//
// The table shipped by default (flags --subnet-a / --subnet-b):
//
//      10.8.0.0/24  -> entry tun (subnet A: WireGuard clients live here)
//      10.8.1.0/24  -> exit-side tun (subnet B: inner WG link to the exit node)
//      0.0.0.0/0    -> exit-side tun   (everything else rides the second hop)
//      ::/0         -> exit-side tun
type router struct {
        mu     sync.RWMutex
        routes []route

        entryAddr netip.Addr // relay address on subnet A (default 10.8.0.1)
        innerAddr netip.Addr // relay address on subnet B (default 10.8.1.2)

        stats relayStats
}

type route struct {
        prefix netip.Prefix
        dev    *usTun
}

type relayStats struct {
        entryRxPackets atomic.Uint64 // packets decrypted from clients (devA -> router)
        entryTxPackets atomic.Uint64 // packets encrypted to clients (router -> devA)
        exitRxPackets  atomic.Uint64 // packets decrypted from exit node (devB -> router)
        exitTxPackets  atomic.Uint64 // packets encrypted to exit node (router -> devB)
        echoReplies    atomic.Uint64
        ttlExpired     atomic.Uint64
        noRouteDrops   atomic.Uint64
        queueDrops     atomic.Uint64
}

func newRouter(entryAddr, innerAddr netip.Addr) *router {
        return &router{entryAddr: entryAddr, innerAddr: innerAddr}
}

func (r *router) addRoute(prefix netip.Prefix, dev *usTun) {
        r.mu.Lock()
        defer r.mu.Unlock()
        r.routes = append(r.routes, route{prefix: prefix, dev: dev})
}

// routePkt performs a longest-prefix match and writes the packet to the
// chosen device queue. Returns the device the packet was sent to (nil if dropped).
func (r *router) routePkt(pkt []byte) *usTun {
        dst, ok := dstAddr(pkt)
        if !ok {
                r.stats.noRouteDrops.Add(1)
                return nil
        }

        r.mu.RLock()
        best := -1
        bestBits := -1
        for i, rt := range r.routes {
                if rt.prefix.Contains(dst) && rt.prefix.Bits() > bestBits {
                        best = i
                        bestBits = rt.prefix.Bits()
                }
        }
        var dev *usTun
        if best >= 0 {
                dev = r.routes[best].dev
        }
        r.mu.RUnlock()

        if dev == nil {
                r.stats.noRouteDrops.Add(1)
                return nil
        }

        out, ok := decrementTTL(pkt)
        if !ok {
                r.stats.ttlExpired.Add(1)
                return nil
        }
        dev.injectFromRouter(out)
        return dev
}

// forward pumps packets in one direction: reads packets written by the
// WireGuard device attached to `from`, routes them, and (for packets
// addressed to the relay itself) answers ICMP echo locally.
//
// side is "entry" (packets coming from clients on subnet A) or "exit"
// (packets coming back from the exit node on subnet B); it drives the
// direction-aware counters.
func (r *router) forward(from *usTun, side string, selfAddrs []netip.Addr, stop <-chan struct{}) {
        for {
                pkt, ok := from.consumeFromDevice(stop)
                if !ok {
                        return
                }
                if side == "entry" {
                        r.stats.entryRxPackets.Add(1)
                } else {
                        r.stats.exitRxPackets.Add(1)
                }

                dst, ok := dstAddr(pkt)
                if ok && isSelfAddr(selfAddrs, dst) {
                        if reply := icmpEchoReply(pkt); reply != nil {
                                from.injectFromRouter(reply)
                                r.stats.echoReplies.Add(1)
                        }
                        continue
                }

                if dev := r.routePkt(pkt); dev != nil {
                        if side == "entry" {
                                r.stats.exitTxPackets.Add(1)
                        } else {
                                r.stats.entryTxPackets.Add(1)
                        }
                }
        }
}

func isSelfAddr(self []netip.Addr, a netip.Addr) bool {
        for _, s := range self {
                if s == a {
                        return true
                }
        }
        return false
}

// ----------------------------------------------------------------- ip bits --

// dstAddr extracts the destination address of an IPv4 or IPv6 packet.
func dstAddr(pkt []byte) (netip.Addr, bool) {
        if len(pkt) < 1 {
                return netip.Addr{}, false
        }
        switch pkt[0] >> 4 {
        case 4:
                if len(pkt) < 20 {
                        return netip.Addr{}, false
                }
                return netip.AddrFrom4([4]byte{pkt[16], pkt[17], pkt[18], pkt[19]}), true
        case 6:
                if len(pkt) < 40 {
                        return netip.Addr{}, false
                }
                var b [16]byte
                copy(b[:], pkt[24:40])
                return netip.AddrFrom16(b), true
        }
        return netip.Addr{}, false
}

// packetLen returns the authoritative on-wire length of an IP packet using the
// header fields (guards against devices handing us over-long buffers).
func packetLen(pkt []byte) (int, bool) {
        if len(pkt) < 1 {
                return 0, false
        }
        switch pkt[0] >> 4 {
        case 4:
                if len(pkt) < 20 {
                        return 0, false
                }
                total := int(binary.BigEndian.Uint16(pkt[2:4]))
                if total < 20 || total > len(pkt) {
                        return 0, false
                }
                return total, true
        case 6:
                if len(pkt) < 40 {
                        return 0, false
                }
                total := 40 + int(binary.BigEndian.Uint16(pkt[4:6]))
                if total > len(pkt) {
                        return 0, false
                }
                return total, true
        }
        return 0, false
}

// decrementTTL decrements IPv4 TTL / IPv6 Hop Limit in place. Returns the
// packet and false when the packet must be dropped (limit exhausted or
// malformed).
func decrementTTL(pkt []byte) ([]byte, bool) {
        n, ok := packetLen(pkt)
        if !ok {
                return nil, false
        }
        pkt = pkt[:n]
        switch pkt[0] >> 4 {
        case 4:
                if pkt[8] <= 1 {
                        return nil, false
                }
                pkt[8]--
                // The IP header checksum changes with TTL; recompute it.
                binary.BigEndian.PutUint16(pkt[10:12], ipv4Checksum(pkt[:20]))
        case 6:
                if pkt[7] <= 1 {
                        return nil, false
                }
                pkt[7]--
        }
        return pkt, true
}

func ipv4Checksum(hdr []byte) uint16 {
        var sum uint32
        for i := 0; i < len(hdr); i += 2 {
                if i == 10 {
                        continue // checksum field itself
                }
                sum += uint32(hdr[i])<<8 | uint32(hdr[i+1])
        }
        for sum > 0xFFFF {
                sum = (sum & 0xFFFF) + (sum >> 16)
        }
        return ^uint16(sum)
}

// icmpEchoReply builds an ICMPv4 echo reply for a request addressed to one of
// the relay's own addresses. Returns nil for anything that is not an IPv4
// echo request (ICMPv6 NDP etc. is intentionally out of scope here).
func icmpEchoReply(pkt []byte) []byte {
        n, ok := packetLen(pkt)
        if !ok || pkt[0]>>4 != 4 || n < 28 {
                return nil
        }
        pkt = pkt[:n]
        proto := pkt[9]
        if proto != 1 { // ICMP
                return nil
        }
        if pkt[20] != 8 { // echo request
                return nil
        }

        reply := make([]byte, n)
        copy(reply, pkt)
        // swap src/dst
        copy(reply[12:16], pkt[16:20])
        copy(reply[16:20], pkt[12:16])
        // ICMP echo reply
        reply[20] = 0
        reply[21] = 0 // code
        reply[22], reply[23] = 0, 0
        binary.BigEndian.PutUint16(reply[24:26], icmpChecksum(reply[20:n]))
        binary.BigEndian.PutUint16(reply[10:12], ipv4Checksum(reply[:20]))
        return reply
}

func icmpChecksum(b []byte) uint16 {
        var sum uint32
        for i := 0; i+1 < len(b); i += 2 {
                sum += uint32(b[i])<<8 | uint32(b[i+1])
        }
        if len(b)%2 == 1 {
                sum += uint32(b[len(b)-1]) << 8
        }
        for sum > 0xFFFF {
                sum = (sum & 0xFFFF) + (sum >> 16)
        }
        return ^uint16(sum)
}
