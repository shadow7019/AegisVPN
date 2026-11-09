# AegisVPN 1.2.1 — release artifacts

All builds are reproducible from this repository (see Reproduce below).
Verify every download against `SHA256SUMS.txt` before installing.

## Downloads (v1.2.1)

| File | Platform | Size | What it is |
|---|---|---:|---|
| `AegisVPN-android.apk` | Android 7.0+ (arm64/armv7/x86/x86_64) | ~6.7 MB | Real WireGuard Go engine (`libwg-go.so` under `lib/`), branded adaptive icon, signed (v2+v3) |
| `AegisVPN-Setup-x64.exe` | Windows 10/11 x64 | ~6.9 MB | NSIS installer: branded GUI + engine + fleet registry + service scripts + shortcuts |
| `AegisVPN-win-x64-portable.zip` | Windows 10/11 x64 | ~8.6 MB | Portable folder: GUI exe, engine CLI, registry, service scripts, README |
| `AegisVPN-1.2.1.dmg` | macOS 12+ (Apple Silicon & Intel) | ~14.8 MB | **Signed universal app in a true Apple UDIF image (HFS+, koly trailer)** — versioned filename |
| `AegisVPN.dmg` | macOS | ~14.8 MB | Byte-identical alias of `AegisVPN-1.2.1.dmg` kept for old links |
| `AegisVPN-macos.zip` | macOS | ~14.7 MB | The signed universal app as a plain ZIP (used by the one-line installer) |

## What changed in 1.2.1 (vs 1.2.0) — "still not working / only 14 MB?"

- **Versioned filename — cache-proof delivery.** All three earlier rebuilds
  kept the same `AegisVPN.dmg` URL, so a browser that had cached the first,
  broken v1.2.0 download could keep handing it back even after a
  re-download. v1.2.1 ships as `AegisVPN-1.2.1.dmg` (a file that cannot
  exist in any cache), the download server now sends
  `Cache-Control: no-store` for `/downloads/*`, and the alias
  `AegisVPN.dmg` is refreshed byte-for-byte for old links. If your file's
  SHA-256 matches `edc3827d…` you have the fixed build.
- **"Only 14 MB?" — that is correct.** AegisVPN is a native Go engine
  (wireguard-go core, like NordLynx) with the dashboard and the 80-node
  fleet registry compiled in. It does not bundle a browser runtime;
  Electron-based apps are 150–200 MB because they ship Chromium. Unpacked,
  the app is ~33 MB (16 MB universal binary + both CLI slices + resources).
  The bundle README now explains this, plus a diagnostic: run
  `/Applications/AegisVPN.app/Contents/MacOS/AegisVPN` directly from
  Terminal to see real output (a healthy start ends with
  `aegisvpn dashboard on http://127.0.0.1:8080`).
- **ZIP alternative published.** `AegisVPN-macos.zip` (the artifact the
  one-line installer has always used) is now also offered on the download
  page for manual installs, as a fallback if a DMG ever fails to mount.
- DMG content is unchanged from the verified 1.2.0 fix: bundle seal
  (`_CodeSignature/CodeResources`) present, round-trip byte-verification
  passing, ADHOC signatures on all three Mach-O images.

## What changed in 1.2.0 (vs 1.1.0) — "application doesn't run on Mac" fixed

- **Root cause 1 — dead launcher.** The v1.1.0 launcher started the engine
  daemon with `--http 127.0.0.1:8080`, a flag that never existed in the
  engine (`daemon` speaks a JSON-line protocol over a Unix socket on macOS).
  The daemon exited instantly and the browser opened a dead port, so the
  app appeared to "not run".
- **Root cause 2 — unsigned app bundle.** The main executable was a shell
  script, which cannot carry a code signature, and the bundle had no
  CodeResources. Quarantined unsigned apps are hard-blocked by Gatekeeper
  on modern macOS ("app is damaged" / "cannot verify the developer").
- **Fix — real signed app.** `AegisVPN.app` now contains a compiled,
  ad-hoc code-signed **universal (x86_64 + arm64) `AegisVPN` binary**
  (signed per-slice with embedded LC_CODE_SIGNATURE + bundle
  `CodeResources`, produced with `rcodesign` 0.29.0). Both architecture
  slices and the CLI binaries in `Contents/MacOS/cli/` are signed.
- **Fix — real browser dashboard served by the app itself.** The engine
  gained a full HTTP UI host (`gui/desktop/cmd/aegisvpn-server` +
  `gui/desktop/webapp`): it serves the production React frontend embedded
  in the binary and bridges `window.go.main.App` calls to the engine
  library over `POST /api/rpc`. The double-click flow is:
  health-check → admin elevation (osascript, asked once — utun needs
  root) → re-exec as root → wait for the listener → open
  `http://127.0.0.1:8080`.
