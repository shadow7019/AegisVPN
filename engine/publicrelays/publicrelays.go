// Package publicrelays gives every AegisVPN front end (CLI, aegisvpn-server
// dashboard, native GUIs) one shared way to browse and connect to the free
// public relay network — so users never have to hand-download an .ovpn file
// and import it into a third-party client.
//
// The relay index is our companion portal (pre-verified every few minutes)
// with the raw VPNGate CSV as a fallback. Everything is pure stdlib: no new
// dependencies for the mobile bindings.
package publicrelays

import (
        "context"
        "encoding/csv"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net"
        "net/http"
        "net/url"
        "os"
        "sort"
        "strconv"
        "strings"
        "sync"
        "time"
)

// Relay is one connectable public server.
type Relay struct {
        Hostname    string `json:"hostname"`
        IP          string `json:"ip"`
        Port        int    `json:"port"`
        Proto       string `json:"proto"`
        CountryCode string `json:"country_code"`
        CountryName string `json:"country_name"`
        Ping        int    `json:"ping_ms"`
        Speed       int64  `json:"speed_bps"`
        Score       int64  `json:"score"`
        Sessions    int    `json:"sessions"`
        ProbeMs     int    `json:"probe_ms"`
        Online      bool   `json:"online"`
        LogPolicy   string `json:"log_policy"`
        Operator    string `json:"operator"`
}

// Endpoints — the companion portal is the primary index, VPNGate's own CSV
// the fallback. Override with AEGIS_RELAY_INDEX / AEGIS_RELAY_CONFIG.
const (
        DefaultIndexURL  = "https://aegis-vpn-app--1t6ikre7.preview.emergentagent.com/api/servers?limit=2000"
        DefaultConfigURL = "https://aegis-vpn-app--1t6ikre7.preview.emergentagent.com/api/servers/{host}/config"
        FallbackListURL  = "https://www.vpngate.net/api/iphone/"
        CacheTTL         = 60 * time.Second
        ProbeTimeout     = 3 * time.Second
        FetchTimeout     = 20 * time.Second
)

func env(key, def string) string {
        if v := os.Getenv(key); v != "" {
                return v
        }
        return def
}

// IndexURL / ConfigTemplate resolve the active endpoints.
func IndexURL() string  { return env("AEGIS_RELAY_INDEX", DefaultIndexURL) }
func ConfigTemplate() string { return env("AEGIS_RELAY_CONFIG", DefaultConfigURL) }

var httpClient = &http.Client{Timeout: FetchTimeout}

// ---------------------------------------------------------------- index ----

type cacheEntry struct {
        relays  []Relay
        fetched time.Time
        busy    bool
}

var (
        cacheMu sync.Mutex
        cache   cacheEntry
)

// List returns the relay index, cached for CacheTTL. force bypasses it.
func List(ctx context.Context, force bool) ([]Relay, error) {
        cacheMu.Lock()
        if !force && time.Since(cache.fetched) < CacheTTL && cache.relays != nil {
                out := cache.relays
                cacheMu.Unlock()
                return out, nil
        }
        if cache.busy {
                cacheMu.Unlock()
                // another goroutine is refreshing; wait briefly for it
                deadline := time.Now().Add(FetchTimeout)
                for time.Now().Before(deadline) {
                        time.Sleep(100 * time.Millisecond)
                        cacheMu.Lock()
                        if cache.relays != nil {
                                out := cache.relays
                                cacheMu.Unlock()
                                return out, nil
                        }
                        cacheMu.Unlock()
                }
                return nil, errors.New("relay list refresh already in progress")
        }
        cache.busy = true
        cacheMu.Unlock()

        relays, err := fetchIndex(ctx)

        cacheMu.Lock()
        cache.busy = false
        if err == nil {
                cache.relays = relays
                cache.fetched = time.Now()
        }
        cacheMu.Unlock()
        return relays, err
}

func fetchIndex(ctx context.Context) ([]Relay, error) {
        relays, err := fetchPortalIndex(ctx)
        if err == nil && len(relays) > 0 {
                return relays, nil
        }
        // portal unavailable → VPNGate CSV fallback
        vg, vgErr := fetchVPNGateIndex(ctx)
        if vgErr == nil && len(vg) > 0 {
                return vg, nil
        }
        if err != nil && vgErr != nil {
                return nil, fmt.Errorf("relay index unavailable (portal: %v; vpngate: %v)", err, vgErr)
        }
        return relays, err
}

type portalServer struct {
        Hostname    string `json:"hostname"`
        IP          string `json:"ip"`
        Port        int    `json:"port"`
        Proto       string `json:"proto"`
        CountryCode string `json:"country_code"`
        CountryName string `json:"country_name"`
        Ping        int    `json:"ping"`
        Speed       int64  `json:"speed"`
        Score       int64  `json:"score"`
        Sessions    int    `json:"sessions"`
        ProbeMs     int    `json:"probe_ms"`
        Online      bool   `json:"online"`
        LogPolicy   string `json:"log_policy"`
        Operator    string `json:"operator"`
}

