#!/usr/bin/env bash
# Install AegisVPN as a systemd service (Linux).
#
#   sudo ./install.sh [--binary /path/to/aegisvpn]
#
# Steps: install binary → render unit → daemon-reload → enable + start.
# Idempotent: safe to re-run (upgrades binary in place, restarts service).
set -euo pipefail

BIN_SRC=""
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary) BIN_SRC="${2:?}"; shift 2 ;;
    -h|--help) grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

[[ $EUID -eq 0 ]] || { echo "must run as root (sudo)" >&2; exit 1; }

log()  { printf '\033[32m[install]\033[0m %s\n' "$*"; }
fail() { printf '\033[31m[install]\033[0m %s\n' "$*" >&2; exit 1; }

command -v systemctl >/dev/null || fail "systemd not found (not a systemd host?)"

# 1. binary -----------------------------------------------------------------
if [[ -z "${BIN_SRC}" ]]; then
  for cand in "${SCRIPT_DIR}/../dist/aegisvpn-linux-amd64" \
              "${SCRIPT_DIR}/../engine/aegisvpn"; do
    if [[ -x "${cand}" ]]; then BIN_SRC="${cand}"; break; fi
  done
fi
if [[ -z "${BIN_SRC}" && -x /usr/local/bin/aegisvpn ]]; then
  BIN_SRC="/usr/local/bin/aegisvpn"
fi
[[ -n "${BIN_SRC}" && -x "${BIN_SRC}" ]] || fail "no aegisvpn binary found — build one: (cd engine && go build -o ../dist/aegisvpn-linux-amd64 ./cmd/aegisvpn) or pass --binary"

install -m 0755 "${BIN_SRC}" /usr/local/bin/aegisvpn
log "binary installed: /usr/local/bin/aegisvpn ($(/usr/local/bin/aegisvpn version 2>/dev/null || echo '?'))"

# 2. unit -------------------------------------------------------------------
install -m 0644 "${SCRIPT_DIR}/aegisvpn.service" /etc/systemd/system/aegisvpn.service
mkdir -p /var/lib/aegisvpn && chmod 700 /var/lib/aegisvpn
log "unit installed: /etc/systemd/system/aegisvpn.service"

# 3. enable + start ----------------------------------------------------------
systemctl daemon-reload
systemctl enable aegisvpn.service >/dev/null
if systemctl is-active --quiet aegisvpn.service; then
  systemctl restart aegisvpn.service     # upgrade path
  log "service restarted"
else
  systemctl start aegisvpn.service
  log "service started"
fi

SOCK="/var/lib/aegisvpn/engine.sock"
for _ in $(seq 1 20); do
  [[ -S "${SOCK}" ]] && break
  sleep 0.25
done

cat <<EOF

AegisVPN service is up.
  unit        : systemctl status aegisvpn
  logs        : journalctl -u aegisvpn -f
  control     : aegisvpn status   (talks via ${SOCK})
  socket      : ${SOCK}$([[ -S "${SOCK}" ]] && echo ' (live)' || echo ' (still starting)')

Disconnect/stop semantics: systemctl stop aegisvpn tears the tunnel down
and removes kill-switch rules cleanly (engine handles SIGTERM).
EOF
