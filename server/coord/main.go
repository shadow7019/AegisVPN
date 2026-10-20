// Package main implements the AegisVPN coordination server ("coord").
//
// Responsibilities (docs/PROTOCOL.md section 4):
//   - node registry: GET /v1/nodes, POST /v1/nodes/register
//   - meshnet:       POST /v1/mesh/register, GET /v1/mesh/peers, POST /v1/mesh/invite
//   - breach lookup: GET /v1/breaches/{email} (HIBP passthrough)
//
// Node load values are refreshed by a periodic UDP status probe sent to each
// node's probePort (answered by the aegisprobe daemon shipped next to every
// WireGuard node). See probe.go for the wire format.
package main

import (
        "context"
        "encoding/json"
        "errors"
        "flag"
        "log"
        "net/http"
        "os"
        "os/signal"
        "strings"
        "syscall"
        "time"
)

var (
        flagListen        = flag.String("listen", envOr("COORD_LISTEN", ":8080"), "HTTP listen address")
        flagData          = flag.String("data", envOr("COORD_DATA", "./data"), "persistent data directory")
        flagToken         = flag.String("token", os.Getenv("COORD_TOKEN"), "registration token (falls back to COORD_TOKEN env)")
        flagProbeInterval = flag.Duration("probe-interval", 30*time.Second, "node load probe interval")
        flagProbeTimeout  = flag.Duration("probe-timeout", 2*time.Second, "per-probe UDP timeout")
        flagHIBPKey       = flag.String("hibp-key", os.Getenv("HIBP_API_KEY"), "HaveIBeenPwned API key (falls back to HIBP_API_KEY env)")
)

func envOr(key, def string) string {
        if v := os.Getenv(key); v != "" {
                return v
        }
        return def
}

func main() {
        flag.Parse()

        store, err := openStore(*flagData)
        if err != nil {
                log.Fatalf("coord: open store: %v", err)
        }

        mesh, err := openMeshStore(*flagData)
        if err != nil {
                log.Fatalf("coord: open mesh store: %v", err)
        }

        srv := &server{
                store:      store,
                mesh:       mesh,
                token:      *flagToken,
                hibpKey:    *flagHIBPKey,
                hibpClient: &http.Client{Timeout: 10 * time.Second},
        }

        // Background: refresh node load/latency via UDP status probes.
        prober := &prober{store: store, timeout: *flagProbeTimeout}
        go prober.loop(*flagProbeInterval)

        mux := http.NewServeMux()
        mux.HandleFunc("GET /v1/nodes", srv.handleListNodes)
        mux.HandleFunc("POST /v1/nodes/register", srv.handleRegisterNode)
        mux.HandleFunc("POST /v1/mesh/register", srv.handleMeshRegister)
        mux.HandleFunc("GET /v1/mesh/peers", srv.handleMeshPeers)
        mux.HandleFunc("POST /v1/mesh/invite", srv.handleMeshInvite)
        mux.HandleFunc("GET /v1/mesh/invites", srv.handleMeshInvites)
        mux.HandleFunc("GET /v1/breaches/", srv.handleBreaches)
        mux.HandleFunc("GET /healthz", srv.handleHealth)

        handler := withRecovery(withLogging(withCORS(mux)))

        httpSrv := &http.Server{
                Addr:              *flagListen,
                Handler:           handler,
                ReadHeaderTimeout: 10 * time.Second,
        }

        errCh := make(chan error, 1)
        go func() {
                log.Printf("coord: listening on %s (data=%s)", *flagListen, *flagData)
                errCh <- httpSrv.ListenAndServe()
        }()

        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

        select {
        case sig := <-sigCh:
                log.Printf("coord: received %s, shutting down", sig)
                ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
                defer cancel()
                _ = httpSrv.Shutdown(ctx)
        case err := <-errCh:
                if err != nil && !errors.Is(err, http.ErrServerClosed) {
                        log.Fatalf("coord: %v", err)
                }
        }
}

// ---------------------------------------------------------------- handlers --

