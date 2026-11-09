#!/usr/bin/env bash
#
# AegisVPN macOS DMG builder — runs entirely on Linux (no Xcode needed).
#
# Produces dist/AegisVPN-<version>.dmg containing AegisVPN.app:
#   Contents/MacOS/AegisVPN        universal (x86_64 + arm64) aegisvpn-server:
#                                  browser dashboard + real wireguard-go engine,
#                                  double-click flow (admin elevation + open UI)
#   Contents/MacOS/cli/            engine CLI binaries (connect/status/…)
#   Contents/Resources/            logo.icns, bundled fleet, README
#
# Pipeline:
#   1. npm/bun build of gui/desktop/frontend (bridge.js + assets)
#   2. copy dist into gui/desktop/webapp/ui (go:embed)
#   3. go build darwin/amd64 + darwin/arm64 of cmd/aegisvpn-server (CGO off)
#   4. scripts/macho-merge.py -> universal Contents/MacOS/AegisVPN
#   5. rcodesign ad-hoc signing of the whole bundle (Gatekeeper-safe shape)
#   6. mkfs.hfsplus + hfsplus populate + libdmg-hfsplus `dmg build` -> UDZO DMG
#
# Tool expectations (override via env):
#   GO_BIN      (default: go on PATH)
#   HFSPLUS     hfsplus tool built from libdmg-hfsplus
#   HDUTIL_DMG  libdmg-hfsplus `dmg` tool
#   MKFS_HFSPLUS hfsprogs mkfs.hfsplus
#   RCODESIGN   apple-codesign rcodesign binary
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist"
WORK="${TMPDIR:-/tmp}/aegisvpn-macos-build"

GO_BIN="${GO_BIN:-go}"
HFSPLUS="${HFSPLUS:-$HOME/tools/libdmg-hfsplus/build/hfs/hfsplus}"
DMG_TOOL="${DMG_TOOL:-$HOME/tools/libdmg-hfsplus/build/dmg/dmg}"
MKFS_HFSPLUS="${MKFS_HFSPLUS:-$HOME/tools/hfs-root/sbin/mkfs.hfsplus}"
RCODESIGN="${RCODESIGN:-$HOME/tools/rcodesign/rcodesign}"

APP_VERSION="1.3.0"
IMAGE_MB=96

