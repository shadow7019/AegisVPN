// Command threatdns is the AegisVPN Threat Protection upstream resolver.
// It answers blocked domains (ads, trackers, malware) with 0.0.0.0/:: and
// forwards everything else to a real recursive resolver. Deployed inside
// the WireGuard subnet (10.8.0.1) so every connected client is protected
// with zero client-side configuration.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type resolver struct {
	mu      sync.RWMutex
	blocked map[string]struct{}
	upstream string
	stats   map[string]uint64
	stMu    sync.Mutex
}

func normalize(qname string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(qname), "."))
}

func (r *resolver) isBlocked(name string) bool {
	n := normalize(name)
	if n == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.blocked[n]; ok {
		return true
	}
	// Match any suffix: ads.example.com is blocked when example.com is.
	for {
		i := strings.IndexByte(n, '.')
		if i < 0 {
			return false
		}
		n = n[i+1:]
		if _, ok := r.blocked[n]; ok {
			return true
		}
	}
}

func (r *resolver) bump(kind string) {
	r.stMu.Lock()
	r.stats[kind]++
	r.stMu.Unlock()
}

// loadDir reads every *.txt file (one domain per line) into the block set.
func (r *resolver) loadDir(dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocked = map[string]struct{}{}
	if dir == "" {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		return err
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			r.blocked[normalize(line)] = struct{}{}
		}
	}
	log.Printf("threatdns: %d blocked domains loaded", len(r.blocked))
	return nil
}

// serve handles one DNS query packet.
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
	// Parse the first question.
	qname, off, err := parseName(buf, 12)
	if err != nil {
		return
	}
	qtype := int(buf[off])<<8 | int(buf[off+1])
	qclass := int(buf[off+2])<<8 | int(buf[off+3])
	end := off + 4

	if r.isBlocked(qname) {
		r.bump("blocked")
		resp := buildBlockedResponse(id, qflags, buf[12:end])
		_, _ = pc.WriteToUDP(resp, from)
		return
	}
	r.bump("forwarded")
	// Forward to upstream and relay the answer verbatim.
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
	_ = qtype
	_ = qclass
}

// parseName decodes a DNS wire-format name starting at off.
func parseName(msg []byte, off int) (string, int, error) {
	var labels []string
	for {
		if off >= len(msg) {
			return "", 0, fmt.Errorf("name overruns packet")
		}
		l := int(msg[off])
		if l == 0 {
			off++
			break
		}
		if l > 63 || off+1+l > len(msg) {
			return "", 0, fmt.Errorf("bad label")
		}
		labels = append(labels, string(msg[off+1:off+1+l]))
		off += 1 + l
	}
	return strings.Join(labels, "."), off, nil
}

// buildBlockedResponse crafts an A/AAAA answer with 0.0.0.0 (NOERROR, one
// answer record of the requested type).
func buildBlockedResponse(id, qflags, question []byte) []byte {
	resp := make([]byte, 0, len(question)+16)
	resp = append(resp, id...)
	resp = append(resp, qflags[0]|0x80, qflags[1]) // QR=1, keep RD
	resp = append(resp, 0x00, 0x01)                // QDCOUNT 1
	resp = append(resp, 0x00, 0x01)                // ANCOUNT 1
	resp = append(resp, 0x00, 0x00, 0x00, 0x00)    // NSCOUNT, ARCOUNT 0
	resp = append(resp, question...)
	resp = append(resp,
		0x00, 0x01, // NAME pointer to offset 12
		0x00, 0x01, // TYPE A
		0x00, 0x01, // CLASS IN
		0x00, 0x00, 0x00, 0x3c, // TTL 60
		0x00, 0x04, // RDLENGTH 4
		0, 0, 0, 0) // RDATA 0.0.0.0
	return resp
}

func mustAddr(hostport string) *net.UDPAddr {
	a, err := net.ResolveUDPAddr("udp", hostport)
	if err != nil {
		log.Fatalf("threatdns: resolve %s: %v", hostport, err)
	}
	return a
}

func main() {
	listen := flag.String("listen", "0.0.0.0:53", "DNS listen address")
	upstream := flag.String("upstream", "1.1.1.1:53", "recursive upstream")
	blocks := flag.String("blocklists", "/blocklists", "directory of *.txt blocklists (one domain per line)")
	flag.Parse()

	r := &resolver{upstream: *upstream, stats: map[string]uint64{}}
	if err := r.loadDir(*blocks); err != nil {
		log.Printf("threatdns: blocklists: %v", err)
	}
	// Reload every 6 hours.
	go func() {
		for range time.Tick(6 * time.Hour) {
			if err := r.loadDir(*blocks); err != nil {
				log.Printf("threatdns: reload: %v", err)
			}
		}
	}()

	pc, err := net.ListenUDP("udp", mustAddr(*listen))
	if err != nil {
		log.Fatalf("threatdns: listen: %v", err)
	}
	log.Printf("threatdns: serving on %s (upstream %s)", *listen, r.upstream)
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
}
