# AegisVPN for macOS

SwiftUI control app + privileged engine daemon. The tunneling itself is **real
WireGuard** (`golang.zx2c4.com/wireguard-go`): the app never simulates anything.

## Architecture (honest version)

```
┌─────────────────────────────────┐        ┌───────────────────────────────────────┐
│ AegisVPN.app  (SwiftUI, GUI)    │        │ net.aegisvpn.daemon  (root launchd)   │
│ net.aegisvpn.AegisVPN           │  UDS   │ /Library/PrivilegedHelperTools/       │
│ sandboxed, app group            ├───────►│   net.aegisvpn.daemon                 │
│ group.net.aegisvpn              │ JSONL  │ RunAtLoad + KeepAlive                 │
└─────────────────────────────────┘        └──────────────────┬────────────────────┘
        op: status/connect/...                                │ wireguard-go tun pkg
        {"ok","data","error"}                                 ▼
                                                     utunNN (real TUN device)
                                                              │
                                                     routes · scoped DNS · pf
                                                     kill-switch anchor
```

* The **engine** (`aegisvpn`, built from `../engine`) is the only privileged
  component. It runs as a root launchd daemon, creates the **utun** interface
  through wireguard-go's `tun` package, owns routes (`route -n add`), DNS
  takeover (scoped resolvers) and the **pf** default-drop kill-switch anchor —
  exactly the macOS column of `docs/PROTOCOL.md` §5.
