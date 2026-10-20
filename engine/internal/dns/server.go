// Package dns implements the AegisVPN Threat Protection DNS forwarder.
//
// It is a real, full DNS forwarder listening on UDP+TCP 127.0.0.1:<port>
// (default 53):
//   - Queries whose QNAME matches the blocklist (embedded starter list plus
//     optional appRoot/blocklists/*.txt files, one domain per line) are
//     sinkholed: A → 0.0.0.0, AAAA → ::, other types → NXDOMAIN-style
//     NameError responses.
//   - Queries matching a SmartPlay rule are forwarded to the rule's provider
//     DNS servers instead of the default upstream.
//   - Everything else is forwarded to the upstream resolver over plain UDP.
//
// Message parsing/building uses golang.org/x/net/dns/dnsmessage.
package dns

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Rule overrides the upstream for domains matching Suffix (SmartPlay).
type Rule struct {
	Suffix  string   // e.g. "netflix.com"
	Servers []string // upstream DNS IPs for matching queries
}

// Stats is a snapshot of forwarder counters.
type Stats struct {
	Queries uint64 `json:"queries"`
	Blocked uint64 `json:"blocked"`
}

// Server is the Threat Protection DNS forwarder.
type Server struct {
	mu       sync.Mutex
	port     int
	upstream string
	block    map[string]bool
	rules    []Rule

	udp    net.PacketConn
	tcp    net.Listener
	closed bool

	wg sync.WaitGroup

	queries uint64
	blocked uint64
}

// New creates a forwarder (not yet listening) on 127.0.0.1:port.
// port 53 is the standard; upstream is the resolver queries are forwarded to.
func New(port int, upstream string) *Server {
	if port <= 0 {
		port = 53
	}
	if upstream == "" {
		upstream = "10.8.0.1"
	}
	return &Server{
		port:     port,
		upstream: upstream,
		block:    make(map[string]bool),
	}
}

// LoadBlocklists loads every *.txt file under dir (one domain per line,
// '#' comments allowed) on top of the embedded starter list.
func (s *Server) LoadBlocklists(dir string) error {
	s.mu.Lock()
	for _, d := range StarterBlocklist {
		s.block[strings.ToLower(d)] = true
	}
	s.mu.Unlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		s.mu.Lock()
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if i := strings.IndexByte(line, '#'); i >= 0 {
				line = strings.TrimSpace(line[:i])
			}
			if line != "" {
				s.block[strings.ToLower(line)] = true
			}
		}
		s.mu.Unlock()
	}
	return nil
}

// SetRules installs SmartPlay upstream override rules.
func (s *Server) SetRules(rules []Rule) {
	s.mu.Lock()
	s.rules = rules
	s.mu.Unlock()
}

// Addr is the listen address (127.0.0.1:<port>).
func (s *Server) Addr() string { return fmt.Sprintf("127.0.0.1:%d", s.port) }

// Upstream returns the configured default upstream resolver.
func (s *Server) Upstream() string { return s.upstream }

// Enable starts the UDP and TCP listeners.
func (s *Server) Enable() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("dns: server closed")
	}
	if s.udp != nil {
		return nil // already enabled
	}
	addr := s.Addr()
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("dns: UDP listen %s: %w", addr, err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		pc.Close()
		return fmt.Errorf("dns: TCP listen %s: %w", addr, err)
	}
	s.udp, s.tcp = pc, ln
	s.wg.Add(2)
	go s.serveUDP(pc)
	go s.serveTCP(ln)
	return nil
}

// Disable stops the listeners (queries in flight are dropped).
func (s *Server) Disable() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.udp != nil {
		s.udp.Close()
		s.udp = nil
	}
	if s.tcp != nil {
		s.tcp.Close()
		s.tcp = nil
	}
	return nil
}

// Enabled reports whether the forwarder is listening.
func (s *Server) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.udp != nil
}

// Close disables the forwarder permanently.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return s.Disable()
}

// Stats returns the current counters.
func (s *Server) Stats() Stats {
	return Stats{Queries: atomic.LoadUint64(&s.queries), Blocked: atomic.LoadUint64(&s.blocked)}
}

func (s *Server) serveUDP(pc net.PacketConn) {
	defer s.wg.Done()
	buf := make([]byte, 4096)
	for {
		n, raddr, err := pc.ReadFrom(buf)
		if err != nil {
			return // closed
		}
		if n < 12 {
			continue
		}
		query := make([]byte, n)
		copy(query, buf[:n])
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			resp := s.handle(query)
			pc.SetWriteDeadline(time.Now().Add(3 * time.Second))
			pc.WriteTo(resp, raddr)
		}()
	}
}

