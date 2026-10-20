# AegisVPN

**A real WireGuard-based VPN suite — an open, self-hosted alternative to NordVPN's premium feature set.**

No mock tunnels. No fake IP stacks. AegisVPN drives the actual WireGuard protocol
(`golang.zx2c4.com/wireguard`) through kernel-grade TUN interfaces — WinTun/WireGuardNT on
Windows, utun on macOS, Linux TUN, and Android `VpnService` — and ships native clients for
every platform plus a deployable server fleet.

```
┌────────────────────────────  CLIENTS  ────────────────────────────┐
│  Windows (WPF + engine service)   macOS (SwiftUI + launchd daemon)│
│  Android (VpnService + gomobile)  Linux (CLI + systemd)           │
└────────────────────────────────┬──────────────────────────────────┘
              ChaCha20-Poly1305 / Curve25519 (WireGuard, NordLynx-grade)
┌────────────────────────────────▼──────────────────────────────────┐
│                            NODE FLEET                             │
│  standard · p2p · double-entry → double-exit · obfuscated · onion │
│  coord API · threat DNS · SmartPlay DNS · Tor SOCKS · obfsd       │
└───────────────────────────────────────────────────────────────────┘
```

---

## Premium feature matrix

| Feature | Status | Where |
|---|---|---|
| WireGuard tunnel (NordLynx-grade) | ✅ real | `engine/internal/tunnel` |
| Kill Switch (block-all firewall lockdown) | ✅ | `engine/internal/router` (per-OS) |
| Split Tunneling (include/exclude CIDRs) | ✅ | `engine/internal/router`, Android per-app |
| Double VPN (multi-hop) | ✅ | entry→exit chains via `server/relay` |
| Onion over VPN | ✅ | `server/tor` (Tor SOCKS behind exit node) |
| Obfuscated servers (DPI-resistant UDP) | ✅ | `engine/internal/obfs` + `server/obfsd` |
| P2P-optimized nodes | ✅ | node type `p2p`, fleet roles |
| Dedicated IP nodes | ✅ | node type `dedicated` |
| Threat Protection (ads/trackers/malware DNS) | ✅ | `engine/internal/dns` + `server/threatdns` |
| Dark Web Monitor (HIBP breach alerts) | ✅ | `engine/internal/features/hibp.go` |
| SmartPlay / SmartDNS (streaming unlock) | ✅ | `engine/internal/features/smartplay.go` + `server/smartdns` |
| Meshnet (device-to-device) | ✅ | `engine/internal/features/meshnet.go` + coord |
| Auto-connect / boot persistence | ✅ | boot receivers, systemd, launchd, Windows service |
| Multi-platform native GUIs | ✅ | WPF / SwiftUI / Android / Wails cross-GUI |

---

## Repository layout

```
engine/            Go engine — WireGuard device, routing, DNS, kill switch, obfs,
                   node registry, local control daemon (JSON line API)
cmd/aegisvpn       engine CLI + daemon
cmd/aegislib       gomobile binding for Android
gui/desktop        cross-platform desktop GUI (Wails + React)
windows/
  wpf/AegisVPN.Wpf native Windows WPF GUI (.NET 8, MVVM) driving the engine
  installer/       Inno Setup script → AegisVPN-Setup.exe
  service/         install/uninstall as a Windows service (sc.exe)
macos/             native SwiftUI app + launchd daemon (install/uninstall scripts)
android/           Kotlin app — VpnService + wireguard-go (gomobile aegislib)
systemd/           Linux unit files + install/uninstall scripts
server/            node stack: coord, relay (Double VPN), obfsd, threatdns,
                   smartdns, tor — docker-compose one-command deployment
deploy/            fleet deployment suite: deploy-node.sh, deploy-fleet.sh,
                   peer.sh (client provisioning), verify.sh
scripts/           per-platform build + packaging scripts (exe/dmg/apk)
docs/PROTOCOL.md   the interface contract every component conforms to
```

---

## The engine

The engine is a real WireGuard implementation (WireGuard® is a registered trademark of
Jason A. Donenfeld — AegisVPN is an independent implementation using the official
`wireguard-go` libraries).

* **TUN**: WinTun/WireGuardNT (Windows), utun (macOS), `/dev/net/tun` (Linux),
  `VpnService.Builder.establish()` fd (Android).
* **Routing & kill switch**: per-OS policy takeover — firewall lockdown rules on Windows,
  pf anchors on macOS, ip rules on Linux — so traffic dies when the tunnel dies.
