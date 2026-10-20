#!/usr/bin/env bash
#
# AegisVPN privileged daemon uninstaller (macOS).
#
# Stops and removes the launchd service, helper binary, control socket and
# app-root directory. User settings (UserDefaults of the GUI) are untouched.
#
# Usage: sudo ./uninstall-daemon.sh

set -euo pipefail

LABEL="net.aegisvpn.daemon"
HELPER_PATH="/Library/PrivilegedHelperTools/$LABEL"
PLIST_DST="/Library/LaunchDaemons/$LABEL.plist"
SOCKET_PATH="/var/run/aegisvpn.sock"
APP_ROOT_DIR="/var/run/aegisvpn"

log()  { echo "[aegisvpn-uninstall] $*"; }

[[ $EUID -eq 0 ]] || { echo "[aegisvpn-uninstall] ERROR: must run as root" >&2; exit 1; }

if launchctl print "system/$LABEL" >/dev/null 2>&1; then
  log "booting out system service $LABEL"
  launchctl bootout "system/$LABEL" || true
fi

rm -f "$PLIST_DST" "$HELPER_PATH" "$SOCKET_PATH"
rmdir "$APP_ROOT_DIR" 2>/dev/null || true

log "daemon removed (logs kept in /Library/Logs/aegisvpn-daemon*.log)"
log "done"
