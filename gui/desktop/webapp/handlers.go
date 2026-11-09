package webapp

import (
        "context"
        "encoding/json"
        "fmt"
        "net/http"
        "os"
        "path/filepath"
        "strings"
        "time"

        aegis "github.com/shadow7019/AegisVPN/engine"
        "github.com/shadow7019/AegisVPN/engine/openvpn"
        "github.com/shadow7019/AegisVPN/engine/publicrelays"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
        w.Header().Set("Content-Type", "application/json; charset=utf-8")
        w.WriteHeader(code)
        _ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
        writeJSON(w, code, map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
        defer r.Body.Close()
        if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
                writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
                return false
        }
        return true
}

// ------------------------------------------------------------ dashboard ----

func (a *App) handleDashboard(w http.ResponseWriter, _ *http.Request) {
        b, err := dashboardFS.ReadFile("dashboard.html")
        if err != nil {
                http.Error(w, "dashboard missing", http.StatusInternalServerError)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Header().Set("Cache-Control", "no-store")
        _, _ = w.Write(b)
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
        writeJSON(w, http.StatusOK, map[string]any{
                "status": "ok",
                "time":   time.Now().UTC(),
        })
}

// --------------------------------------------------------------- tunnel ----

func (a *App) handleStatus(w http.ResponseWriter, _ *http.Request) {
        writeJSON(w, http.StatusOK, a.client.Status())
}

func (a *App) handleNodes(w http.ResponseWriter, _ *http.Request) {
        nodes, err := a.client.Registry().List(false)
        if err != nil {
                writeErr(w, http.StatusServiceUnavailable, "fleet registry unavailable: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, nodes)
}

func (a *App) handleTunnelConnect(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, "POST only")
                return
        }
        var body struct {
                Node       string `json:"node"`
                KillSwitch bool   `json:"killswitch"`
        }
        if !decodeBody(w, r, &body) {
                return
        }
        if body.Node == "" {
                writeErr(w, http.StatusBadRequest, "node is required")
                return
        }
        opts := aegis.ConnectOpts{
                Node:        body.Node,
                KillSwitch:  body.KillSwitch,
                AutoConnect: true,
        }
        if err := a.client.Connect(opts); err != nil {
                writeErr(w, http.StatusBadGateway, err.Error())
                return
        }
        writeJSON(w, http.StatusOK, a.client.Status())
}

func (a *App) handleTunnelDisconnect(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, "POST only")
                return
        }
        if err := a.client.Disconnect(); err != nil {
                writeErr(w, http.StatusBadGateway, err.Error())
                return
        }
        writeJSON(w, http.StatusOK, a.client.Status())
}

// --------------------------------------------------------------- relays ----

func (a *App) handleRelays(w http.ResponseWriter, r *http.Request) {
        q := r.URL.Query()
        ctx := r.Context()
        relays, err := publicrelays.List(ctx, q.Get("force") == "1")
        if err != nil {
                writeErr(w, http.StatusBadGateway, err.Error())
                return
        }
        if cc := q.Get("country"); cc != "" && !strings.EqualFold(cc, "any") {
                filtered := make([]publicrelays.Relay, 0, len(relays))
                for _, s := range relays {
                        if strings.EqualFold(s.CountryCode, cc) {
                                filtered = append(filtered, s)
                        }
                }
                relays = filtered
        }
        publicrelays.SortByScore(relays)

        limit := 200
        if n := q.Get("limit"); n != "" {
                _, _ = fmt.Sscanf(n, "%d", &limit)
        }
        if limit > 0 && len(relays) > limit {
                relays = relays[:limit]
        }

        // live probe the top few rows so the UI shows real reachability
        probeN := 0
        if q.Get("probe") == "1" {
                probeN = 8
        }
        type probed struct {
                publicrelays.Relay
                ProbeMs int  `json:"probe_ms_now"`
                Alive   bool `json:"alive"`
        }
        out := make([]probed, len(relays))
        for i, s := range relays {
                out[i] = probed{Relay: s, ProbeMs: s.ProbeMs, Alive: s.Online}
        }
        for i := 0; i < len(out) && i < probeN; i++ {
                if ms, err := publicrelays.Probe(ctx, out[i].IP, out[i].Port); err == nil {
                        out[i].ProbeMs, out[i].Alive = ms, true
                } else {
                        out[i].Alive = false
                }
        }

        // country chips for the UI
        countries := map[string]string{}
        for _, s := range relays {
                countries[s.CountryCode] = s.CountryName
        }

        writeJSON(w, http.StatusOK, map[string]any{
                "count":     len(out),
                "relays":    out,
                "countries": countries,
        })
}