- **Offline-first server list.** If the coordination server is
  unreachable, the dashboard serves the bundled fleet (80 nodes / 56
  countries) from `Contents/Resources/nodes.json` — previously the UI
  errored out with no servers.
- **Fix — bundle seal actually shipped.** The first v1.2.0 DMG embedded
  ad-hoc signatures in every Mach-O but the signing step's
  `Contents/_CodeSignature/CodeResources` bundle seal was never written
  into the DMG's HFS+ volume — macOS opened the image, found a bundle
  whose main executable claims a signature while the seal is missing, and
  reported exactly *"AegisVPN is damaged and can't be opened"*. The DMG
  was rebuilt with `_CodeSignature/CodeResources` present (sealing all
  resources + nested CLI binaries), and `scripts/build-macos-dmg-linux.sh`
  now ends with a **round-trip verification**: DMG → HFS+ → per-file byte
  compare against the originals, exec-bit check and ADHOC-signature check;
  the build fails hard on any mismatch.
- **Build pipeline.** `scripts/build-macos-dmg-linux.sh` reproduces the
  whole deliverable on Linux: frontend build → Go universal binary
  (`scripts/macho-merge.py`, no lipo needed) → rcodesign signing →
  HFS+ image (`mkfs.hfsplus`) populated with the `hfsplus` tool → UDZO
  UDIF wrapped by libdmg-hfsplus.

## Install notes (per platform)

### Android
- Settings → allow installing from your browser/files app → install APK.
- If an older build was installed with a different signature, uninstall it
  first ("App not installed" usually means a signature conflict).
- Notification permission is requested on first connect (Android 13+).

### Windows
- SmartScreen: **More info → Run anyway** (binaries are not EV-signed).
- Tunnel creation needs admin; the installer offers a Windows-service mode
  (`install-service.ps1`) so daily use needs no elevation prompts.
- WebView2 runtime is preinstalled on Win10 21H2+/Win11; otherwise grab the
  Evergreen runtime from Microsoft.

### macOS
**Recommended — one-line install (zero Gatekeeper dialogs):**

```bash
curl -fsSL <download-page-origin>/downloads/install-macos.sh | bash -s -- "<download-page-origin>"
```

The script downloads `AegisVPN-macos.zip` (the signed universal app),
verifies SHA-256, installs to `/Applications` and launches it. `curl`
downloads carry no `com.apple.quarantine` attribute — the mechanism
Tailscale/Homebrew rely on — so the app opens with no Gatekeeper prompt
at all. Only the normal macOS admin password appears (once; the utun
device needs root).

**Manual DMG install (v1.2.1):**
- Download **`AegisVPN-1.2.1.dmg`** (not an old cached `AegisVPN.dmg`) and
  double-click it, drag `AegisVPN.app` to **Applications**; click **Done**
  if the *""AegisVPN" Not Opened — Apple could not verify…"* dialog
  appears. (~14.8 MB is the correct size — see the size note above.)
- One-time Gatekeeper pass (the app is ad-hoc signed, not notarized —
  the dialog is expected and does **not** mean a broken download):
  - **fastest**: in Terminal run
    `xattr -dr com.apple.quarantine /Applications/AegisVPN.app`;
  - macOS 15+: System Settings → Privacy & Security → scroll to
    **Open Anyway** next to AegisVPN → Open Anyway → Open;
  - macOS 13/14: right-click the app → **Open** → **Open**.
- Launch AegisVPN — approve the admin prompt (once). The dashboard opens
  at `http://127.0.0.1:8080` (localhost only).
- If the engine ever fails to start, the app now shows a visible error
  dialog (previously it failed silently); logs at
  `/var/root/Library/Logs/AegisVPN-server.log`.
- Still blocked after the xattr step? Run the engine binary directly —
  `/Applications/AegisVPN.app/Contents/MacOS/AegisVPN` — and read the
  output; a healthy start ends with
  `aegisvpn dashboard on http://127.0.0.1:8080`.

## Reproduce

```bash
# macOS UDIF DMG — full pipeline on Linux (frontend + universal binary +
# ad-hoc signing + HFS+ + UDIF wrap):
./scripts/build-macos-dmg-linux.sh

# engine CLI binaries (pure Go, CGO off)
cd engine && for t in darwin/amd64 darwin/arm64 windows/amd64; do \
  GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w" -o dist/aegisvpn-${t%/*}-${t#*/} ./cmd/aegisvpn; done

# Android APK (no-Gradle pipeline, bootstraps its own toolchain)
cd android-java && ./build-apk.sh

# Windows GUI + installer (NSIS)
cd gui/desktop/frontend && npm install && npm run build && cd ..
goversioninfo -platform-specific versioninfo.json
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  go build -tags desktop,production -ldflags "-s -w -H windowsgui" -o ../../dist/win-package/AegisVPN.exe .
cd windows/installer && makensis aegisvpn.nsi

# checksums
cd dist && sha256sum AegisVPN-* > SHA256SUMS.txt
```
