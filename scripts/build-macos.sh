#!/usr/bin/env bash
#
# AegisVPN macOS build (Task 2-b).
#
# Produces dist/AegisVPN.dmg containing AegisVPN.app (SwiftUI GUI) with the
# wireguard-go engine embedded as the privileged daemon payload.
#
# Pipeline:
#   1. build the Go engine (real wireguard-go, darwin/arm64 and/or amd64)
#   2. generate AegisVPN.xcodeproj with xcodegen (macos/project.yml)
#   3. xcodebuild Release build of the app
#   4. embed engine binary + daemon install scripts/plist into the app bundle
#   5. ad-hoc codesign (replace with Developer ID + notarization for release)
#   6. stage app + /Applications symlink, hdiutil create -format UDZO
#
# Requires (run on macOS 13+): go, xcodegen (brew install xcodegen),
# Xcode (xcodebuild), codesign, hdiutil.
#
# Options:
#   --universal    build a universal (arm64 + amd64) engine via lipo
#   --skip-engine  reuse the engine binary from a previous run
#   --clean        wipe macos/build and dist/ first

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENGINE_DIR="$ROOT/engine"
MACOS_DIR="$ROOT/macos"
DAEMON_DIR="$MACOS_DIR/daemon"
BUILD_DIR="$MACOS_DIR/build"
DIST_DIR="$ROOT/dist"
STAGING="$DIST_DIR/staging"

APP_NAME="AegisVPN"
HOST_ARCH="$(uname -m)"

usage() {
  cat <<EOF
Usage: $0 [options]
  --universal     Build a universal (arm64 + amd64) engine binary via lipo.
  --skip-engine   Reuse the engine binary from a previous run.
  --clean         Remove macos/build and dist/ before building.
  -h, --help      Show this help.
EOF
}

UNIVERSAL=0
SKIP_ENGINE=0
CLEAN=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --universal)  UNIVERSAL=1;  shift ;;
    --skip-engine) SKIP_ENGINE=1; shift ;;
    --clean)      CLEAN=1;      shift ;;
    -h|--help)    usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
done

case "$HOST_ARCH" in
  arm64)  HOST_GOARCH="arm64" ;;
  x86_64) HOST_GOARCH="amd64" ;;
  *) echo "unsupported host arch: $HOST_ARCH" >&2; exit 1 ;;
esac

