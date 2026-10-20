package main

import (
        _ "embed"
        "encoding/base64"
        "encoding/json"
        "errors"
        "fmt"
        "log"
        "os"
        "path/filepath"
        "regexp"
        "sync"
        "time"
)

// Node types per docs/PROTOCOL.md section 4.
var validNodeTypes = map[string]bool{
        "standard":   true,
        "p2p":        true,
        "double":     true,
        "onion":      true,
        "obfuscated": true,
        "dedicated":  true,
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)

// Node is the registry entry shape returned by GET /v1/nodes.
// The JSON field names follow docs/PROTOCOL.md section 4 exactly; lastSeen
// and probePort are AegisVPN extensions used for health probing.
type Node struct {
        ID        string  `json:"id"`
        Country   string  `json:"country"`
        City      string  `json:"city"`
        Host      string  `json:"host"`
        Port      int     `json:"port"`
        ProbePort int     `json:"probePort,omitempty"`
        Pubkey    string  `json:"pubkey"`
        Type      string  `json:"type"`
        Load      float64 `json:"load"`
        LatencyMs int     `json:"latencyMs"`
        LastSeen  int64   `json:"lastSeen,omitempty"`
}

type nodeFile struct {
        Nodes []Node `json:"nodes"`
}

type duplicateError struct{ msg string }

func (e *duplicateError) Error() string { return e.msg }

type badRequestError struct{ msg string }

func (e *badRequestError) Error() string { return e.msg }

// store is the file-backed node registry. nodes.json is the single source of
// truth: it is seeded from the embedded example registry on first boot and
// updated in place when nodes self-register or when the UDP prober refreshes
// their load/latency.
type store struct {
        mu    sync.RWMutex
        path  string
        nodes []Node
}

//go:embed nodes.json
var seedNodes []byte

func openStore(dataDir string) (*store, error) {
        if err := os.MkdirAll(dataDir, 0o755); err != nil {
                return nil, err
        }
        path := filepath.Join(dataDir, "nodes.json")

        s := &store{path: path}
        raw, err := os.ReadFile(path)
        if errors.Is(err, os.ErrNotExist) {
                // First boot: materialize the embedded seed file.
                if err := os.WriteFile(path, seedNodes, 0o644); err != nil {
                        return nil, err
                }
                raw = seedNodes
                log.Printf("coord: seeded node registry at %s (%d nodes)", path, countNodes(raw))
        } else if err != nil {
                return nil, err
        }

        var f nodeFile
        if err := json.Unmarshal(raw, &f); err != nil {
                return nil, fmt.Errorf("parse %s: %w", path, err)
        }
        s.nodes = f.Nodes
        return s, nil
}

func countNodes(raw []byte) int {
        var f nodeFile
        if json.Unmarshal(raw, &f) != nil {
                return 0
        }
        return len(f.Nodes)
}

func (s *store) saveLocked() error {
        raw, err := json.MarshalIndent(nodeFile{Nodes: s.nodes}, "", "  ")
        if err != nil {
                return err
        }
        tmp := s.path + ".tmp"
        if err := os.WriteFile(tmp, raw, 0o644); err != nil {
                return err
        }
        return os.Rename(tmp, s.path)
}

func (s *store) list() []Node {
        s.mu.RLock()
        defer s.mu.RUnlock()
        out := make([]Node, len(s.nodes))
        copy(out, s.nodes)
        return out
}

func (s *store) count() int {
        s.mu.RLock()
        defer s.mu.RUnlock()
        return len(s.nodes)
}

func (s *store) register(id, country, city, host string, port, probePort int, pubkey, typ string) (*Node, error) {
        if !idRe.MatchString(id) {
                return nil, &badRequestError{fmt.Sprintf("invalid node id %q (want [a-z0-9-]{3,64})", id)}
        }
        if typ == "" {
                typ = "standard"
        }
        if !validNodeTypes[typ] {
                return nil, &badRequestError{fmt.Sprintf("invalid node type %q (want one of standard|p2p|double|onion|obfuscated|dedicated)", typ)}
        }
        if host == "" {
                return nil, &badRequestError{"host is required"}
        }
        if port <= 0 || port > 65535 {
                return nil, &badRequestError{fmt.Sprintf("invalid port %d", port)}
        }
        raw, err := base64.StdEncoding.DecodeString(pubkey)
        if err != nil {
                return nil, &badRequestError{"pubkey must be base64"}
        }
        if len(raw) != 32 {
                return nil, &badRequestError{"pubkey must decode to 32 bytes (a WireGuard public key)"}
        }

        s.mu.Lock()
        defer s.mu.Unlock()
        for i := range s.nodes {
                if s.nodes[i].ID == id {
                        return nil, &duplicateError{fmt.Sprintf("node %q already registered", id)}
                }
        }
        n := Node{
                ID:        id,
                Country:   country,
                City:      city,
                Host:      host,
                Port:      port,
                ProbePort: probePort,
                Pubkey:    pubkey,
                Type:      typ,
                // A freshly registered node has no probe data yet; expose it with a
                // neutral 0 load until the first probe round completes.
                Load: 0,
        }
        s.nodes = append(s.nodes, n)
        if err := s.saveLocked(); err != nil {
                s.nodes = s.nodes[:len(s.nodes)-1]
                return nil, err
        }
        return &n, nil
}

// updateLoad is called by the prober when a node answers a status probe.
func (s *store) updateLoad(id string, load float64, latencyMs int, sessions int) {
        s.mu.Lock()
        defer s.mu.Unlock()
        for i := range s.nodes {
                if s.nodes[i].ID == id {
                        s.nodes[i].Load = load
                        s.nodes[i].LatencyMs = latencyMs
                        s.nodes[i].LastSeen = time.Now().Unix()
                        _ = sessions // reported by the probe; kept for future capacity logic
                        _ = s.saveLocked()
                        return
                }
        }
}

func (s *store) probedNodes() []Node {
        s.mu.RLock()
        defer s.mu.RUnlock()
        out := make([]Node, 0, len(s.nodes))
        for _, n := range s.nodes {
                if n.ProbePort > 0 && n.Host != "" {
                        out = append(out, n)
                }
        }
        return out
}
