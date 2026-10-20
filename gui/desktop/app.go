package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	aegis "github.com/shadow7019/AegisVPN/engine"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	settingsFile    = "settings.json"
	nodesCacheTTL   = 60 * time.Second
	breachTimeout   = 15 * time.Second
	defaultCoordURL = "https://coord.aegisvpn.net"
)

// ConnectOptions carries the per-connection toggles chosen in the UI.
// Mapped 1:1 onto aegis.ConnectOpts (docs/PROTOCOL.md §1).
type ConnectOptions struct {
	KillSwitch       bool   `json:"killSwitch"`
	ThreatProtection bool   `json:"threatProtection"`
	Obfuscate        bool   `json:"obfuscate"`
	RelayNode        string `json:"relayNode,omitempty"` // second hop (Double VPN)
}

// Settings is the persisted GUI configuration (settings.json in appRoot).
type Settings struct {
	KillSwitch               bool     `json:"killSwitch"`
	ThreatProtection         bool     `json:"threatProtection"`
	ObfuscatedOnly           bool     `json:"obfuscatedOnly"`
	AutoConnectUntrustedWiFi bool     `json:"autoConnectUntrustedWiFi"`
	DNSOverride              string   `json:"dnsOverride"`
	SplitApps                []string `json:"splitApps"`
	SplitExcludeCidrs        []string `json:"splitExcludeCidrs"`
	CoordURL                 string   `json:"coordUrl"`
	ObfsKey                  string   `json:"obfsKey"`
	LastNodeID               string   `json:"lastNodeId"`
}

// Node mirrors one entry of the coordination-server registry
// (docs/PROTOCOL.md §4, GET /v1/nodes).
type Node struct {
	ID        string  `json:"id"`
	Country   string  `json:"country"`
	City      string  `json:"city"`
	Host      string  `json:"host"`
	Port      int     `json:"port"`
	Pubkey    string  `json:"pubkey"`
	Type      string  `json:"type"`
	Load      float64 `json:"load"`
	LatencyMs int     `json:"latencyMs,omitempty"`
}

// Breach is one Dark Web Monitor hit (shape matches the daemon's
// `breach_check` op data, docs/PROTOCOL.md §2).
type Breach struct {
	Source      string   `json:"source"`
	Date        string   `json:"date"`
	Email       string   `json:"email,omitempty"`
	DataClasses []string `json:"dataClasses"`
	Description string   `json:"description,omitempty"`
}

// BreachReport is the result returned to the frontend.
type BreachReport struct {
	Email    string   `json:"email"`
	Count    int      `json:"count"`
	Breaches []Breach `json:"breaches"`
}

// App is the Wails-bound backend. It is a thin orchestration layer over the
// real engine library (package aegis) — no VPN behaviour is simulated here.
type App struct {
	ctx context.Context

	mu        sync.Mutex
	appRoot   string
	client    *aegis.Client
	clientErr error
	settings  Settings

	http *http.Client

	nodesMu sync.Mutex
	nodes   []Node
	nodesAt time.Time
}

func NewApp() *App {
	return &App{
		settings: defaultSettings(),
		http:     &http.Client{Timeout: breachTimeout},
	}
}

func defaultSettings() Settings {
	return Settings{
		SplitApps:         []string{},
		SplitExcludeCidrs: []string{},
		CoordURL:          defaultCoordURL,
	}
}

// ---------------------------------------------------------------------------
// Lifecycle

// StartUp is invoked by Wails before the frontend loads.
func (a *App) StartUp(ctx context.Context) {
	if a.ctx == nil {
		a.ctx = ctx
	}

	root, err := appRoot()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.clientErr = fmt.Errorf("cannot resolve app data dir: %w", err)
		return
	}
	a.appRoot = root
	if err := os.MkdirAll(root, 0o755); err != nil {
		a.clientErr = fmt.Errorf("cannot create app data dir: %w", err)
		return
	}

	a.loadSettingsLocked()

	// Real engine client: owns the WireGuard device, routes, DNS, kill switch.
	a.client, a.clientErr = aegis.New(root)

	// Auto-connect (engine evaluates network trust when AutoConnect is set).
	if a.settings.AutoConnectUntrustedWiFi && a.settings.LastNodeID != "" {
		req := ConnectOptions{
			KillSwitch:       a.settings.KillSwitch,
			ThreatProtection: a.settings.ThreatProtection,
			Obfuscate:        a.settings.ObfuscatedOnly,
		}
		last := a.settings.LastNodeID
		go func() {
			time.Sleep(1200 * time.Millisecond)
			_, _ = a.Connect(last, req)
		}()
	}
}

// shutdown releases the engine (tun device, firewall rules, sockets).
func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		_ = a.client.Close()
	}
}