log()  { printf '\033[36m[build-macos]\033[0m %s\n' "$*"; }
fail() { printf '\033[31m[build-macos] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

for tool in go xcodegen xcodebuild codesign hdiutil; do
  command -v "$tool" >/dev/null 2>&1 \
    || fail "'$tool' not found in PATH (brew install xcodegen; install Xcode + Go)"
done

if (( CLEAN )); then
  log "cleaning $BUILD_DIR and $DIST_DIR"
  rm -rf "$BUILD_DIR" "$DIST_DIR"
fi

mkdir -p "$BUILD_DIR/engine" "$DIST_DIR"

# ---------------------------------------------------------------------------
# 1. Engine (wireguard-go based, module github.com/shadow7019/AegisVPN/engine)
# ---------------------------------------------------------------------------
ENGINE_BIN="$BUILD_DIR/engine/aegisvpn"

build_engine() {
  local goarch="$1" outfile="$2"
  local pkg="./cmd/aegisvpn"
  [[ -d "$ENGINE_DIR/cmd/aegisvpn" ]] || pkg="./"
  log "building engine darwin/$goarch -> $outfile"
  (cd "$ENGINE_DIR" && CGO_ENABLED=0 GOOS=darwin GOARCH="$goarch" \
      go build -trimpath -ldflags "-s -w" -o "$outfile" "$pkg")
}

if (( SKIP_ENGINE )) && [[ -x "$ENGINE_BIN" ]]; then
  log "reusing existing engine binary: $ENGINE_BIN"
elif (( UNIVERSAL )); then
  build_engine arm64 "$BUILD_DIR/engine/aegisvpn-darwin-arm64"
  build_engine amd64 "$BUILD_DIR/engine/aegisvpn-darwin-amd64"
  log "lipo: merging arm64 + amd64 into a universal engine"
  lipo -create \
    "$BUILD_DIR/engine/aegisvpn-darwin-arm64" \
    "$BUILD_DIR/engine/aegisvpn-darwin-amd64" \
    -output "$ENGINE_BIN"
  lipo -info "$ENGINE_BIN"
else
  build_engine "$HOST_GOARCH" "$ENGINE_BIN"
fi

# ---------------------------------------------------------------------------
# 2. Xcode project (XcodeGen)
# ---------------------------------------------------------------------------
log "generating Xcode project (xcodegen generate)"
(cd "$MACOS_DIR" && xcodegen generate)

# ---------------------------------------------------------------------------
# 3. App build (Release)
# ---------------------------------------------------------------------------
log "building app (Release configuration)"
xcodebuild \
  -project "$MACOS_DIR/AegisVPN.xcodeproj" \
  -scheme AegisVPN \
  -configuration Release \
  -destination "generic/platform=macOS" \
  -derivedDataPath "$BUILD_DIR/DerivedData" \
  -quiet \
  build

APP_PATH="$BUILD_DIR/DerivedData/Build/Products/Release/$APP_NAME.app"
[[ -d "$APP_PATH" ]] || fail "app bundle was not produced at $APP_PATH"

# ---------------------------------------------------------------------------
# 4. Embed the privileged-daemon payload into the app bundle.
#    The GUI's onboarding runs Resources/install-daemon.sh via an admin
#    prompt; the script copies Resources/aegisvpn + Resources/*.plist into
#    /Library/PrivilegedHelperTools and /Library/LaunchDaemons.
# ---------------------------------------------------------------------------
RES="$APP_PATH/Contents/Resources"
cp "$ENGINE_BIN" "$RES/aegisvpn"
chmod 0755 "$RES/aegisvpn"
cp "$DAEMON_DIR/install-daemon.sh" \
   "$DAEMON_DIR/uninstall-daemon.sh" \
   "$DAEMON_DIR/net.aegisvpn.daemon.plist" \
   "$RES/"
chmod 0755 "$RES/install-daemon.sh" "$RES/uninstall-daemon.sh"
chmod 0644 "$RES/net.aegisvpn.daemon.plist"

# ---------------------------------------------------------------------------
# 5. Codesign — ad-hoc for local use/testing.
#    For distribution: sign with your "Developer ID Application" identity,
#    enable hardened runtime (already on), then notarize (macos/README.md).
#    The nested engine binary is signed first so the outer seal is valid.
# ---------------------------------------------------------------------------
log "codesigning (ad-hoc)"
codesign --force --sign - "$RES/aegisvpn" \
  || log "warning: could not re-sign the embedded engine binary"
codesign --force --deep --sign - "$APP_PATH"
codesign --verify --deep --strict "$APP_PATH" || fail "codesign verification failed"

# ---------------------------------------------------------------------------
# 6. DMG (staging folder with the app + /Applications symlink -> UDZO)
# ---------------------------------------------------------------------------
log "staging DMG contents"
rm -rf "$STAGING"
mkdir -p "$STAGING"
cp -R "$APP_PATH" "$STAGING/$APP_NAME.app"
ln -s /Applications "$STAGING/Applications"

DMG="$DIST_DIR/AegisVPN.dmg"
log "creating $DMG"
hdiutil create -volname "AegisVPN" \
  -srcfolder "$STAGING" \
  -ov -format UDZO \
  "$DMG"

rm -rf "$STAGING"

echo
log "engine binary : $ENGINE_BIN ($(file -b "$ENGINE_BIN" | cut -d, -f1-2))"
log "app bundle    : $APP_PATH"
log "DMG artifact  : $DMG"
echo "ARTIFACT: $DMG"