* The **app** is a pure control plane: it speaks the local JSON API of
  PROTOCOL.md §2 over a unix domain socket and never sees private keys (they
  live in the engine's keychain dir, §6.4).
* There is **no embedded SMJobBless helper**. The daemon binary is shipped
  inside the app bundle (`Contents/Resources/aegisvpn`) and installed by
  `daemon/install-daemon.sh`, executed through an administrator prompt.

### Control socket resolution (EngineClient)

The GUI tries these paths in order and keeps the first that connects:

1. `/var/run/aegisvpn.sock` — root daemon (preferred; launchd plist passes
   `daemon --socket /var/run/aegisvpn.sock`)
2. `/var/run/aegisvpn/engine.sock` — the `$appRoot/engine.sock` convention of
   PROTOCOL.md §2 (`AEGISVPN_APP_ROOT=/var/run/aegisvpn` is exported by the
   plist)
3. `~/Library/Group Containers/group.net.aegisvpn/engine.sock` — app-group
   path (works best from a sandboxed GUI if the engine is configured for it)
4. `~/Library/Application Support/AegisVPN/engine.sock` — user-mode engine
   (running `aegisvpn daemon` unprivileged for development)

Ops implemented: `status`, `connect`, `disconnect`, `killswitch`,
`threat_protection`, `split_tunnel`, `nodes`, `breach_check`
(+ `meshnet_peers`, `dns`). Wire format is one JSON object per line:
`{"op": "...", "args": {...}}` → `{"ok": bool, "data": ..., "error": "..."}`.

> Note for the engine author: the macOS launchd plist expects the `daemon`
> subcommand to accept `--socket <path>`; if only the `$appRoot` convention
> exists, the GUI still finds it at `/var/run/aegisvpn/engine.sock`.

### Privileged install flow (onboarding)

1. Dashboard shows an onboarding card until the daemon answers.
2. "Install Daemon" → `osascript -e 'do shell script "bash …/install-daemon.sh
   <bundle>" with administrator privileges with prompt …'` (user types their
   admin password once).
3. `install-daemon.sh` (root): copies the engine to
   `/Library/PrivilegedHelperTools/net.aegisvpn.daemon`, re-signs ad-hoc,
   copies the plist to `/Library/LaunchDaemons/`, `plutil -lint`, bootout of
   any old instance, `launchctl bootstrap system …`, then waits for the
   control socket. `/var/run/aegisvpn/` is created as the app-root dir
   (note: `/var/run` is cleaned at boot; the daemon recreates its socket via
   `RunAtLoad`).
4. `uninstall-daemon.sh` reverses everything. Detection helpers (socket,
   `pgrep`, `launchctl print system/net.aegisvpn.daemon`) live in
   `AegisVPN/DaemonInstaller.swift`.

### Sandbox notes

The app is sandboxed with `com.apple.security.app-sandbox`,
`com.apple.security.application-groups = [group.net.aegisvpn]` and
`com.apple.security.network.client`. Because the daemon socket lives under
`/var/run`, the entitlements add
`com.apple.security.temporary-exception.files.absolute-paths.read-write` for
`/var/run/aegisvpn.sock` and `/var/run/aegisvpn/`. That exception is fine for
direct distribution but **not Mac App Store compatible** — for MAS you would
run the tunnel inside a Network Extension and drop the exception.

## Building

```bash
# on macOS 13+ with Xcode 15, Go and xcodegen installed
scripts/build-macos.sh                # host-arch engine
scripts/build-macos.sh --universal    # arm64 + amd64 engine via lipo
scripts/build-macos.sh --skip-engine  # reuse previously built engine
scripts/build-macos.sh --clean
```

The script:

1. builds the Go engine (`engine/`, module
   `github.com/shadow7019/AegisVPN/engine`) for `darwin/arm64`/`darwin/amd64`
   (`--universal` merges them with `lipo`),
2. generates `macos/AegisVPN.xcodeproj` with `xcodegen generate`
   (`macos/project.yml`),
3. `xcodebuild -configuration Release` builds `AegisVPN.app`,
4. embeds the engine + `install-daemon.sh` / `uninstall-daemon.sh` +
   `net.aegisvpn.daemon.plist` into `Contents/Resources`,
5. ad-hoc codesigns (`codesign --force --deep --sign -`) and verifies,
6. stages `AegisVPN.app` + an `/Applications` symlink and creates the DMG:

```bash
hdiutil create -volname AegisVPN -srcfolder <staging> -ov -format UDZO dist/AegisVPN.dmg
```

Final artifact path is printed as `ARTIFACT: …/dist/AegisVPN.dmg`.

## Notarization (optional, for distribution outside the Mac App Store)

The build script signs ad-hoc so it works on any machine with Xcode. To ship:

```bash
IDENTITY="Developer ID Application: Your Name (TEAMID)"
codesign --force --deep --options runtime --entitlements macos/AegisVPN.entitlements \
  --sign "$IDENTITY" build/DerivedData/Build/Products/Release/AegisVPN.app
ditto -c -k --keepParent AegisVPN.app AegisVPN.zip
xcrun notarytool submit AegisVPN.zip --keychain-profile aegisvpn --wait
xcrun stapler staple AegisVPN.app
# rebuild the DMG from the stapled app (same staging step as the script)
```

The privileged daemon binary installed by `install-daemon.sh` should be signed
with the same Developer ID; launchd accepts ad-hoc locally.

## File map

```
macos/
  project.yml                 XcodeGen definition (target AegisVPN, scheme, Info.plist props)
  AegisVPN.entitlements       sandbox + app group + /var/run socket exception
  AegisVPN/
    AegisVPNApp.swift         @main, dark appearance, AppModel (polling, settings, connect)
    Models.swift              Status/ConnectOpts (PROTOCOL §1), Node (§4), MeshPeer, BreachReport
    EngineClient.swift        actor client for PROTOCOL §2 (NWConnection, JSONL, socket probing)
    DaemonInstaller.swift     status detection + osascript admin install/uninstall
    Theme.swift               navy/teal palette (#17C7BD), badges, load bars, formatters
    Views/
      ContentView.swift       sidebar: Dashboard / Servers / Settings / Dark Web Monitor
      DashboardView.swift     circular connect button, status ring + pulse, live stats, onboarding
      ServersView.swift       country sections, load bars, type badges, double-hop chain picker
      SettingsView.swift      Kill Switch / Threat Protection / Auto-connect / Obfuscated-only,
                              split-tunnel CIDR editors, daemon install/uninstall
      BreachCheckView.swift   Dark Web Monitor (breach_check)
    Assets.xcassets/          AccentColor #17C7BD + AppIcon placeholder
  daemon/
    net.aegisvpn.daemon.plist root LaunchDaemon (RunAtLoad+KeepAlive, --socket /var/run/aegisvpn.sock)
    install-daemon.sh         privileged installer (helper + plist + bootstrap + socket wait)
    uninstall-daemon.sh       privileged uninstaller
scripts/build-macos.sh        engine + xcodegen + xcodebuild + codesign + dist/AegisVPN.dmg
```

## Known limitations (by design, for now)

* No embedded helper: install needs one admin password prompt (osascript).
  `SMAppService` (macOS 13) with a signed launchd daemon is the longer-term
  blessed path and can reuse the same plist/scripts.
* The GUI does not ship Developer ID-signed/notarized by default; `--universal`
  only affects the engine binary (the app itself builds universal via Xcode).
* `MeshPeer` and `Breach` field names are provisional — PROTOCOL.md lists the
  ops but not their exact shapes, so decoding is tolerant of extra/missing
  fields.