type server struct {
        store      *store
        mesh       *meshStore
        token      string
        hibpKey    string
        hibpClient *http.Client
}

func (s *server) handleListNodes(w http.ResponseWriter, r *http.Request) {
        nodes := s.store.list()
        writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

func (s *server) handleRegisterNode(w http.ResponseWriter, r *http.Request) {
        if !s.authorized(r) {
                if s.token == "" {
                        writeErr(w, http.StatusForbidden,
                                "node registration is disabled: COORD_TOKEN is not configured on the coordination server")
                        return
                }
                writeErr(w, http.StatusUnauthorized, "missing or invalid bearer token")
                return
        }

        var req struct {
                ID        string `json:"id"`
                Country   string `json:"country"`
                City      string `json:"city"`
                Host      string `json:"host"`
                Port      int    `json:"port"`
                ProbePort int    `json:"probePort"`
                Pubkey    string `json:"pubkey"`
                Type      string `json:"type"`
        }
        if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
                writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
                return
        }

        node, err := s.store.register(req.ID, req.Country, req.City, req.Host, req.Port, req.ProbePort, req.Pubkey, req.Type)
        if err != nil {
                var dup *duplicateError
                var bad *badRequestError
                switch {
                case errors.As(err, &dup):
                        writeErr(w, http.StatusConflict, err.Error())
                case errors.As(err, &bad):
                        writeErr(w, http.StatusBadRequest, err.Error())
                default:
                        writeErr(w, http.StatusInternalServerError, err.Error())
                }
                return
        }
        writeJSON(w, http.StatusCreated, map[string]any{"node": node})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, http.StatusOK, map[string]any{
                "ok":        true,
                "service":   "aegis-coord",
                "nodes":     s.store.count(),
                "meshPeers": s.mesh.count(),
                "time":      time.Now().UTC().Format(time.RFC3339),
        })
}

func (s *server) authorized(r *http.Request) bool {
        if s.token == "" {
                return false
        }
        h := r.Header.Get("Authorization")
        if strings.HasPrefix(h, "Bearer ") && strings.TrimPrefix(h, "Bearer ") == s.token {
                return true
        }
        // Convenience for provisioning scripts and curl users.
        return r.Header.Get("X-Coord-Token") == s.token
}

// ------------------------------------------------------------- middleware --

type statusRecorder struct {
        http.ResponseWriter
        status int
}

func (r *statusRecorder) WriteHeader(code int) {
        r.status = code
        r.ResponseWriter.WriteHeader(code)
}

func withLogging(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                start := time.Now()
                rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
                next.ServeHTTP(rec, r)
                log.Printf("%s %s -> %d (%s) from %s",
                        r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Microsecond), remoteAddr(r))
        })
}

func withCORS(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                origin := r.Header.Get("Origin")
                if origin == "" {
                        origin = "*"
                }
                h := w.Header()
                h.Set("Access-Control-Allow-Origin", origin)
                h.Set("Vary", "Origin")
                h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
                h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Coord-Token")
                h.Set("Access-Control-Max-Age", "600")
                if r.Method == http.MethodOptions {
                        w.WriteHeader(http.StatusNoContent)
                        return
                }
                next.ServeHTTP(w, r)
        })
}

func withRecovery(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                defer func() {
                        if rec := recover(); rec != nil {
                                log.Printf("coord: panic serving %s %s: %v", r.Method, r.URL.Path, rec)
                                writeErr(w, http.StatusInternalServerError, "internal error")
                        }
                }()
                next.ServeHTTP(w, r)
        })
}

// ------------------------------------------------------------ json helpers --

func writeJSON(w http.ResponseWriter, status int, v any) {
        w.Header().Set("Content-Type", "application/json; charset=utf-8")
        w.WriteHeader(status)
        enc := json.NewEncoder(w)
        enc.SetIndent("", "  ")
        _ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
        writeJSON(w, status, map[string]any{"error": msg})
}

func remoteAddr(r *http.Request) string {
        if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
                return strings.TrimSpace(strings.Split(fwd, ",")[0])
        }
        return r.RemoteAddr
}