func (s *Server) serveTCP(ln net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // closed
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleTCPConn(conn)
		}()
	}
}

func (s *Server) handleTCPConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	var lenBuf [2]byte
	for {
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		n := binary.BigEndian.Uint16(lenBuf[:])
		query := make([]byte, n)
		if _, err := io.ReadFull(conn, query); err != nil {
			return
		}
		resp := s.handle(query)
		out := make([]byte, 2+len(resp))
		binary.BigEndian.PutUint16(out, uint16(len(resp)))
		copy(out[2:], resp)
		if _, err := conn.Write(out); err != nil {
			return
		}
	}
}

// handle processes one raw DNS query and returns the raw response.
func (s *Server) handle(query []byte) []byte {
	atomic.AddUint64(&s.queries, 1)
	var p dnsmessage.Parser
	hdr, err := p.Start(query)
	if err != nil {
		return servfail(hdr.ID)
	}
	q, err := p.Question()
	if err != nil {
		return servfail(hdr.ID)
	}
	name := strings.ToLower(strings.TrimSuffix(q.Name.String(), "."))

	s.mu.Lock()
	blocked := s.block[name] || matchSuffix(s.block, name)
	var ruleServers []string
	for _, r := range s.rules {
		if name == r.Suffix || strings.HasSuffix(name, "."+r.Suffix) {
			ruleServers = r.Servers
			break
		}
	}
	s.mu.Unlock()

	if blocked {
		atomic.AddUint64(&s.blocked, 1)
		if resp := blockedResponse(hdr.ID, q); resp != nil {
			return resp
		}
		// Non-address query types get a NameError (NXDOMAIN-style).
		return nameError(hdr.ID, q)
	}

	upstream := s.upstream
	if len(ruleServers) > 0 {
		upstream = ruleServers[0]
	}
	if resp := s.forward(query, upstream); resp != nil {
		return resp
	}
	return servfail(hdr.ID)
}

// matchSuffix reports whether name or any parent of it is in the set
// (e.g. set has "doubleclick.net", query is "ads.doubleclick.net").
func matchSuffix(set map[string]bool, name string) bool {
	for {
		i := strings.IndexByte(name, '.')
		if i < 0 || i+1 >= len(name) {
			return false
		}
		name = name[i+1:]
		if set[name] {
			return true
		}
	}
}

// forward relays the raw query to upstream over plain UDP.
func (s *Server) forward(query []byte, upstream string) []byte {
	host := upstream
	if _, _, err := net.SplitHostPort(upstream); err != nil {
		host = net.JoinHostPort(upstream, "53")
	}
	c, err := net.DialTimeout("udp", host, 3*time.Second)
	if err != nil {
		return nil
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(query); err != nil {
		return nil
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil || n < 12 {
		return nil
	}
	return buf[:n]
}

// blockedResponse sinkholes A → 0.0.0.0 and AAAA → ::.
func blockedResponse(id uint16, q dnsmessage.Question) []byte {
	var body dnsmessage.ResourceBody
	switch q.Type {
	case dnsmessage.TypeA:
		body = &dnsmessage.AResource{A: [4]byte{0, 0, 0, 0}}
	case dnsmessage.TypeAAAA:
		body = &dnsmessage.AAAAResource{AAAA: [16]byte{}}
	default:
		return nil
	}
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:                 id,
			Response:           true,
			Authoritative:      true,
			RecursionAvailable: true,
			RCode:              dnsmessage.RCodeSuccess,
		},
		Questions: []dnsmessage.Question{q},
		Answers: []dnsmessage.Resource{{
			Header: dnsmessage.ResourceHeader{
				Name: q.Name, Type: q.Type, Class: dnsmessage.ClassINET, TTL: 300,
			},
			Body: body,
		}},
	}
	out, err := msg.Pack()
	if err != nil {
		return nil
	}
	return out
}

// nameError builds an NXDOMAIN-style response.
func nameError(id uint16, q dnsmessage.Question) []byte {
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID: id, Response: true, Authoritative: true,
			RecursionAvailable: true, RCode: dnsmessage.RCodeNameError,
		},
		Questions: []dnsmessage.Question{q},
	}
	out, err := msg.Pack()
	if err != nil {
		return nil
	}
	return out
}

// servfail builds a minimal SERVFAIL response.
func servfail(id uint16) []byte {
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{ID: id, Response: true, RCode: dnsmessage.RCodeServerFailure},
	}
	out, _ := msg.Pack()
	return out
}