log()  { printf '\033[1;36m[macos-dmg]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[macos-dmg] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

command -v "$GO_BIN" >/dev/null 2>&1 || fail "go toolchain missing (set GO_BIN)"
for t in "$HFSPLUS" "$DMG_TOOL" "$MKFS_HFSPLUS" "$RCODESIGN"; do
  [ -x "$t" ] || fail "tool missing or not executable: $t"
done

rm -rf "$WORK"
mkdir -p "$WORK" "$DIST"

# ---------------------------------------------------------------------------
# 1. Engine server binaries (both architectures)
log "building aegisvpn-server darwin/amd64 + arm64"
( cd "$ROOT/gui/desktop" &&
  CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 "$GO_BIN" build -trimpath \
      -ldflags "-s -w" -o "$WORK/darwin-amd64" ./cmd/aegisvpn-server &&
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 "$GO_BIN" build -trimpath \
      -ldflags "-s -w" -o "$WORK/darwin-arm64" ./cmd/aegisvpn-server )
log "building aegisvpn CLI darwin/amd64 + arm64"
( cd "$ROOT/engine" &&
  CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 "$GO_BIN" build -trimpath \
      -ldflags "-s -w" -o "$WORK/cli-amd64" ./cmd/aegisvpn &&
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 "$GO_BIN" build -trimpath \
      -ldflags "-s -w" -o "$WORK/cli-arm64" ./cmd/aegisvpn )

# ---------------------------------------------------------------------------
# 4. Universal main executable + CLI copies
log "merging universal binary (macho-merge)"
python3 "$ROOT/scripts/macho-merge.py" "$WORK/AegisVPN" \
    "$WORK/darwin-amd64" "$WORK/darwin-arm64"

# universal CLI for terminal users (one binary, runs on both arches)
log "merging universal CLI (macho-merge)"
python3 "$ROOT/scripts/macho-merge.py" "$WORK/aegisvpn-cli" "$WORK/cli-amd64" "$WORK/cli-arm64"

# ---------------------------------------------------------------------------
# 5. Assemble AegisVPN.app
APP="$WORK/AegisVPN.app"
log "assembling $APP"
mkdir -p "$APP/Contents/MacOS/cli" "$APP/Contents/Resources"
chmod 755 "$WORK/AegisVPN" "$WORK/aegisvpn-cli"
cp "$WORK/AegisVPN" "$APP/Contents/MacOS/AegisVPN"
cp "$WORK/aegisvpn-cli" "$APP/Contents/MacOS/cli/aegisvpn"
cp "$ROOT/server/coord/nodes.json" "$APP/Contents/Resources/nodes.json"
cp "$ROOT/assets/macos/logo.icns" "$APP/Contents/Resources/logo.icns" 2>/dev/null \
  || cp "$HOME/tools/logo.icns" "$APP/Contents/Resources/logo.icns"

cat > "$APP/Contents/PkgInfo" <<'EOF'
APPL????
EOF

cat > "$APP/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>AegisVPN</string>
  <key>CFBundleDisplayName</key><string>AegisVPN</string>
  <key>CFBundleIdentifier</key><string>net.aegisvpn.desktop</string>
  <key>CFBundleVersion</key><string>$APP_VERSION</string>
  <key>CFBundleShortVersionString</key><string>$APP_VERSION</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleExecutable</key><string>AegisVPN</string>
  <key>CFBundleIconFile</key><string>logo</string>
  <key>CFBundleDevelopmentRegion</key><string>en</string>
  <key>LSMinimumSystemVersion</key><string>12.0</string>
  <key>LSApplicationCategoryType</key><string>public.app-category.utilities</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSHumanReadableCopyright</key><string>Copyright © 2026 AegisVPN. WireGuard is a registered trademark of Jason A. Donenfeld.</string>
</dict>
</plist>
EOF

cat > "$APP/Contents/Resources/README-FIRST.txt" <<'EOF'
AegisVPN for macOS — real WireGuard-protocol VPN
================================================

INSTALL
  1. Drag "AegisVPN.app" onto "Applications" in the Finder sidebar.
     If the ""AegisVPN" Not Opened — Apple could not verify ..." dialog
     appears (one Done button), click Done and go to step 2.
  2. FIRST LAUNCH PASS (30 seconds, one time only) — that dialog is the
     standard Gatekeeper screen for apps without Apple notarization; the
     code signature itself is valid. Any ONE of these clears it:
       • Terminal (Spotlight -> "Terminal"), paste + Return:
           xattr -dr com.apple.quarantine /Applications/AegisVPN.app
       • System Settings -> Privacy & Security -> scroll to
         "Open Anyway" next to AegisVPN -> Open Anyway -> Open.
       • macOS 13/14: right-click the app -> Open -> Open.
     Gatekeeper never asks again after this.
  3. Launch AegisVPN — it asks for administrator rights ONCE (the VPN
     tunnel needs a utun device) and opens the dashboard in your browser
     at http://127.0.0.1:8080

FEATURES
  * FREE PUBLIC RELAYS — one-click connect from the dashboard "Public
    relays" tab (VPNGate network, live reachability checks). The engine
    fetches the profile and connects automatically. CLI:
    Contents/MacOS/cli/aegisvpn relays connect --country JP
  * 80 WireGuard nodes in 56 countries (bundled registry, live updates)
  * Kill Switch, Threat Protection (DNS filtering), Obfuscation
  * Double VPN (two-hop chains), P2P / Onion / dedicated node types
  * Split tunneling, Meshnet, Dark Web Monitor, auto-connect
  * Dashboard + API bind to 127.0.0.1 only — nothing leaves your machine

WHY IS THE APP ONLY ~15 MB?
  AegisVPN is a native Go engine — the same wireguard-go core behind
  NordLynx — with the dashboard and the 80-node fleet registry built in.
  It does NOT bundle a browser runtime: Electron-based apps ship a full
  Chromium copy, which is why those installers are 150-200 MB. Small is
  correct here: unpacked, AegisVPN.app is ~33 MB.

IF THE APP STILL WILL NOT OPEN
  Run the engine directly from Terminal so failures print real output:
      /Applications/AegisVPN.app/Contents/MacOS/AegisVPN
  A healthy start ends with:
      aegisvpn dashboard on http://127.0.0.1:8080
  and your browser opens the dashboard automatically.

CLI
  /Applications/AegisVPN.app/Contents/MacOS/cli/aegisvpn
  commands: connect | disconnect | status | nodes | daemon | relays | version

Logs:  /var/root/Library/Logs/AegisVPN-server.log (elevated run)
       ~/Library/Logs/                            (fallback run)
EOF

# ---------------------------------------------------------------------------
# 6. Ad-hoc code signing (no "damaged" Gatekeeper state on Apple Silicon)
log "ad-hoc code signing bundle (rcodesign)"
"$RCODESIGN" sign "$APP"

# The bundle seal MUST exist — macOS rejects bundles whose main executable
# carries an embedded signature while _CodeSignature/CodeResources is absent
# (exact "damaged and can't be opened" Gatekeeper state).
CODE_SEAL="$APP/Contents/_CodeSignature/CodeResources"
[ -f "$CODE_SEAL" ] || fail "rcodesign did not produce _CodeSignature/CodeResources"
log "bundle seal present: _CodeSignature/CodeResources ($(wc -c <"$CODE_SEAL") bytes)"

# ---------------------------------------------------------------------------
# 7. HFS+ image -> UDIF (UDZO) DMG
IMG="$WORK/aegisvpn.hfs"
# Versioned output name: every rebuild changes the URL, so a browser can
# never serve a stale cached copy of a previously broken build (the v1.2.0
# downloads shipped a defective bundle seal; the fix must reach users as
# fresh bytes, not as a cached "AegisVPN.dmg").
DMG_OUT="$DIST/AegisVPN-$APP_VERSION.dmg"
DMG_ALIAS="$DIST/AegisVPN.dmg"
log "creating HFS+ volume ($IMAGE_MB MB)"
dd if=/dev/zero of="$IMG" bs=1M count="$IMAGE_MB" 2>/dev/null
"$MKFS_HFSPLUS" -v "AegisVPN" "$IMG"

log "populating HFS+ volume"
hsymlink() { "$HFSPLUS" "$IMG" symlink "$1" "$2"; }
hmkdir() { "$HFSPLUS" "$IMG" mkdir "$1"; }
hadd()   { "$HFSPLUS" "$IMG" add "$1" "$2"; }
hchmod() { "$HFSPLUS" "$IMG" chmod "$1" "$2"; }

hsymlink /Applications /Applications
hmkdir /AegisVPN.app
hsymlink /Applications /Applications
hmkdir /AegisVPN.app/Contents
hsymlink /Applications /Applications
hmkdir /AegisVPN.app/Contents/MacOS
hsymlink /Applications /Applications
hmkdir /AegisVPN.app/Contents/MacOS/cli
hsymlink /Applications /Applications
hmkdir /AegisVPN.app/Contents/Resources
hsymlink /Applications /Applications
hmkdir /AegisVPN.app/Contents/_CodeSignature

hadd "$APP/Contents/Info.plist"        /AegisVPN.app/Contents/Info.plist
hadd "$APP/Contents/PkgInfo"           /AegisVPN.app/Contents/PkgInfo
hadd "$APP/Contents/_CodeSignature/CodeResources" /AegisVPN.app/Contents/_CodeSignature/CodeResources
hadd "$APP/Contents/MacOS/AegisVPN"    /AegisVPN.app/Contents/MacOS/AegisVPN
hadd "$APP/Contents/MacOS/cli/aegisvpn" /AegisVPN.app/Contents/MacOS/cli/aegisvpn
hadd "$APP/Contents/Resources/logo.icns"  /AegisVPN.app/Contents/Resources/logo.icns
hadd "$APP/Contents/Resources/nodes.json" /AegisVPN.app/Contents/Resources/nodes.json
hadd "$APP/Contents/Resources/README-FIRST.txt" /AegisVPN.app/Contents/Resources/README-FIRST.txt

hchmod 755 /AegisVPN.app/Contents/MacOS/AegisVPN
hchmod 755 /AegisVPN.app/Contents/MacOS/cli/aegisvpn

log "wrapping UDIF (UDZO) DMG"
"$DMG_TOOL" build "$IMG" "$DMG_OUT"
cp -f "$DMG_OUT" "$DMG_ALIAS"
log "published: $(basename "$DMG_OUT") + alias $(basename "$DMG_ALIAS")"

# ---------------------------------------------------------------------------
# 8. Round-trip verification: DMG -> HFS+ -> bytes must be identical and all
#    signatures intact. This catches UDZO corruption, missing bundle seals,
#    lost exec bits — everything that shows up on a Mac as "damaged".
log "round-trip verification (dmg extract -> per-file byte compare -> signature check)"
VDIR="$WORK/verify"
rm -rf "$VDIR" && mkdir -p "$VDIR"
"$DMG_TOOL" extract "$DMG_OUT" "$VDIR/verify.hfs" >/dev/null

VAPP="$VDIR/AegisVPN.app"
hvextract() { "$HFSPLUS" "$VDIR/verify.hfs" extract "$1" "$2"; }

mkdir -p "$VAPP/Contents/MacOS/cli" "$VAPP/Contents/Resources" "$VAPP/Contents/_CodeSignature"

test_bundle_file() { # hfs_path  local_original
  local hfs="$1" orig="$2" out="$VDIR/out.bin"
  hvextract "$hfs" "$out" >/dev/null || fail "extract failed from volume: $hfs"
  cmp -s "$orig" "$out" || fail "BYTE MISMATCH inside DMG: $hfs"
}

test_bundle_file /AegisVPN.app/Contents/Info.plist                       "$APP/Contents/Info.plist"
test_bundle_file /AegisVPN.app/Contents/PkgInfo                          "$APP/Contents/PkgInfo"
test_bundle_file /AegisVPN.app/Contents/_CodeSignature/CodeResources     "$APP/Contents/_CodeSignature/CodeResources"
test_bundle_file /AegisVPN.app/Contents/MacOS/AegisVPN                   "$APP/Contents/MacOS/AegisVPN"
test_bundle_file /AegisVPN.app/Contents/MacOS/cli/aegisvpn               "$APP/Contents/MacOS/cli/aegisvpn"
test_bundle_file /AegisVPN.app/Contents/Resources/logo.icns              "$APP/Contents/Resources/logo.icns"
test_bundle_file /AegisVPN.app/Contents/Resources/nodes.json             "$APP/Contents/Resources/nodes.json"
test_bundle_file /AegisVPN.app/Contents/Resources/README-FIRST.txt       "$APP/Contents/Resources/README-FIRST.txt"

# Exec bit must survive the HFS+ round trip (mode 100755).
MODES=$("$HFSPLUS" "$VDIR/verify.hfs" ls /AegisVPN.app/Contents/MacOS)
echo "$MODES" | grep -q "100755 .* AegisVPN" || fail "main binary lost +x inside DMG"

# Every Mach-O must carry an ADHOC signature and the bundle identifier.
for BIN in AegisVPN cli/aegisvpn; do
  "$RCODESIGN" print-signature-info "$APP/Contents/MacOS/$BIN" | grep -q "ADHOC" \
    || fail "signature not ADHOC after packaging: $BIN"
done

"$HFSPLUS" "$VDIR/verify.hfs" ls / | grep -q "Applications" || fail "Applications symlink missing in DMG"

log "round-trip verification PASSED — 9 files byte-identical, +x intact, ADHOC signatures intact"

# ---------------------------------------------------------------------------
# 9. Native zip of the signed bundle — consumed by scripts/install-macos.sh.
#    curl-fetched archives carry no com.apple.quarantine, so apps installed
#    via the one-line installer open with ZERO Gatekeeper dialogs.
log "packaging macOS zip (for the one-line installer)"
ZIP_OUT="$DIST/AegisVPN-macos.zip"
rm -f "$ZIP_OUT"
( cd "$WORK" && zip -qry "$ZIP_OUT" AegisVPN.app )

# Zip sanity: integrity + the main binary must keep its exec bit.
unzip -tqq "$ZIP_OUT" >/dev/null || fail "zip integrity check failed"
ZMODE=$(unzip -Z "$ZIP_OUT" "AegisVPN.app/Contents/MacOS/AegisVPN" | awk '/^-rwx/{print "ok"; exit}')
[ "$ZMODE" = "ok" ] || fail "zip lost the +x bit on the main binary"
log "zip verification PASSED → $ZIP_OUT"

log "done → $DMG_OUT (alias $DMG_ALIAS)"
ls -la "$DMG_OUT" "$DMG_ALIAS" "$ZIP_OUT"