// ---------------------------------------------------------------------------
// Bound VPN controls (engine library surface, PROTOCOL.md §1)

// Connect brings up the WireGuard tunnel towards the selected node.
func (a *App) Connect(nodeID string, opts ConnectOptions) (aegis.Status, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return offlineStatus(nil), errors.New("nodeID is required")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	c, err := a.clientLocked()
	if err != nil {
		return offlineStatus(err), err
	}

	// Generate an obfuscation passphrase once so GUI and nodes agree.
	if opts.Obfuscate && a.settings.ObfsKey == "" {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err == nil {
			a.settings.ObfsKey = hex.EncodeToString(buf)
			_ = a.saveSettingsLocked()
		}
	}

	engineOpts := aegis.ConnectOpts{
		Node:             nodeID,
		Relay:            opts.RelayNode,
		KillSwitch:       opts.KillSwitch,
		ThreatProtection: opts.ThreatProtection,
		SplitExclude:     a.settings.SplitExcludeCidrs,
		Obfuscate:        opts.Obfuscate,
		ObfsKey:          a.settings.ObfsKey,
		AutoConnect:      a.settings.AutoConnectUntrustedWiFi,
	}
	if err := c.Connect(engineOpts); err != nil {
		return c.Status(), err
	}

	a.settings.LastNodeID = nodeID
	_ = a.saveSettingsLocked()
	return c.Status(), nil
}

// Disconnect tears the tunnel down.
func (a *App) Disconnect() (aegis.Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, err := a.clientLocked()
	if err != nil {
		return offlineStatus(err), err
	}
	if err := c.Disconnect(); err != nil {
		return c.Status(), err
	}
	return c.Status(), nil
}

// GetStatus returns the live engine status; the frontend polls it every 1.5s.
func (a *App) GetStatus() aegis.Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		st := aegis.Status{State: "disconnected"}
		if a.clientErr != nil {
			st.Error = a.clientErr.Error()
		}
		return st
	}
	return a.client.Status()
}

// SetKillSwitch toggles the engine's block-all firewall rules.
func (a *App) SetKillSwitch(enabled bool) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		if err := a.client.SetKillSwitch(enabled); err != nil {
			return a.settings.KillSwitch, err
		}
	}
	a.settings.KillSwitch = enabled
	return enabled, a.saveSettingsLocked()
}

// SetThreatProtection toggles DNS-level ad/tracker/malware blocking.
func (a *App) SetThreatProtection(enabled bool) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		if err := a.client.SetThreatProtection(enabled); err != nil {
			return a.settings.ThreatProtection, err
		}
	}
	a.settings.ThreatProtection = enabled
	return enabled, a.saveSettingsLocked()
}

// SetSplitTunnel applies the excluded CIDR list to the live tunnel and persists it.
func (a *App) SetSplitTunnel(exclude []string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	exclude = cleanCIDRs(exclude)
	if a.client != nil {
		if err := a.client.SetSplitTunnel(exclude, nil); err != nil {
			return false, err
		}
	}
	a.settings.SplitExcludeCidrs = exclude
	return true, a.saveSettingsLocked()
}

// ---------------------------------------------------------------------------
// Node registry (PROTOCOL.md §4) & Dark Web Monitor

// GetNodes fetches the coordination server's node registry. The engine
// library surface (§1) exposes no node listing, so the GUI queries the same
// registry the engine provisions from; responses are cached for 60s and
// served stale if the registry is unreachable.
func (a *App) GetNodes() ([]Node, error) {
	coord := a.coordURL()

	a.nodesMu.Lock()
	cached, at := a.nodes, a.nodesAt
	a.nodesMu.Unlock()
	if cached != nil && time.Since(at) < nodesCacheTTL {
		return cached, nil
	}

	var payload struct {
		Nodes []Node `json:"nodes"`
	}
	if err := a.getJSON(coord+"/v1/nodes", &payload); err != nil {
		if cached != nil {
			return cached, nil
		}
		return nil, fmt.Errorf("node registry unreachable: %w", err)
	}

	a.nodesMu.Lock()
	a.nodes = payload.Nodes
	a.nodesAt = time.Now()
	a.nodesMu.Unlock()

	if payload.Nodes == nil {
		return []Node{}, nil
	}
	return payload.Nodes, nil
}

