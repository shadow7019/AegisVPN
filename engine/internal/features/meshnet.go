package features

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MeshPeer is one device in the user's Meshnet.
type MeshPeer struct {
	Name     string `json:"name"`
	Pubkey   string `json:"pubkey"`
	IP       string `json:"ip"`     // Meshnet internal address
	Online   bool   `json:"online"`
	CanRoute bool   `json:"canRoute"` // peer allows traffic routing through it
}

// MeshnetClient registers devices and lists peers against the coordination
// server (docs/PROTOCOL.md section 4: POST /v1/mesh/register,
// GET /v1/mesh/peers).
type MeshnetClient struct {
	BaseURL string
	hc      *http.Client
}

// NewMeshnetClient creates a Meshnet client against the coordination server.
func NewMeshnetClient(baseURL string) *MeshnetClient {
	if baseURL == "" {
		baseURL = coordURL()
	}
	return &MeshnetClient{BaseURL: baseURL, hc: &http.Client{Timeout: 15 * time.Second}}
}

type meshRegisterRequest struct {
	DeviceName string `json:"deviceName"`
	Pubkey     string `json:"pubkey"`
}

type meshRegisterResponse struct {
	IP      string `json:"ip,omitempty"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// Register adds (or updates) this device in the user's Meshnet.
func (m *MeshnetClient) Register(deviceName, pubkey string) (*meshRegisterResponse, error) {
	payload, err := json.Marshal(meshRegisterRequest{DeviceName: deviceName, Pubkey: pubkey})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, m.BaseURL+"/v1/mesh/register", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AegisVPN")
	resp, err := m.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meshnet register: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("meshnet register: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	var out meshRegisterResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Peers lists the user's Meshnet peers.
func (m *MeshnetClient) Peers() ([]MeshPeer, error) {
	req, err := http.NewRequest(http.MethodGet, m.BaseURL+"/v1/mesh/peers", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AegisVPN")
	resp, err := m.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meshnet peers: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("meshnet peers: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	var peers []MeshPeer
	if err := json.Unmarshal(body, &peers); err != nil {
		return nil, err
	}
	return peers, nil
}
