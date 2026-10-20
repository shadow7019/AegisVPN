#!/usr/bin/env bash
#
# AegisVPN privileged daemon installer (macOS).
#
# Installs the wireguard-go engine binary ("aegisvpn") as a root launchd
# daemon that owns the real utun interface:
#
#   1. copies the engine binary -> /Library/PrivilegedHelperTools/net.aegisvpn.daemon
#   2. copies the launchd plist -> /Library/LaunchDaemons/net.aegisvpn.daemon.plist
#   3. bootstraps the service   -> launchctl bootstrap system <plist>
#   4. waits for the control socket (default /var/run/aegisvpn.sock)
#
# Usage:
#   sudo ./install-daemon.sh [path/to/AegisVPN.app]
#
# The AegisVPN app runs this script via
#   osascript -e 'do shell script "bash <script> <bundle>" with administrator privileges'
# so it executes as root; it can also be run manually with sudo.
#
# Environment overrides:
#   AEGISVPN_BIN  path to a standalone engine binary (skips app-bundle lookup)

set -euo pipefail

LABEL="net.aegisvpn.daemon"
HELPER_DIR="/Library/PrivilegedHelperTools"
HELPER_PATH="$HELPER_DIR/$LABEL"
PLIST_DST="/Library/LaunchDaemons/$LABEL.plist"
SOCKET_PATH="/var/run/aegisvpn.sock"
APP_ROOT_DIR="/var/run/aegisvpn"
LOG_OUT="/Library/Logs/aegisvpn-daemon.log"
LOG_ERR="/Library/Logs/aegisvpn-daemon.err.log"

log()  { echo "[aegisvpn-install] $*"; }
fail() { echo "[aegisvpn-install] ERROR: $*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || fail "must run as root (sudo, or the app's admin prompt)"

APP_BUNDLE="${1:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --- locate the engine binary -------------------------------------------------
BIN_SRC=""
if [[ -n "${AEGISVPN_BIN:-}" && -f "${AEGISVPN_BIN:-}" ]]; then
  BIN_SRC="$AEGISVPN_BIN"
elif [[ -n "$APP_BUNDLE" && -f "$APP_BUNDLE/Contents/Resources/aegisvpn" ]]; then
  BIN_SRC="$APP_BUNDLE/Contents/Resources/aegisvpn"
elif [[ -f "$SCRIPT_DIR/aegisvpn" ]]; then
  BIN_SRC="$SCRIPT_DIR/aegisvpn"
else
  fail "engine binary not found (pass the AegisVPN.app bundle path, or set AEGISVPN_BIN)"
fi

# --- locate the launchd plist --------------------------------------------------
PLIST_SRC=""
if [[ -n "$APP_BUNDLE" && -f "$APP_BUNDLE/Contents/Resources/net.aegisvpn.daemon.plist" ]]; then
  PLIST_SRC="$APP_BUNDLE/Contents/Resources/net.aegisvpn.daemon.plist"
elif [[ -f "$SCRIPT_DIR/net.aegisvpn.daemon.plist" ]]; then
  PLIST_SRC="$SCRIPT_DIR/net.aegisvpn.daemon.plist"
else
  fail "net.aegisvpn.daemon.plist not found"
fi

log "engine binary : $BIN_SRC"
log "launchd plist : $PLIST_SRC"

# --- install the helper binary --------------------------------------------------
mkdir -p "$HELPER_DIR"
install -m 0755 -o root -g wheel "$BIN_SRC" "$HELPER_PATH"

# go build produces an ad-hoc signed binary on arm64; re-sign defensively so
# launchd always accepts the image.
if [[ -x /usr/bin/codesign ]]; then
  /usr/bin/codesign --force --sign - "$HELPER_PATH" \
    || log "warning: ad-hoc re-sign failed (continuing)"
fi

# --- install the launchd plist --------------------------------------------------
install -m 0644 -o root -g wheel "$PLIST_SRC" "$PLIST_DST"
/usr/bin/plutil -lint "$PLIST_DST" >/dev/null || fail "installed plist failed plutil -lint"

# --- socket / app-root directories ----------------------------------------------
# /var/run is cleaned on reboot; the daemon (RunAtLoad) recreates its socket at
# boot. APP_ROOT_DIR is where the engine stores state when using the
# $appRoot/engine.sock convention from PROTOCOL.md section 2.
mkdir -p "$APP_ROOT_DIR"
chown root:wheel "$APP_ROOT_DIR"
chmod 0755 "$APP_ROOT_DIR"

# --- (re)start the service -------------------------------------------------------
if launchctl print "system/$LABEL" >/dev/null 2>&1; then
  log "existing service found — booting out before reinstall"
  launchctl bootout "system/$LABEL" || true
fi

launchctl bootstrap system "$PLIST_DST" || fail "launchctl bootstrap failed"
launchctl kickstart -k "system/$LABEL" || true

# --- wait for the control socket --------------------------------------------------
log "waiting for control socket at $SOCKET_PATH ..."
SOCKET_UP=0
for _ in $(seq 1 20); do
  if [[ -S "$SOCKET_PATH" ]]; then
    SOCKET_UP=1
    log "socket is up"
    break
  fi
  sleep 0.5
done

if [[ $SOCKET_UP -eq 0 ]]; then
  log "warning: socket not present yet — check $LOG_ERR"
  log "note: the engine may serve at $APP_ROOT_DIR/engine.sock (app-root convention); the GUI probes both paths"
fi

log "installed helper : $HELPER_PATH"
log "installed plist  : $PLIST_DST"
log "logs             : $LOG_OUT / $LOG_ERR"
log "done"
