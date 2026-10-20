package main

import (
        "crypto/rand"
        "encoding/base64"
        "encoding/hex"
        "encoding/json"
        "errors"
        "fmt"
        "net/http"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"
)

// Meshnet peers get addresses from the CGNAT pool 100.64.0.0/10, the range
// reserved by RFC 6598 for carrier-grade NAT — shared, not routed on the
// public internet, which is exactly what a private peer-to-peer overlay wants.
const (
        meshBaseV   = uint32(100)<<24 | uint32(64)<<16 // 100.64.0.0
        meshSpanV   = uint32(1) << 22                  // /10 covers 2^22 addresses
        meshAnchorV = 2                                // 100.64.0.0 = network, 100.64.0.1 = coord anchor
)

// offsetAddr maps a pool offset onto 100.64.0.0/10.
func offsetAddr(off int) (netipAddr, bool) {
        v := meshBaseV + uint32(off)
        if v >= meshBaseV+meshSpanV {
                return netipAddr{}, false
        }
        return netipAddr{[4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}}, true
}

type netipAddr struct{ b [4]byte }

func (a netipAddr) String() string {
        b := a.b
        return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
}

// MeshPeer is one registered device in the user's private mesh.
type MeshPeer struct {
        DeviceName string `json:"deviceName"`
        Pubkey     string `json:"pubkey"`
        IP         string `json:"ip"`
        AddedAt    int64  `json:"addedAt"`
}

// MeshInvite links two registered devices so their engines can exchange
// WireGuard endpoints (hole punching happens engine-side).
type MeshInvite struct {
        ID        string `json:"id"`
        From      string `json:"from"`
        To        string `json:"to"`
        Nonce     string `json:"nonce"`
        CreatedAt int64  `json:"createdAt"`
        ExpiresAt int64  `json:"expiresAt"`
}

type meshFile struct {
        Peers   []MeshPeer   `json:"peers"`
        Invites []MeshInvite `json:"invites"`
}

type meshStore struct {
        mu      sync.Mutex
        path    string
        data    meshFile
        nextIdx uint32
}

func openMeshStore(dataDir string) (*meshStore, error) {
        if err := os.MkdirAll(dataDir, 0o755); err != nil {
                return nil, err
        }
        path := filepath.Join(dataDir, "mesh.json")
        m := &meshStore{path: path}
        raw, err := os.ReadFile(path)
        if errors.Is(err, os.ErrNotExist) {
                return m, nil // empty registry, nothing persisted yet
        }
        if err != nil {
                return nil, err
        }
        if err := json.Unmarshal(raw, &m.data); err != nil {
                return nil, fmt.Errorf("parse %s: %w", path, err)
        }
        m.nextIdx = uint32(len(m.data.Peers))
        return m, nil
}

func (m *meshStore) saveLocked() error {
        raw, err := json.MarshalIndent(m.data, "", "  ")
        if err != nil {
                return err
        }
        tmp := m.path + ".tmp"
        if err := os.WriteFile(tmp, raw, 0o644); err != nil {
                return err
        }
        return os.Rename(tmp, m.path)
}

// allocateLocked returns the next free CGNAT address in 100.64.0.0/10.
func (m *meshStore) allocateLocked() (string, error) {
        for attempt := 0; attempt < int(meshSpanV); attempt++ {
                ip, ok := offsetAddr(int(m.nextIdx) + meshAnchorV)
                m.nextIdx++
                if !ok {
                        return "", errors.New("mesh pool exhausted")
                }
                taken := false
                for _, p := range m.data.Peers {
                        if p.IP == ip.String() {
                                taken = true
                                break
                        }
                }
                if !taken {
                        return ip.String(), nil
                }
        }
        return "", errors.New("mesh pool exhausted")
}

// ------------------------------------------------------------- HTTP layer --

func (s *server) handleMeshRegister(w http.ResponseWriter, r *http.Request) {
        var req struct {
                DeviceName string `json:"deviceName"`
                Pubkey     string `json:"pubkey"`
        }
        if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
                writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
                return
        }
        name := strings.TrimSpace(req.DeviceName)
        if name == "" || len(name) > 64 {
                writeErr(w, http.StatusBadRequest, "deviceName must be 1-64 characters")
                return
        }
        raw, err := base64.StdEncoding.DecodeString(req.Pubkey)
        if err != nil || len(raw) != 32 {
                writeErr(w, http.StatusBadRequest, "pubkey must be base64 and decode to 32 bytes (a WireGuard public key)")
                return
        }

        peer, err := s.mesh.register(name, req.Pubkey)
        if err != nil {
                writeErr(w, http.StatusInternalServerError, err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{"peer": peer})
}

