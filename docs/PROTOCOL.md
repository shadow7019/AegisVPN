# AegisVPN Shared Protocol Contract

This document is the **single source of truth** for interfaces shared between the
engine (Go), the desktop GUIs (Windows/macOS), the Android app and the servers.
Every component MUST conform to these contracts. Do not change them without
updating every consumer.

## 1. Engine library API (Go)

Module path: `github.com/shadow7019/AegisVPN/engine`

Package `aegis` (in `engine/internal/tunnel`, re-exported via `engine/aegis.go`)
is importable as a library by:

* the Wails desktop app (Windows/macOS),
* the gomobile Android binding (`engine/cmd/aegislib`).

```go
package aegis

type Status struct {
    State        string   `json:"state"`   // "disconnected"|"connecting"|"connected"|"disconnecting"
    Profile      string   `json:"profile,omitempty"`
    Node         string   `json:"node,omitempty"`
    LocalIP      string   `json:"localIp,omitempty"`
    Endpoint     string   `json:"endpoint,omitempty"`
    Hops         int      `json:"hops"`             // 1 = single, 2 = Double VPN
    KillSwitch   bool     `json:"killSwitch"`
    ThreatProtection bool `json:"threatProtection"`
    RxBytes      uint64   `json:"rxBytes"`
    TxBytes      uint64   `json:"txBytes"`
    HandshakeAt  int64    `json:"handshakeAt"`      // unix seconds of last handshake
    LatencyMs    int      `json:"latencyMs,omitempty"`
    Error        string   `json:"error,omitempty"`
}

type ConnectOpts struct {
    Profile          string   `json:"profile"`          // path or inline wg-quick conf
    Node             string   `json:"node,omitempty"`   // node id from registry
    Relay            string   `json:"relay,omitempty"`  // second hop node (Double VPN)
    KillSwitch       bool     `json:"killSwitch"`
    ThreatProtection bool     `json:"threatProtection"` // DNS ad/tracker blocking
    SplitExclude     []string `json:"splitExclude,omitempty"` // CIDRs excluded from tunnel
    SplitInclude     []string `json:"splitInclude,omitempty"` // CIDRs forced through tunnel
    Obfuscate        bool     `json:"obfuscate"`        // UDP obfuscation wrapper
    ObfsKey          string   `json:"obfsKey,omitempty"`
    AutoConnect      bool     `json:"autoConnect"`
}

// Library surface (kept gomobile-friendly: primitives, no error chaining)
func New(appRoot string) (*Client, error)
func (c *Client) Connect(opts ConnectOpts) error
func (c *Client) Disconnect() error
func (c *Client) Status() Status
func (c *Client) SetKillSwitch(enabled bool) error
func (c *Client) SetThreatProtection(enabled bool) error
func (c *Client) SetSplitTunnel(excludeCIDRs, includeCIDRs []string) error
func (c *Client) ApplyDNS(servers []string) error   // system DNS takeover
func (c *Client) Close() error
```

### Android gomobile binding (`engine/cmd/aegislib`, package name `aegislib`)

gomobile requires primitive types only:

```go
func Start(fd int64, wgConfig string, obfsEnabled bool, obfsKey string) error
// wgConfig = full wg-quick text (incl. AegisObfs extensions).
// fd = int from VpnService.Builder.establish(). Android side owns routes/DNS
// via Builder; Go side owns the WireGuard device over tun.CreateTUNFromFile.
func Stop() error
func StatusJSON() string // serialized aegis.Status
func Version() string
```

## 2. Local control API (daemon mode)

When run as `aegisvpn daemon`, the engine serves JSON over
`127.0.0.1:5599` (Windows) / unix socket `$appRoot/engine.sock` (macOS/Linux).

All requests: `{"op": string, "args": object}` → `{"ok": bool, "data": object, "error": string}`.

| op                  | args                                     | data                  |
|---------------------|------------------------------------------|-----------------------|
| `status`            | `{}`                                     | `Status`              |
| `connect`           | `ConnectOpts`                            | `Status`              |
| `disconnect`        | `{}`                                     | `Status`              |
| `killswitch`        | `{"enabled": bool}`                      | `{}`                  |
| `threat_protection` | `{"enabled": bool}`                      | `{}`                  |
| `split_tunnel`      | `{"exclude":[],"include":[]}`            | `{}`                  |
| `dns`               | `{"servers":[]}`                         | `{}`                  |
| `nodes`             | `{}`                                     | `[Node]`              |
| `breach_check`      | `{"email": string}`                      | `{"breaches":[...]}`  |
| `meshnet_peers`     | `{}`                                     | `[MeshPeer]`          |

## 3. WireGuard profile format

Profiles are standard **wg-quick** `.conf` files with two optional AegisVPN
extensions in `[Interface]`:

```ini
[Interface]
PrivateKey = <base64>
Address = 10.8.0.2/32
DNS = 10.8.0.1
# AegisVPN extensions:
AegisObfs = true            # optional: UDP obfuscation
AegisObfsKey = <passphrase> # optional: obfuscation passphrase

[Peer]
PublicKey = <server base64>
AllowedIPs = 0.0.0.0/0
Endpoint = vpn.example.com:51820
PersistentKeepalive = 25
```

## 4. Node registry (coordination server)

`GET https://<coord>/v1/nodes` →
```json
{"nodes":[{"id":"us-east-1","country":"US","city":"Ashburn",
  "host":"us-east-1.example.com","port":51820,"pubkey":"<base64>",
  "type":"standard|p2p|double|onion|obfuscated|dedicated",
  "load":0.42,"latencyMs":88}]}
```

`POST /v1/mesh/register` / `GET /v1/mesh/peers` for Meshnet.

## 5. Platform responsibilities

| Concern            | Windows                    | macOS                          | Android                    |
|--------------------|----------------------------|--------------------------------|----------------------------|
| TUN device         | WireGuardNT/WinTun (tun pkg)| utun via `wireguard/tun`      | `VpnService.Builder.establish()` fd |
| Routes             | `route` + firewall lockdown| `route -n add` + pf rules     | split via `addDisallowedApplication` |
| DNS takeover       | registry/`netsh` per-iface | scoped resolver settings      | `addDnsServer`             |
| Kill switch        | block-all firewall rules   | pf anchor default-drop        | `protect()` + disallow all |
| Split tunnel       | route-based + WFP note     | route-based                   | per-app disallow list      |
| Packaging          | Inno Setup → `.exe`        | hdiutil → `.dmg`              | gradle → `.apk`            |

## 6. Non-negotiables

1. **Real WireGuard only** — `golang.zx2c4.com/wireguard/{device,conn,tun}`.
   No mock tunnels, no fake IP stacks.
2. Engine compiles for `linux/amd64`, `windows/amd64`, `darwin/arm64`, `darwin/amd64`.
3. Android binds via gomobile package `aegislib`.
4. Never trust the GUI with secrets: keys live in the engine keychain dir.
