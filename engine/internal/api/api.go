// Package api implements the AegisVPN local control daemon: a JSON line
// protocol over 127.0.0.1:5599 (Windows) or a unix socket (macOS/Linux),
// exactly as specified in docs/PROTOCOL.md section 2.
//
// Request:  {"op": "...", "args": {...}}\n
// Response: {"ok": true|false, "data": {...}, "error": "..."}\n
package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/shadow7019/AegisVPN/engine/internal/features"
	tunnel "github.com/shadow7019/AegisVPN/engine/internal/tunnel"
)

// ControlPort is the localhost TCP port used on Windows.
const ControlPort = 5599

// Server wires a tunnel.Client to the control protocol.
type Server struct {
	client   *tunnel.Client
	hibp     *features.HIBPClient
	mesh     *features.MeshnetClient
	sockPath string // unix socket path (darwin/linux)
	ln       net.Listener
	mu       sync.Mutex
	closed   bool
}

// Listen starts the control listener. addr overrides the default transport.
func Listen(client *tunnel.Client) (*Server, error) {
	s := &Server{client: client, hibp: features.NewHIBPClient()}
	coord := os.Getenv("AEGIS_COORD_URL")
	if coord == "" {
		coord = "https://coord.aegisvpn.net"
	}
	s.mesh = features.NewMeshnetClient(coord)
	var err error
	if runtime.GOOS == "windows" {
		s.ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ControlPort))
	} else {
		root := client.Root()
		if mkErr := os.MkdirAll(root, 0o700); mkErr != nil {
			return nil, mkErr
		}
		s.sockPath = filepath.Join(root, "engine.sock")
		_ = os.Remove(s.sockPath) // stale socket from a previous run
		s.ln, err = net.Listen("unix", s.sockPath)
	}
	if err != nil {
		return nil, err
	}
	go s.acceptLoop()
	return s, nil
}

// SocketPath returns the unix socket path (non-Windows).
func (s *Server) SocketPath() string { return s.sockPath }

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		go s.serve(conn)
	}
}

func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	w := bufio.NewWriter(conn)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		resp := s.dispatch(line)
		if err := writeJSONLine(w, resp); err != nil {
			return
		}
	}
}

func writeJSONLine(w *bufio.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte(`{"ok":false,"error":"marshal response"}`)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return w.Flush()
}

type request struct {
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
}

type response struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func ok(data any) response        { return response{OK: true, Data: data} }
func fail(err error) response     { return response{OK: false, Error: err.Error()} }
func failStr(msg string) response { return response{OK: false, Error: msg} }

func decode(args json.RawMessage, v any) error {
	if len(args) == 0 {
		return nil
	}
	return json.Unmarshal(args, v)
}

func (s *Server) dispatch(line []byte) response {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return failStr("invalid request json")
	}
	switch req.Op {
	case "status":
		return ok(s.client.Status())

	case "connect":
		var opts tunnel.ConnectOpts
		if err := decode(req.Args, &opts); err != nil {
			return fail(err)
		}
		if err := s.client.Connect(opts); err != nil {
			return fail(err)
		}
		return ok(s.client.Status())

	case "disconnect":
		if err := s.client.Disconnect(); err != nil {
			return fail(err)
		}
		return ok(s.client.Status())

	case "killswitch":
		var a struct {
			Enabled bool `json:"enabled"`
		}
		if err := decode(req.Args, &a); err != nil {
			return fail(err)
		}
		if err := s.client.SetKillSwitch(a.Enabled); err != nil {
			return fail(err)
		}
		return ok(nil)

	case "threat_protection":
		var a struct {
			Enabled bool `json:"enabled"`
		}
		if err := decode(req.Args, &a); err != nil {
			return fail(err)
		}
		if err := s.client.SetThreatProtection(a.Enabled); err != nil {
			return fail(err)
		}
		return ok(nil)

	case "split_tunnel":
		var a struct {
			Exclude []string `json:"exclude"`
			Include []string `json:"include"`
		}
		if err := decode(req.Args, &a); err != nil {
			return fail(err)
		}
		if err := s.client.SetSplitTunnel(a.Exclude, a.Include); err != nil {
			return fail(err)
		}
		return ok(nil)

	case "dns":
		var a struct {
			Servers []string `json:"servers"`
		}
		if err := decode(req.Args, &a); err != nil {
			return fail(err)
		}
		if err := s.client.ApplyDNS(a.Servers); err != nil {
			return fail(err)
		}
		return ok(nil)

	case "nodes":
		nodes, err := s.client.Registry().List(false)
		if err != nil {
			return fail(err)
		}
		return ok(nodes)

	case "breach_check":
		var a struct {
			Email string `json:"email"`
		}
		if err := decode(req.Args, &a); err != nil {
			return fail(err)
		}
		if a.Email == "" {
			return failStr("email is required")
		}
		breaches, err := s.hibp.BreachedAccount(a.Email)
		if err != nil {
			return fail(err)
		}
		return ok(breaches)

	case "meshnet_peers":
		peers, err := s.mesh.Peers()
		if err != nil {
			return fail(err)
		}
		return ok(peers)

	case "meshnet_register":
		var a struct {
			DeviceName string `json:"deviceName"`
			Pubkey     string `json:"pubkey"`
		}
		if err := decode(req.Args, &a); err != nil {
			return fail(err)
		}
		resp, err := s.mesh.Register(a.DeviceName, a.Pubkey)
		if err != nil {
			return fail(err)
		}
		return ok(resp)

	case "ping":
		return ok(map[string]string{"version": tunnel.Version, "time": time.Now().UTC().Format(time.RFC3339)})

	default:
		return failStr("unknown op: " + req.Op)
	}
}

// Close shuts the listener down.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.sockPath != "" {
		_ = os.Remove(s.sockPath)
	}
	return s.ln.Close()
}
