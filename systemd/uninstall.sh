#!/usr/bin/env bash
# Uninstall the AegisVPN systemd service.
#   sudo ./uninstall.sh [--purge]   # --purge also removes /var/lib/aegisvpn
set -euo pipefail

PURGE=""
[[ "${1:-}" == "--purge" ]] && PURGE=1
[[ $EUID -eq 0 ]] || { echo "must run as root (sudo)" >&2; exit 1; }

log()  { printf '\033[33m[uninstall]\033[0m %s\n' "$*"; }

if systemctl list-unit-files 2>/dev/null | grep -q '^aegisvpn.service'; then
  systemctl disable --now aegisvpn.service 2>/dev/null || true
  log "service stopped and disabled"
fi
rm -f /etc/systemd/system/aegisvpn.service
systemctl daemon-reload
log "unit removed"

if [[ -n "${PURGE}" ]]; then
  rm -rf /var/lib/aegisvpn
  log "purged /var/lib/aegisvpn (keys, profiles, socket)"
else
  log "kept /var/lib/aegisvpn (pass --purge to remove keys/profiles)"
fi

if [[ -x /usr/local/bin/aegisvpn ]]; then
  rm -f /usr/local/bin/aegisvpn
  log "binary removed: /usr/local/bin/aegisvpn"
fi

log "done — tunnel, kill-switch rules and DNS were reverted on service stop"