func (s *server) handleMeshPeers(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, http.StatusOK, map[string]any{"peers": s.mesh.list()})
}

func (s *server) handleMeshInvite(w http.ResponseWriter, r *http.Request) {
        var req struct {
                From string `json:"from"`
                To   string `json:"to"`
        }
        if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
                writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
                return
        }
        inv, err := s.mesh.invite(strings.TrimSpace(req.From), strings.TrimSpace(req.To))
        if err != nil {
                var bad *badRequestError
                if errors.As(err, &bad) {
                        writeErr(w, http.StatusBadRequest, err.Error())
                        return
                }
                writeErr(w, http.StatusInternalServerError, err.Error())
                return
        }
        writeJSON(w, http.StatusCreated, map[string]any{"invite": inv})
}

func (s *server) handleMeshInvites(w http.ResponseWriter, r *http.Request) {
        device := strings.TrimSpace(r.URL.Query().Get("device"))
        writeJSON(w, http.StatusOK, map[string]any{"invites": s.mesh.listInvites(device)})
}

// -------------------------------------------------------------- registry --

func (m *meshStore) register(deviceName, pubkey string) (*MeshPeer, error) {
        m.mu.Lock()
        defer m.mu.Unlock()

        // Idempotent: re-registering a known pubkey returns the existing lease.
        for i := range m.data.Peers {
                if m.data.Peers[i].Pubkey == pubkey {
                        return &m.data.Peers[i], nil
                }
        }

        ip, err := m.allocateLocked()
        if err != nil {
                return nil, err
        }
        peer := MeshPeer{
                DeviceName: deviceName,
                Pubkey:     pubkey,
                IP:         ip,
                AddedAt:    time.Now().Unix(),
        }
        m.data.Peers = append(m.data.Peers, peer)
        if err := m.saveLocked(); err != nil {
                m.data.Peers = m.data.Peers[:len(m.data.Peers)-1]
                return nil, err
        }
        return &peer, nil
}

func (m *meshStore) list() []MeshPeer {
        m.mu.Lock()
        defer m.mu.Unlock()
        out := make([]MeshPeer, len(m.data.Peers))
        copy(out, m.data.Peers)
        return out
}

func (m *meshStore) invite(from, to string) (*MeshInvite, error) {
        if from == "" || to == "" {
                return nil, &badRequestError{"both \"from\" and \"to\" device names are required"}
        }
        if from == to {
                return nil, &badRequestError{"a device cannot invite itself"}
        }

        m.mu.Lock()
        defer m.mu.Unlock()

        find := func(name string) *MeshPeer {
                for i := range m.data.Peers {
                        if m.data.Peers[i].DeviceName == name {
                                return &m.data.Peers[i]
                        }
                }
                return nil
        }
        if find(from) == nil {
                return nil, &badRequestError{fmt.Sprintf("device %q is not registered in the mesh", from)}
        }
        if find(to) == nil {
                return nil, &badRequestError{fmt.Sprintf("device %q is not registered in the mesh", to)}
        }

        nonce := make([]byte, 16)
        if _, err := rand.Read(nonce); err != nil {
                return nil, err
        }
        inv := MeshInvite{
                ID:        hex.EncodeToString(nonce[:4]),
                From:      from,
                To:        to,
                Nonce:     hex.EncodeToString(nonce),
                CreatedAt: time.Now().Unix(),
                ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
        }
        m.data.Invites = append(m.data.Invites, inv)
        if err := m.saveLocked(); err != nil {
                m.data.Invites = m.data.Invites[:len(m.data.Invites)-1]
                return nil, err
        }
        return &inv, nil
}

func (m *meshStore) listInvites(device string) []MeshInvite {
        m.mu.Lock()
        defer m.mu.Unlock()
        now := time.Now().Unix()
        out := make([]MeshInvite, 0, len(m.data.Invites))
        for _, inv := range m.data.Invites {
                if inv.ExpiresAt < now {
                        continue
                }
                if device == "" || inv.From == device || inv.To == device {
                        out = append(out, inv)
                }
        }
        return out
}

// count exposes the mesh size for /healthz.
func (m *meshStore) count() int {
        m.mu.Lock()
        defer m.mu.Unlock()
        return len(m.data.Peers)
}