* **DNS**: system DNS takeover + Threat Protection blocklists (ads / trackers / malware).
* **Control plane**: `aegisvpn daemon` exposes the JSON-line API on `127.0.0.1:5599`
  (Windows) or a unix socket (macOS/Linux) — GUIs are thin, keys never leave the engine.

### Build (cross-compiles from anywhere with Go ≥ 1.22)

```bash
cd engine
GOOS=linux   GOARCH=amd64 go build -o ../dist/aegisvpn-linux-amd64        ./cmd/aegisvpn
GOOS=windows GOARCH=amd64 go build -o ../dist/aegisvpn-windows-amd64.exe ./cmd/aegisvpn
GOOS=darwin  GOARCH=arm64 go build -o ../dist/aegisvpn-darwin-arm64      ./cmd/aegisvpn
```

### Try it (CLI)

```bash
aegisvpn daemon                 # control API for GUIs
aegisvpn connect -node us-east-1 -killswitch -threat
aegisvpn connect my-profile.conf -obfs -obfs-key hunter2
aegisvpn status && aegisvpn disconnect
```

---

## Native clients

### Windows — WPF (`.exe` installer)

```powershell
# engine + GUI
dotnet publish windows/wpf/AegisVPN.Wpf -c Release -r win-x64 -p:PublishSingleFile=true
# installer (Inno Setup)
iscc windows/installer/aegisvpn.iss        # → AegisVPN-Setup.exe
```

`windows/service/install-service.ps1` registers the engine as a Windows service so the
GUI (and kill switch) survive reboots. The WPF app auto-elevates (requireAdministrator)
because TUN + firewall control need admin.

### macOS — SwiftUI (`.dmg`)

```bash
cd macos && xcodegen && xcodebuild -project AegisVPN.xcodeproj -scheme AegisVPN build
scripts/build-macos.sh                      # → AegisVPN.dmg
macos/daemon/install-daemon.sh              # launchd service (net.aegisvpn.daemon)
```

### Android — `.apk`

```bash
cd engine && gomobile bind -target=android -o ../android/app/libs/aegislib.aar ./cmd/aegislib
cd ../android && ./gradlew assembleRelease  # → app-release.apk
```

`VpnService.Builder.establish()` hands the real tun fd to wireguard-go; split tunneling
uses `addDisallowedApplication`, kill switch uses always-on + lockdown mode.

---

## Server fleet deployment (real VPS nodes)

One command per node, or roll out an entire fleet in parallel:

```bash
# single node (run locally on the VPS)
sudo deploy/deploy-node.sh --local --role standard --country DE --city Falkenstein

# or from your laptop, over SSH
deploy/deploy-node.sh --host root@203.0.113.10 --role double-entry --country NL --city Amsterdam

# whole fleet, parallel, with summary table
cp deploy/fleet.conf.example deploy/fleet.conf && $EDITOR deploy/fleet.conf
deploy/deploy-fleet.sh

# daily driver: issue / list / revoke client peers
deploy/peer.sh add --host root@203.0.113.10 node-1 alice
deploy/peer.sh list node-1
deploy/peer.sh revoke node-1 alice

# post-deploy health checks
deploy/verify.sh --host root@203.0.113.10
```

Roles: `standard`, `p2p`, `double-entry`, `double-exit`, `obfs`. The suite handles docker
install, sysctl/BBR hardening, UFW rules, real key generation, coord registration and
client profile emission. See `deploy/README.md`.

---

## Pushing to GitHub

The repo remote is `git@github.com:shadow7019/AegisVPN.git`. The sandbox ships no ssh
client, so `scripts/push.sh` uses a tiny Go-based ssh transport:

1. Copy `~/.ssh/id_ed25519.pub` into GitHub → repo *Deploy keys* (enable **write**) or
   account SSH keys.
2. `./scripts/push.sh`

---

## Security notes

* Private keys never leave the engine process / root-owned keychain dir (`~/.aegisvpn`).
* Coordination API is bearer-token protected; `deploy/env.example` documents rotation.
* Kill switch is default-deny: firewall rules block all non-tunnel traffic.
* Obfuscation key (`AegisObfs`) is optional per-profile; rotate alongside node keys.

## License

MIT — see `LICENSE`. WireGuard and the WireGuard logo are registered trademarks of
Jason A. Donenfeld; AegisVPN is not affiliated with Nord Security or NordVPN.
