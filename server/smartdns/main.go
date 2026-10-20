// Command smartdns implements AegisVPN SmartPlay: a DNS server that routes
// selected streaming domains to provider-optimized resolver IPs so
// geo-restricted services see the exit region's catalogues. All other
// queries are forwarded upstream.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// defaultStreaming lists the suffixes that get SmartPlay treatment.
var defaultStreaming = []string{
	"netflix.com", "nflxvideo.net", "nflximg.net", "nflxext.com",
	"hulu.com", "hulustream.com",
	"spotify.com", "scdn.co",
	"bbc.co.uk", "bbci.co.uk",
	"disneyplus.com", "disney-plus.net", "dssott.com",
	"primevideo.com", "amazonvideo.com",
	"hbomax.com", "hbo.com", "max.com",
	"youtube.com", "googlevideo.com",
	"crunchyroll.com",
	"peacocktv.com",
	"paramountplus.com",
	"apple.com", "mzstatic.com",
}

type resolver struct {
	mu       sync.RWMutex
	suffixes map[string]struct{}
	smartIPs []net.IP
	upstream string
}

func normalize(qname string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(qname), "."))
}

func (r *resolver) isSmart(name string) bool {
	n := normalize(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	for {
		if _, ok := r.suffixes[n]; ok {
			return true
		}
		i := strings.IndexByte(n, '.')
		if i < 0 {
			return false
		}
		n = n[i+1:]
	}
}

func (r *resolver) serve(pc *net.UDPConn, buf []byte, n int, from *net.UDPAddr) {
	if n < 12 {
		return
	}
	id := buf[0:2]
	qflags := buf[2:4]
	qdcount := int(buf[4])<<8 | int(buf[5])
	if qdcount == 0 {
		return
	}
	qname, off, err := parseName(buf, 12)
	if err != nil {
		return
	}
	_ = off
	if r.isSmart(qname) {
		// Answer A queries with the first SmartPlay resolver IP.
		if isAQuery(buf) && len(r.smartIPs) > 0 {
			resp := buildSmartResponse(id, qflags, buf[12:], r.smartIPs[0])
			_, _ = pc.WriteToUDP(resp, from)
			return
		}
	}
	// Forward upstream.
	up, err := net.DialUDP("udp", nil, mustAddr(r.upstream))
	if err != nil {
		return
	}
	defer up.Close()
	_ = up.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := up.Write(buf[:n]); err != nil {
		return
	}
	resp := make([]byte, 4096)
	m, err := up.Read(resp)
	if err != nil {
		return
	}
	_, _ = pc.WriteToUDP(resp[:m], from)
}

func isAQuery(msg []byte) bool {
	// Walk to QTYPE; only the fixed first question is inspected (clients
	// pipeline one question per packet in practice).
	off := 12
	for {
		if off >= len(msg) {
			return false
		}
		l := int(msg[off])
		if l == 0 {
			off++
			break
		}
		if l > 63 {
			return false
		}
		off += 1 + l
	}
	if off+4 > len(msg) {
		return false
	}
	qtype := int(msg[off])<<8 | int(msg[off+1])
	return qtype == 1 // A
}

func buildSmartResponse(id, qflags, question []byte, ip net.IP) []byte {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil
	}
	resp := make([]byte, 0, len(question)+16)
	resp = append(resp, id...)
	resp = append(resp, qflags[0]|0x80, qflags[1])
	resp = append(resp, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00)
	resp = append(resp, question...)
	resp = append(resp,
		0xc0, 0x0c,
		0x00, 0x01,
		0x00, 0x01,
		0x00, 0x00, 0x00, 0x3c,
		0x00, 0x04)
	resp = append(resp, ip4...)
	return resp
}

func parseName(msg []byte, off int) (string, int, error) {
	var labels []string
	for {
		if off >= len(msg) {
			return "", 0, logErr("name overruns packet")
		}
		l := int(msg[off])
		if l == 0 {
			off++
			break
		}
		if l > 63 || off+1+l > len(msg) {
			return "", 0, logErr("bad label")
		}
		labels = append(labels, string(msg[off+1:off+1+l]))
		off += 1 + l
	}
	return strings.Join(labels, "."), off, nil
}

func logErr(s string) error { return &simpleError{s} }

type simpleError struct{ s string }

func (e *simpleError) Error() string { return e.s }

func mustAddr(hostport string) *net.UDPAddr {
	a, err := net.ResolveUDPAddr("udp", hostport)
	if err != nil {
		log.Fatalf("smartdns: resolve %s: %v", hostport, err)
	}
	return a
}

func main() {
	listen := flag.String("listen", "0.0.0.0:5353", "DNS listen address")
	upstream := flag.String("upstream", "1.1.1.1:53", "recursive upstream")
	ipsFlag := flag.String("smart-ips", "", "comma-separated SmartPlay resolver IPs (env SMARTPLAY_IPS)")
	streamFlag := flag.String("streaming", "", "comma-separated streaming suffixes override")
	flag.Parse()

	ips := []net.IP{}
	candidates := strings.Split(*ipsFlag, ",")
	if env := os.Getenv("SMARTPLAY_IPS"); env != "" && *ipsFlag == "" {
		candidates = strings.Split(env, ",")
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if ip := net.ParseIP(c); ip != nil {
			ips = append(ips, ip)
		}
	}
	suffixes := map[string]struct{}{}
	list := defaultStreaming
	if *streamFlag != "" {
		list = strings.Split(*streamFlag, ",")
	}
	for _, s := range list {
		suffixes[normalize(s)] = struct{}{}
	}
	r := &resolver{suffixes: suffixes, smartIPs: ips, upstream: *upstream}

	pc, err := net.ListenUDP("udp", mustAddr(*listen))
	if err != nil {
		log.Fatalf("smartdns: listen: %v", err)
	}
	log.Printf("smartdns: serving on %s (%d smart suffixes, %d smart IPs)",
		*listen, len(suffixes), len(ips))
	go func() {
		buf := make([]byte, 4096)
		for {
			n, from, err := pc.ReadFromUDP(buf)
			if err != nil {
				return
			}
			go r.serve(pc, buf, n, from)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	_ = pc.Close()
	_ = time.Now
}
