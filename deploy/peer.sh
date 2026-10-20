#!/usr/bin/env bash
# deploy/peer.sh — client peer lifecycle for AegisVPN nodes.
#
#   peer.sh add    [--host user@ip|--local] [--conf entry|exit] [--ip 10.8.0.X] \
#                  [--obfs-key k] [--dns "1.1.1.1,9.9.9.9"] <node-name> <client-name>
#   peer.sh list   [--host user@ip|--local] [<node-name>]
#   peer.sh show   [--host user@ip|--local] <node-name> <client-name>
#   peer.sh revoke [--host user@ip|--local] <node-name> <client-name>
#
# add: generates a REAL client keypair + preshared key, allocates the next
#      free tunnel IP, appends the [Peer] to the node's wg0.conf, applies it
#      LIVE into the running container (wg set) and writes a ready-to-use
#      client profile under deploy/peers/<node>/<client>.conf (chmod 600).
#      The server's PRIVATE key never leaves the host — the public key is
#      derived on the host itself.
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${DEPLOY_DIR}/common.sh"

PEERS_DIR="${DEPLOY_DIR}/peers"
CONF_SIDE="entry" IP="" OBFSKEY="${AE_OBFS_KEY:-}" DNS="10.8.0.1"
NODE="" CLIENT=""

# ---------------------------------------------------------------- parse -----
CMD="${1:-}"; [[ $# -gt 0 ]] && shift
ARGS=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        --host)    AE_HOST="${2:?}"; shift 2 ;;
        --local)   AE_HOST=""; shift ;;
        --conf)    CONF_SIDE="${2:?}"; shift 2 ;;
        --ip)      IP="${2:?}"; shift 2 ;;
        --obfs-key) OBFSKEY="${2:?}"; shift 2 ;;
        --dns)     DNS="${2:?}"; shift 2 ;;
        -h|--help) grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *)         ARGS+=("$1"); shift ;;
    esac
done

[[ -z "${AE_HOST}" || "${AE_HOST}" == *@* ]] || ae_fail "--host expects user@ip"
[[ "${CONF_SIDE}" == "entry" || "${CONF_SIDE}" == "exit" ]] || ae_fail "--conf must be entry|exit"

cmd_usage() {
    cat <<'EOF'
usage: peer.sh <add|list|show|revoke> [flags] <node> [client]
  add     [--conf entry|exit] [--ip 10.8.0.X] [--obfs-key k] [--dns servers] <node> <client>
  list    [<node>]
  show    <node> <client>
  revoke  <node> <client>
flags: --host user@ip | --local
EOF
    exit 2
}

