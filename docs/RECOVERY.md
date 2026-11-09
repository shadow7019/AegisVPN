# AegisVPN — source recovery seed

> **Status: recovery in progress.** The original `~/AegisVPN-remote` repository
> (5 commits, d4a4c08…9d953db) was destroyed by a sandbox environment reset
> before any commit reached GitHub. This directory lives inside the persistent
> project (`my-project`) so it survives future resets. Files below marked
> "verbatim" are byte-exact recoveries from session history / surviving copies;
> the remaining components are being rebuilt.

## Recovered (verbatim)

| File | Origin |
|---|---|
| `scripts/macho-merge.py` | verbatim — fat-binary merger, 16 KiB arm64 alignment |
| `scripts/build-macos-dmg-linux.sh` | verbatim v1.2.1 — full Linux DMG pipeline with round-trip verification |
| `scripts/install-macos.sh` | survived on disk (`public/downloads/`) — zero-Gatekeeper curl installer |
| `gui/desktop/cmd/aegisvpn-server/main.go` | verbatim — macOS/Windows/Linux dashboard host, elevation flow |

## Surviving release artifacts (verified byte-perfect this session)

In `my-project/public/downloads/` — all gateway-hash-verified:

- `AegisVPN-1.2.1.dmg` / `AegisVPN.dmg` (sha256 `edc3827d…`, round-trip verified)
- `AegisVPN-macos.zip` (sha256 `8e9449c2…`)
- `AegisVPN-Setup-x64.exe` (sha256 `6b793107…`, NSIS PE32)
- `AegisVPN-android.apk` (sha256 `c4cd9814…`, APK Signing Block, 4 ABIs)
- `AegisVPN-win-x64-portable.zip` (sha256 `ff0fa4ed…`)
- `SHA256SUMS.txt`, `RELEASES.md` (v1.2.1)

## To rebuild (lost with the reset)

- `engine/` — Go WireGuard engine (wireguard-go core, aegisvpn CLI)
- `gui/desktop/frontend` + `webapp` — dashboard frontend + go:embed host
- `gui/windows` — Wails GUI shell + NSIS installer (`windows/installer`)
- `android-java` — APK wrapper + no-Gradle build pipeline
- `server/` — VPS node deployment scripts, 80-node / 56-country fleet registry
- toolchain under `~/tools/` — go, libdmg-hfsplus (`dmg`, `hfsplus`),
  hfsprogs (`mkfs.hfsplus`), rcodesign, portable openssh
- deploy key `~/.ssh/id_ed25519` — must be regenerated and re-registered on
  GitHub before the first push

## Reproduce the macOS DMG (after toolchain restore)

```bash
GO_BIN=$HOME/tools/go/bin/go bash scripts/build-macos-dmg-linux.sh
```