// CheckBreaches runs the Dark Web Monitor lookup. Breach intelligence is a
// service-side feature: the query goes to the coordination server, mirroring
// the engine daemon's `breach_check` op (PROTOCOL.md §2).
func (a *App) CheckBreaches(email string) (BreachReport, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") || len(email) > 254 {
		return BreachReport{}, errors.New("enter a valid email address")
	}

	coord := a.coordURL()
	body, err := json.Marshal(map[string]string{"email": email})
	if err != nil {
		return BreachReport{}, err
	}
	resp, err := a.http.Post(coord+"/v1/breach-check", "application/json", bytes.NewReader(body))
	if err != nil {
		return BreachReport{}, fmt.Errorf("dark web monitor unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return BreachReport{}, fmt.Errorf("dark web monitor returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Breaches []Breach `json:"breaches"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return BreachReport{}, fmt.Errorf("invalid breach report: %w", err)
	}
	report := BreachReport{Email: email, Count: len(payload.Breaches), Breaches: payload.Breaches}
	if report.Breaches == nil {
		report.Breaches = []Breach{}
	}
	return report, nil
}

// ---------------------------------------------------------------------------
// Settings

// GetSettings returns the current GUI settings.
func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// SetSettings validates, persists and (where relevant) live-applies settings.
func (a *App) SetSettings(next Settings) (Settings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	prevDNS := a.settings.DNSOverride
	next.CoordURL = strings.TrimSpace(next.CoordURL)
	next.DNSOverride = strings.TrimSpace(next.DNSOverride)
	next.ObfsKey = strings.TrimSpace(next.ObfsKey)
	if next.CoordURL == "" {
		next.CoordURL = defaultCoordURL
	}
	if next.SplitApps == nil {
		next.SplitApps = []string{}
	}
	next.SplitExcludeCidrs = cleanCIDRs(next.SplitExcludeCidrs)

	a.settings = next
	if err := a.saveSettingsLocked(); err != nil {
		return a.settings, err
	}

	// Push a changed DNS override straight onto the live tunnel.
	if a.client != nil && next.DNSOverride != prevDNS {
		if servers := splitDNS(next.DNSOverride); len(servers) > 0 {
			if err := a.client.ApplyDNS(servers); err != nil {
				return a.settings, fmt.Errorf("settings saved, but ApplyDNS failed: %w", err)
			}
		}
	}
	return a.settings, nil
}

// PickAppFile opens a native file dialog and returns the chosen executable
// path for the split-tunnel application list.
func (a *App) PickAppFile() (string, error) {
	if a.ctx == nil {
		return "", errors.New("app not ready")
	}
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Select application to exclude from the VPN tunnel",
		Filters: []wruntime.FileFilter{
			{DisplayName: "Applications (*.exe;*.app)", Pattern: "*.exe;*.app"},
			{DisplayName: "All files (*.*)", Pattern: "*.*"},
		},
	})
}

// ---------------------------------------------------------------------------
// Internals

func (a *App) clientLocked() (*aegis.Client, error) {
	if a.client == nil {
		if a.clientErr != nil {
			return nil, fmt.Errorf("engine unavailable: %w", a.clientErr)
		}
		return nil, errors.New("engine not initialised")
	}
	return a.client, nil
}

func (a *App) settingsPathLocked() string {
	return filepath.Join(a.appRoot, settingsFile)
}

func (a *App) loadSettingsLocked() {
	a.settings = defaultSettings()
	if a.appRoot == "" {
		return
	}
	raw, err := os.ReadFile(a.settingsPathLocked())
	if err != nil {
		return // first run — keep defaults
	}
	// Unmarshal over the defaults so missing keys keep their fallback values.
	if err := json.Unmarshal(raw, &a.settings); err != nil {
		a.settings = defaultSettings()
		return
	}
	if a.settings.SplitApps == nil {
		a.settings.SplitApps = []string{}
	}
	if a.settings.SplitExcludeCidrs == nil {
		a.settings.SplitExcludeCidrs = []string{}
	}
	if a.settings.CoordURL == "" {
		a.settings.CoordURL = defaultCoordURL
	}
}

func (a *App) saveSettingsLocked() error {
	if a.appRoot == "" {
		return errors.New("app root not initialised")
	}
	raw, err := json.MarshalIndent(a.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.settingsPathLocked(), raw, 0o600)
}

// coordURL returns the normalised coordination-server base URL.
func (a *App) coordURL() string {
	a.mu.Lock()
	u := strings.TrimSpace(a.settings.CoordURL)
	a.mu.Unlock()
	u = strings.TrimRight(u, "/")
	if u == "" {
		u = defaultCoordURL
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	return u
}

func (a *App) getJSON(url string, out interface{}) error {
	resp, err := a.http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// appRoot resolves the per-OS application data directory.
func appRoot() (string, error) {
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			local = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(local, "AegisVPN"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "AegisVPN"), nil
	default:
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			return filepath.Join(dir, "aegisvpn"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "aegisvpn"), nil
	}
}

func cleanCIDRs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func splitDNS(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func offlineStatus(err error) aegis.Status {
	st := aegis.Status{State: "disconnected"}
	if err != nil {
		st.Error = err.Error()
	}
	return st
}