case "${CMD}" in
    add)    [[ ${#ARGS[@]} -ge 2 ]] || cmd_usage; NODE="${ARGS[0]}"; CLIENT="${ARGS[1]}" ;;
    list)   NODE="${ARGS[0]:-}" ;;
    show|revoke) [[ ${#ARGS[@]} -ge 2 ]] || cmd_usage; NODE="${ARGS[0]}"; CLIENT="${ARGS[1]}" ;;
    *) cmd_usage ;;
esac

STACK="/opt/aegisvpn"
WGC="${STACK}/wg-configs/${CONF_SIDE}"

# ---------------------------------------------------------------- remote ops -
# All server-side work happens through ae_script (script via stdin, args after --).

case "${CMD}" in
# ------------------------------------------------------------------- ADD ----
add)
    ae_info "issuing peer '${CLIENT}' on node '${NODE}' (${CONF_SIDE})"

    # 1. real client keypair + psk (operator side)
    C_PRIV=$(ae_wg_genkey)
    C_PUB=$(printf '%s' "${C_PRIV}" | ae_wg_pubkey)
    C_PSK=$(ae_wg_genpsk)
    [[ "${C_PUB}" =~ ^[A-Za-z0-9+/]{43}=$ ]] || ae_fail "client key generation failed"

    # 2. server-side: derive server pubkey, allocate IP, append peer, apply live
    OUT=$(ae_script "${NODE}" "${CLIENT}" "${CONF_SIDE}" "${IP}" "${C_PUB}" "${C_PSK}" "${STACK}" <<'AEPEER'
set -euo pipefail
NODE="$1"; CLIENT="$2"; SIDE="$3"; REQ_IP="$4"; C_PUB="$5"; C_PSK="$6"; STACK="$7"
WGC="${STACK}/wg-configs/${SIDE}"
CONF="${WGC}/wg0.conf"
[[ -f "${CONF}" ]] || { echo "ERR conf-missing ${CONF}" >&2; exit 3; }

# server pubkey derived ON the host (private key never leaves)
S_PRIV=$(awk -F' = ' '/^\[Interface\]/{f=1} f&&/^PrivateKey/{print $2; exit}' "${CONF}")
[[ -n "${S_PRIV}" ]] || { echo "ERR no-private-key" >&2; exit 3; }
S_PUB=$(printf '%s' "${S_PRIV}" | docker run --rm -q -i lscr.io/linuxserver/wireguard:latest wg pubkey 2>/dev/null \
        || printf '%s' "${S_PRIV}" | wg pubkey)
[[ "${S_PUB}" =~ ^[A-Za-z0-9+/]{43}=$ ]] || { echo "ERR pub-derive" >&2; exit 3; }

# duplicate client name guard
if grep -q "AegisPeer: ${CLIENT}$" "${CONF}"; then
    echo "ERR duplicate ${CLIENT}" >&2; exit 4
fi

# IP allocation
if [[ -n "${REQ_IP}" ]]; then
    if grep -q "AllowedIPs = ${REQ_IP}/32" "${CONF}"; then echo "ERR ip-taken ${REQ_IP}" >&2; exit 5; fi
    C_IP="${REQ_IP}"
else
    BASE=$(awk -F' = ' '/^Address/{split($2,a,"."); print a[1]"."a[2]"."a[3]; exit}' "${CONF}")
    C_IP=""
    for oct in $(seq 2 254); do
        if ! grep -qE "AllowedIPs = ${BASE}\.${oct}/32" "${CONF}"; then C_IP="${BASE}.${oct}/32"; break; fi
    done
    [[ -n "${C_IP}" ]] || { echo "ERR no-free-ip" >&2; exit 5; }
fi

# listen port + endpoint for the client profile
PORT=$(awk -F' = ' '/^ListenPort/{print $2; exit}' "${CONF}")

# append tagged peer block
cat >> "${CONF}" <<PEER

# AegisPeer: ${CLIENT}
[Peer]
# name: ${CLIENT}
PublicKey = ${C_PUB}
PresharedKey = ${C_PSK}
AllowedIPs = ${C_IP}/32
PersistentKeepalive = 25
PEER
chmod 600 "${CONF}"

# apply live if the container is running (config re-read covers restarts too)
CONTAINER=""
if docker compose -f "${STACK}/docker-compose.yml" -f "${STACK}/compose.override.yml" ps node-entry 2>/dev/null | grep -q "running\|Up"; then
    CONTAINER="node-entry"
elif docker compose -f "${STACK}/docker-compose.yml" -f "${STACK}/compose.override.yml" ps node-exit 2>/dev/null | grep -q "running\|Up"; then
    CONTAINER="node-exit"
fi
APPLIED="config-only"
if [[ -n "${CONTAINER}" ]]; then
    if docker compose -f "${STACK}/docker-compose.yml" -f "${STACK}/compose.override.yml" \
         exec -T "${CONTAINER}" wg set wg0 peer "${C_PUB}" allowed-ips "${C_IP}/32" 2>/dev/null; then
        # preload the psk into the live session
        docker compose -f "${STACK}/docker-compose.yml" -f "${STACK}/compose.override.yml" \
            exec -T "${CONTAINER}" sh -c "wg set wg0 peer ${C_PUB} preshared-key /dev/stdin" <<< "${C_PSK}" >/dev/null 2>&1 || true
        APPLIED="live+config"
    fi
fi

# machine-readable result
echo "OK|${S_PUB}|${C_IP}|${PORT}|${APPLIED}"
AEPEER
)

    if echo "${OUT}" | grep -q "^ERR "; then
        ae_fail "peer add failed: ${OUT#ERR }"
    fi
    IFS='|' read -r _ S_PUB C_IP SPORT APPLIED <<< "${OUT}"
    if [[ -n "${AE_HOST}" ]]; then
        EP_HOST="${AE_HOST#*@}"          # root@1.2.3.4 -> 1.2.3.4
        EP_HOST="${EP_HOST%%:*}"         # strip ssh port suffix if present
    else
        EP_HOST=$(curl -fsS --max-time 6 https://api.ipify.org 2>/dev/null || echo "<node-host>")
    fi
    EP="${EP_HOST}:${SPORT}"

    # 3. client profile (operator side)
    OUTDIR="${PEERS_DIR}/${NODE}"
    mkdir -p "${OUTDIR}"
    CONFFILE="${OUTDIR}/${CLIENT}.conf"
    emit_client_conf "${CONFFILE}" "${CLIENT}" "${C_PRIV}" "${C_IP}" "${S_PUB}" "${EP}" "${DNS}" "${OBFSKEY}"

    ae_info "peer applied: ${APPLIED} | ip=${C_IP} | endpoint=${EP}"
    cat <<EOF

${C_G}──────────────── client profile: ${CONFFILE} ────────────────${C_0}
$(cat "${CONFFILE}")
${C_G}─────────────────────────────────────────────────────────────${C_0}

Import into any client:
  engine CLI : aegisvpn connect ${CONFFILE}
  systemd    : wg-quick up ${CONFFILE}
  Windows    : AegisVPN GUI → Settings → Import profile
EOF
    ;;

# ------------------------------------------------------------------ LIST ----
list)
    ae_script "${NODE}" "${STACK}" <<'AELIST'
set -euo pipefail
NODE="$1"; STACK="$2"
for side in entry exit; do
    CONF="${STACK}/wg-configs/${side}/wg0.conf"
    [[ -f "${CONF}" ]] || continue
    echo "== ${NODE} (${side}) =="
    awk -v RS='' -v ORS='\n\n' '/# AegisPeer:/' "${CONF}" 2>/dev/null \
        | grep -E "AegisPeer:|PublicKey|AllowedIPs" \
        | awk '/AegisPeer:/{name=$0; sub(/.*AegisPeer: /,"",name)}
               /PublicKey/{pub=$3}
               /AllowedIPs/{print name"  "$3"  "pub}' || true
    echo ""
done
AELIST
    ;;

# ------------------------------------------------------------------ SHOW ----
show)
    FILE="${PEERS_DIR}/${NODE}/${CLIENT}.conf"
    [[ -f "${FILE}" ]] || ae_fail "profile not found: ${FILE} (issue it first: peer.sh add ${NODE} ${CLIENT})"
    cat "${FILE}"
    ;;

# ---------------------------------------------------------------- REVOKE ----
revoke)
    [[ -f "${PEERS_DIR}/${NODE}/${CLIENT}.conf" ]] || ae_warn "no local profile for ${NODE}/${CLIENT} (revoking server-side anyway)"
    ae_info "revoking peer '${CLIENT}' on node '${NODE}'"
    RESULT=$(ae_script "${CLIENT}" "${CONF_SIDE}" "${STACK}" <<'AEREVOKE'
set -euo pipefail
CLIENT="$1"; SIDE="$2"; STACK="$3"
CONF="${STACK}/wg-configs/${SIDE}/wg0.conf"
[[ -f "${CONF}" ]] || { echo "ERR conf-missing"; exit 3; }
C_PUB=$(awk -v cl="# AegisPeer: ${CLIENT}" '
    $0 == cl {tag=1}
    tag && /^PublicKey/ {print $3; exit}' "${CONF}")
[[ -n "${C_PUB}" ]] || { echo "ERR peer-not-found ${CLIENT}"; exit 4; }
# live removal (best-effort)
if docker compose -f "${STACK}/docker-compose.yml" -f "${STACK}/compose.override.yml" ps node-entry 2>/dev/null | grep -q "running\|Up"; then
    docker compose -f "${STACK}/docker-compose.yml" -f "${STACK}/compose.override.yml" \
        exec -T node-entry wg set wg0 peer "${C_PUB}" remove 2>/dev/null || true
    echo "OK live-removed"
else
    echo "OK config-only"
fi
# strip block from config: from the tag line through the end of that [Peer] block
awk -v cl="# AegisPeer: ${CLIENT}" '
    $0 == cl {skip=1; next}
    skip && /^\[Peer\]/ {skip=0}
    !skip {print}' "${CONF}" > "${CONF}.tmp" && mv "${CONF}.tmp" "${CONF}"
chmod 600 "${CONF}"
AEREVOKE
)
    if [[ "${RESULT}" == ERR* ]]; then ae_fail "${RESULT}"; fi
    ae_info "server-side: ${RESULT}"
    if [[ -f "${PEERS_DIR}/${NODE}/${CLIENT}.conf" ]]; then
        mv "${PEERS_DIR}/${NODE}/${CLIENT}.conf" "${PEERS_DIR}/${NODE}/${CLIENT}.conf.revoked.$(date +%s)"
        ae_info "local profile moved to .revoked"
    fi
    ;;
esac