func fetchPortalIndex(ctx context.Context) ([]Relay, error) {
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, IndexURL(), nil)
        if err != nil {
                return nil, err
        }
        req.Header.Set("User-Agent", "AegisVPN/1.3 (+https://github.com/shadow7019/AegisVPN)")
        resp, err := httpClient.Do(req)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                return nil, fmt.Errorf("portal index HTTP %d", resp.StatusCode)
        }
        var raw []portalServer
        if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&raw); err != nil {
                return nil, err
        }
        out := make([]Relay, 0, len(raw))
        for _, s := range raw {
                out = append(out, Relay{
                        Hostname:    s.Hostname,
                        IP:          s.IP,
                        Port:        s.Port,
                        Proto:       s.Proto,
                        CountryCode: s.CountryCode,
                        CountryName: s.CountryName,
                        Ping:        s.Ping,
                        Speed:       s.Speed,
                        Score:       s.Score,
                        Sessions:    s.Sessions,
                        ProbeMs:     s.ProbeMs,
                        Online:      s.Online,
                        LogPolicy:   s.LogPolicy,
                        Operator:    s.Operator,
                })
        }
        return out, nil
}

// fetchVPNGateIndex parses the classic VPNGate CSV (#Host,#IP,#Port,...).
func fetchVPNGateIndex(ctx context.Context) ([]Relay, error) {
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, FallbackListURL, nil)
        if err != nil {
                return nil, err
        }
        resp, err := httpClient.Do(req)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()
        body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
        if err != nil {
                return nil, err
        }
        // skip the leading comment lines (# ...)
        text := string(body)
        start := 0
        for _, line := range strings.Split(text, "\n") {
                if strings.HasPrefix(line, "#") {
                        start += len(line) + 1
                        continue
                }
                break
        }
        text = text[start:]
        r := csv.NewReader(strings.NewReader(text))
        r.FieldsPerRecord = -1
        rows, err := r.ReadAll()
        if err != nil {
                return nil, err
        }
        if len(rows) < 2 {
                return nil, errors.New("vpngate csv: no rows")
        }
        header := rows[0]
        col := func(name string) int {
                for i, h := range header {
                        if h == name {
                                return i
                        }
                }
                return -1
        }
        cHost, cIP, cPort := col("#Host"), col("#IP"), col("#Port")
        cCC, cCN, cPing := col("#CountryShort"), col("#CountryLong"), col("#Ping")
        cSpeed, cScore, cSess := col("#Speed"), col("#Score"), col("#NumVpnSessions")
        cLog := col("#LogType")
        if cIP < 0 || cPort < 0 {
                return nil, errors.New("vpngate csv: missing columns")
        }
        atoi := func(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
        atoi64 := func(s string) int64 { n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64); return n }
        out := make([]Relay, 0, len(rows)-1)
        for _, row := range rows[1:] {
                if len(row) <= cPort {
                        continue
                }
                get := func(i int) string {
                        if i < 0 || i >= len(row) {
                                return ""
                        }
                        return row[i]
                }
                port := atoi(get(cPort))
                if port == 0 {
                        continue
                }
                out = append(out, Relay{
                        Hostname:    get(cHost),
                        IP:          get(cIP),
                        Port:        port,
                        Proto:       "tcp",
                        CountryCode: get(cCC),
                        CountryName: get(cCN),
                        Ping:        atoi(get(cPing)),
                        Speed:       atoi64(get(cSpeed)),
                        Score:       atoi64(get(cScore)),
                        Sessions:    atoi(get(cSess)),
                        Online:      true,
                        LogPolicy:   get(cLog),
                })
        }
        return out, nil
}

// ------------------------------------------------------------- helpers ----

// Best returns the highest-scored online relay for a country ("" = global).
func Best(relays []Relay, country string) (Relay, bool) {
        best := Relay{}
        found := false
        cc := strings.ToUpper(country)
        for _, r := range relays {
                if !r.Online {
                        continue
                }
                if cc != "" && cc != "ANY" && !strings.EqualFold(r.CountryCode, cc) {
                        continue
                }
                if !found || r.Score > best.Score {
                        best, found = r, true
                }
        }
        return best, found
}

// Probe measures TCP connect latency to a relay endpoint.
func Probe(ctx context.Context, ip string, port int) (int, error) {
        d := net.Dialer{Timeout: ProbeTimeout}
        start := time.Now()
        c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
        if err != nil {
                return 0, err
        }
        _ = c.Close()
        return int(time.Since(start).Milliseconds()), nil
}

// Profile downloads a ready-to-run OpenVPN config for a relay hostname.
func Profile(ctx context.Context, hostname string) ([]byte, error) {
        tpl := ConfigTemplate()
        if !strings.Contains(tpl, "{host}") {
                return nil, errors.New("config template missing {host}")
        }
        u := strings.ReplaceAll(tpl, "{host}", url.PathEscape(hostname))
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
        if err != nil {
                return nil, err
        }
        req.Header.Set("User-Agent", "AegisVPN/1.3 (+https://github.com/shadow7019/AegisVPN)")
        resp, err := httpClient.Do(req)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                return nil, fmt.Errorf("profile %s: HTTP %d", hostname, resp.StatusCode)
        }
        return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// Sort helpers used by the UIs.
func SortByScore(relays []Relay) {
        sort.SliceStable(relays, func(i, j int) bool { return relays[i].Score > relays[j].Score })
}

func SortByCountry(relays []Relay) {
        sort.SliceStable(relays, func(i, j int) bool {
                if relays[i].CountryCode != relays[j].CountryCode {
                        return relays[i].CountryCode < relays[j].CountryCode
                }
                return relays[i].Score > relays[j].Score
        })
}
