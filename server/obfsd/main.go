// Command obfsd is the AegisVPN obfuscation relay used by "obfuscated
// server" nodes. It listens on a public UDP port, XOR-unmasks incoming
// datagrams (identical transform to the client engine's internal/obfs),
// forwards them to the real WireGuard port on localhost, and masks the
// replies. WireGuard traffic behind obfsd is invisible to DPI classifiers.
//
// Topology:  client --(masked UDP)--> obfsd:443 --(plain WG UDP)--> wg:51820
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/blake2s"
)

// maskedLen mirrors the engine's internal/obfs maskedLen.
const maskedLen = 16

// keystream mirrors the engine derivation exactly.
func keystream(key string) []byte {
	sum := blake2s.Sum256([]byte("aegisvpn-obfs-v1|" + key))
	return sum[:]
}

// mask applies/removes the XOR mask over the datagram prefix (involution).
func mask(buf []byte, ks []byte) {
	n := len(buf)
	if n > maskedLen {
		n = maskedLen
	}
	for i := 0; i < n; i++ {
		buf[i] ^= ks[i]
	}
}

type relay struct {
	ks       []byte
	upstream *net.UDPAddr

	front *net.UDPConn // public masked-UDP socket
	back  *net.UDPConn // upstream plain-WG socket

	mu       sync.Mutex
	sessions map[string]*net.UDPAddr
	lastSeen map[string]time.Time
}

func (r *relay) rememberClient(addr *net.UDPAddr) {
	k := addr.String()
	r.mu.Lock()
	r.sessions[k] = addr
	r.lastSeen[k] = time.Now()
	r.mu.Unlock()
}

func (r *relay) activeClient() *net.UDPAddr {
	r.mu.Lock()
	defer r.mu.Unlock()
	var best string
	var newest time.Time
	for k, t := range r.lastSeen {
		if time.Since(t) > 5*time.Minute {
			delete(r.sessions, k)
			delete(r.lastSeen, k)
			continue
		}
		if t.After(newest) {
			newest, best = t, k
		}
	}
	if best == "" {
		return nil
	}
	return r.sessions[best]
}

// pumpFront reads masked datagrams from clients, unmasks, sends upstream.
func (r *relay) pumpFront() {
	buf := make([]byte, 65535)
	for {
		n, from, err := r.front.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		mask(pkt, r.ks) // unmask client -> WG
		if _, err := r.back.WriteToUDP(pkt, r.upstream); err != nil {
			continue
		}
		r.rememberClient(from)
	}
}

// pumpBack reads plain WG replies, masks, sends to the active client.
func (r *relay) pumpBack() {
	buf := make([]byte, 65535)
	for {
		n, _, err := r.back.ReadFromUDP(buf)
		if err != nil {
			return
		}
		client := r.activeClient()
		if client == nil {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		mask(pkt, r.ks) // mask WG -> client
		_, _ = r.front.WriteToUDP(pkt, client)
	}
}

func main() {
	listen := flag.String("listen", "0.0.0.0:443", "public masked-UDP listen address")
	upstream := flag.String("upstream", "127.0.0.1:51820", "real WireGuard UDP endpoint")
	key := flag.String("key", "", "obfuscation passphrase (must match clients; env AEGIS_OBFS_KEY)")
	flag.Parse()
	if *key == "" {
		*key = os.Getenv("AEGIS_OBFS_KEY")
	}
	if *key == "" {
		log.Fatal("obfsd: -key or AEGIS_OBFS_KEY is required")
	}
	upAddr, err := net.ResolveUDPAddr("udp", *upstream)
	if err != nil {
		log.Fatalf("obfsd: resolve upstream: %v", err)
	}
	front, err := net.ListenUDP("udp", mustAddr(*listen))
	if err != nil {
		log.Fatalf("obfsd: listen: %v", err)
	}
	back, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		log.Fatalf("obfsd: upstream socket: %v", err)
	}
	r := &relay{
		ks:       keystream(*key),
		upstream: upAddr,
		front:    front,
		back:     back,
		sessions: map[string]*net.UDPAddr{},
		lastSeen: map[string]time.Time{},
	}
	log.Printf("obfsd: masking %s -> %s (16-byte BLAKE2s XOR)", *listen, *upstream)
	go r.pumpFront()
	go r.pumpBack()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	_ = front.Close()
	_ = back.Close()
}

func mustAddr(hostport string) *net.UDPAddr {
	a, err := net.ResolveUDPAddr("udp", hostport)
	if err != nil {
		log.Fatalf("obfsd: resolve %s: %v", hostport, err)
	}
	return a
}
