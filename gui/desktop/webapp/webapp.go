// Package webapp serves the local AegisVPN dashboard that ships inside the
// desktop bundles (http://127.0.0.1:8080). It is deliberately dependency
// free — net/http plus the engine's public API — so every platform build
// (macOS bundle, Windows shell, plain Linux) compiles it unchanged.
//
// Surfaces:
//
//	GET  /                       dashboard UI (embedded, offline-capable)
//	GET  /api/health             liveness used by the launcher
//	GET  /api/status             WireGuard tunnel status snapshot
//	GET  /api/nodes              private fleet registry
//	POST /api/tunnel/connect     {"node": id, "killswitch": bool}
//	POST /api/tunnel/disconnect
//	GET  /api/relays             free public relay index (country filter)
//	GET  /api/relays/profile     download an .ovpn (fallback path)
//	POST /api/relays/connect     {"hostname" | "country"} — one click
//	POST /api/relays/disconnect
//	GET  /api/relays/status      session + log tail + openvpn binary path
//
// The HTTP listener binds loopback only (enforced by the launcher main).
package webapp

import (
	"embed"
	"net/http"
	"os"
	"path/filepath"

	aegis "github.com/shadow7019/AegisVPN/engine"
	"github.com/shadow7019/AegisVPN/engine/openvpn"
)

//go:embed dashboard.html
var dashboardFS embed.FS

// App is the dashboard server.
type App struct {
	root   string
	client *aegis.Client
	om     *openvpn.Manager
	mux    *http.ServeMux
}

// New builds the dashboard app rooted at an application-data directory.
func New(root string) (*App, error) {
	if root == "" {
		return nil, os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Join(root, "relays"), 0o700); err != nil {
		return nil, err
	}
	client, err := aegis.New(root)
	if err != nil {
		return nil, err
	}
	a := &App{
		root:   root,
		client: client,
		om:     &openvpn.Manager{},
		mux:    http.NewServeMux(),
	}
	a.routes()
	return a, nil
}

// Handler returns the root http.Handler.
func (a *App) Handler() http.Handler { return a.mux }

// Close stops any relay child and releases the engine client.
func (a *App) Close() {
	_ = a.om.Stop()
	_ = a.client.Disconnect()
}

func (a *App) routes() {
	a.mux.HandleFunc("/", a.handleDashboard)
	a.mux.HandleFunc("/api/health", a.handleHealth)
	a.mux.HandleFunc("/api/status", a.handleStatus)
	a.mux.HandleFunc("/api/nodes", a.handleNodes)
	a.mux.HandleFunc("/api/tunnel/connect", a.handleTunnelConnect)
	a.mux.HandleFunc("/api/tunnel/disconnect", a.handleTunnelDisconnect)
	a.mux.HandleFunc("/api/relays", a.handleRelays)
	a.mux.HandleFunc("/api/relays/profile", a.handleRelayProfile)
	a.mux.HandleFunc("/api/relays/connect", a.handleRelayConnect)
	a.mux.HandleFunc("/api/relays/disconnect", a.handleRelayDisconnect)
	a.mux.HandleFunc("/api/relays/status", a.handleRelayStatus)
}
