// Package registry is the AegisVPN node registry client: it talks to the
// coordination server (GET /v1/nodes), caches node lists to disk for offline
// fallback, and measures per-node TCP connect latency.
package registry

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net"
        "net/http"
        "os"
        "path/filepath"
        "sort"
        "sync"
        "time"
)

// Node mirrors the coordination server's /v1/nodes entry
// (docs/PROTOCOL.md section 4).
type Node struct {
        ID        string  `json:"id"`
        Country   string  `json:"country"`
        City      string  `json:"city"`
        Host      string  `json:"host"`
        Port      int     `json:"port"`
        Pubkey    string  `json:"pubkey"`
        Type      string  `json:"type"` // standard|p2p|double|onion|obfuscated|dedicated
        Load      float64 `json:"load"`
        LatencyMs int     `json:"latencyMs"`
}

// CoordBaseURL is the default coordination server.
const CoordBaseURL = "https://coord.aegisvpn.net"

// Client fetches and caches node lists.
type Client struct {
        base string
        root string // appRoot for the nodes.json cache
        hc   *http.Client
        mu   sync.Mutex
}

// New creates a registry client. The coordination base URL is taken from the
// AEGIS_COORD_URL environment variable, falling back to CoordBaseURL.
func New(appRoot string) *Client {
        base := os.Getenv("AEGIS_COORD_URL")
        if base == "" {
                base = CoordBaseURL
        }
        return &Client{
                base: base,
                root: appRoot,
                hc:   &http.Client{Timeout: 15 * time.Second},
        }
}

type wireNodes struct {
        Nodes []Node `json:"nodes"`
}

// List returns nodes from the coordination server, falling back to the disk
// cache (appRoot/nodes.json) when the server is unreachable. force skips the
// cache fallback (used to surface server errors to the caller).
func (c *Client) List(force bool) ([]Node, error) {
        c.mu.Lock()
        defer c.mu.Unlock()
        body, err := c.getJSON(c.base + "/v1/nodes")
        if err == nil {
                var wrapper wireNodes
                if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Nodes != nil {
                        c.saveCache(wrapper.Nodes)
                        return wrapper.Nodes, nil
                }
                var plain []Node
                if err := json.Unmarshal(body, &plain); err == nil && plain != nil {
                        c.saveCache(plain)
                        return plain, nil
                }
                return nil, fmt.Errorf("registry: unexpected /v1/nodes payload")
        }
        if !force {
                if nodes, cerr := c.loadCache(); cerr == nil {
                        return nodes, nil
                }
        }
        return nil, fmt.Errorf("registry: %w", err)
}

// Find returns the node with the given ID (cache-aware).
func (c *Client) Find(id string) (*Node, error) {
        nodes, err := c.List(false)
        if err != nil {
                return nil, err
        }
        for i := range nodes {
                if nodes[i].ID == id {
                        return &nodes[i], nil
                }
        }
        return nil, fmt.Errorf("registry: node %q not found", id)
}

// Measure TCP-connect-latency-tests every node (3s timeout per node, run
// concurrently, 8 at a time) and returns the list sorted by latency.
// This is a reachability probe — the real WireGuard handshake RTT is only
// observable with a live tunnel.
func (c *Client) Measure(nodes []Node) []Node {
        out := make([]Node, len(nodes))
        copy(out, nodes)
        sem := make(chan struct{}, 8)
        var wg sync.WaitGroup
        for i := range out {
                wg.Add(1)
                go func(n *Node) {
                        defer wg.Done()
                        sem <- struct{}{}
                        defer func() { <-sem }()
                        n.LatencyMs = tcpLatency(n.Host, n.Port, 3*time.Second)
                }(&out[i])
        }
        wg.Wait()
        sort.SliceStable(out, func(i, j int) bool { return out[i].LatencyMs < out[j].LatencyMs })
        return out
}

// tcpLatency measures one TCP connect to host:port; returns -1 on failure.
func tcpLatency(host string, port int, timeout time.Duration) int {
        if port <= 0 {
                port = 51820
        }
        ctx, cancel := context.WithTimeout(context.Background(), timeout)
        defer cancel()
        var d net.Dialer
        start := time.Now()
        conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
        if err != nil {
                return -1
        }
        conn.Close()
        return int(time.Since(start).Milliseconds())
}

func (c *Client) getJSON(url string) ([]byte, error) {
        req, err := http.NewRequest(http.MethodGet, url, nil)
        if err != nil {
                return nil, err
        }
        req.Header.Set("User-Agent", "AegisVPN")
        resp, err := c.hc.Do(req)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                return nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
        }
        return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func (c *Client) cachePath() string { return filepath.Join(c.root, "nodes.json") }

func (c *Client) saveCache(nodes []Node) {
        data, err := json.MarshalIndent(nodes, "", "  ")
        if err != nil {
                return
        }
        _ = os.MkdirAll(c.root, 0o755)
        _ = os.WriteFile(c.cachePath(), data, 0o600)
}

func (c *Client) loadCache() ([]Node, error) {
        data, err := os.ReadFile(c.cachePath())
        if err != nil {
                return nil, err
        }
        var nodes []Node
        if err := json.Unmarshal(data, &nodes); err != nil {
                return nil, err
        }
        return nodes, nil
}