func (a *App) handleRelayProfile(w http.ResponseWriter, r *http.Request) {
        host := strings.TrimSpace(r.URL.Query().Get("hostname"))
        if host == "" || strings.ContainsAny(host, "/\\") {
                writeErr(w, http.StatusBadRequest, "hostname required")
                return
        }
        cfg, err := publicrelays.Profile(r.Context(), host)
        if err != nil {
                writeErr(w, http.StatusBadGateway, err.Error())
                return
        }
        w.Header().Set("Content-Type", "application/x-openvpn-profile")
        w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "AegisVPN-"+safeRelayName(host)+".ovpn"))
        w.Header().Set("Cache-Control", "no-store")
        _, _ = w.Write(cfg)
}

func (a *App) handleRelayConnect(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, "POST only")
                return
        }
        if _, running := a.om.Active(); running {
                writeErr(w, http.StatusConflict, "a relay session is already running")
                return
        }
        var body struct {
                Hostname string `json:"hostname"`
                Country  string `json:"country"`
        }
        if !decodeBody(w, r, &body) {
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
        defer cancel()

        relays, err := publicrelays.List(ctx, false)
        if err != nil {
                writeErr(w, http.StatusBadGateway, err.Error())
                return
        }
        host := body.Hostname
        if host == "" {
                best, ok := publicrelays.Best(relays, body.Country)
                if !ok {
                        writeErr(w, http.StatusNotFound, "no online relay matches that country")
                        return
                }
                host = best.Hostname
        }
        endpoint := host
        for _, s := range relays {
                if strings.EqualFold(s.Hostname, host) {
                        endpoint = fmt.Sprintf("%s:%d/%s", s.IP, s.Port, s.Proto)
                        break
                }
        }

        cfg, err := publicrelays.Profile(ctx, host)
        if err != nil {
                writeErr(w, http.StatusBadGateway, err.Error())
                return
        }
        dir := filepath.Join(a.root, "relays")
        cfgPath := filepath.Join(dir, safeRelayName(host)+".ovpn")
        if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
                writeErr(w, http.StatusInternalServerError, err.Error())
                return
        }

        info, err := a.om.Start(cfgPath, dir, host, endpoint)
        if err != nil {
                writeErr(w, http.StatusInternalServerError, err.Error())
                return
        }
        _ = openvpn.SaveSession(filepath.Join(dir, "session.json"), info)

        connected := openvpn.WaitForHandshake(info.LogPath, 25*time.Second)
        resp := map[string]any{
                "connected": connected,
                "session":   info,
        }
        if !connected {
                resp["log_tail"] = tailLog(info.LogPath, 8)
        }
        writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleRelayDisconnect(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, "POST only")
                return
        }
        if err := a.om.Stop(); err != nil {
                writeErr(w, http.StatusInternalServerError, err.Error())
                return
        }
        openvpn.ClearSession(filepath.Join(a.root, "relays", "session.json"))
        writeJSON(w, http.StatusOK, map[string]any{"connected": false})
}

func (a *App) handleRelayStatus(w http.ResponseWriter, _ *http.Request) {
        bin, binErr := openvpn.FindBinary()
        resp := map[string]any{
                "connected": false,
                "openvpn": map[string]any{
                        "path":  bin,
                        "found": binErr == nil,
                        "hint":  openvpnHint(binErr),
                },
        }
        if info, ok := a.om.Active(); ok {
                resp["connected"] = true
                resp["session"] = info
                resp["log_tail"] = tailLog(info.LogPath, 12)
        } else {
                stale := filepath.Join(a.root, "relays", "session.json")
                if fileExists(stale) {
                        _ = os.Remove(stale) // marker from a previous process run
                }
        }
        writeJSON(w, http.StatusOK, resp)
}

// -------------------------------------------------------------- helpers ----

func openvpnHint(err error) string {
        if err == nil {
                return ""
        }
        return "Install the OpenVPN core (e.g. `brew install openvpn`) or point AEGIS_OPENVPN at the binary — AegisVPN will use it automatically."
}

func fileExists(p string) bool {
        st, err := os.Stat(p)
        return err == nil && !st.IsDir()
}

func safeRelayName(s string) string {
        return strings.Map(func(r rune) rune {
                switch {
                case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
                        return r
                default:
                        return '-'
                }
        }, s)
}

func tailLog(path string, n int) []string {
        b, err := os.ReadFile(path)
        if err != nil {
                return nil
        }
        lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
        if len(lines) > n {
                lines = lines[len(lines)-n:]
        }
        return lines
}
