#!/usr/bin/env bash
# deploy/deploy-node.sh — turn one fresh Ubuntu 22.04+/Debian 12+ box into a
# real AegisVPN node (WireGuard + role services + coord registration).
#
#   # run on the VPS itself:
#   sudo ./deploy-node.sh --local --role standard --country DE --city Falkenstein
#
#   # or from your laptop (ships the server/ stack over ssh, then re-runs itself):
#   ./deploy-node.sh --host root@203.0.113.10 --role double-entry --country NL --city Amsterdam
#
# Optional flags:
#   --name <node-id>        override node id (default: <host>-<role>)
#   --port <udp-port>       WireGuard listen port (default: 51820, exit: 51821)
#   --coord-host h:p        coordination API for registration (also env AE_COORD_HOST)
#   --coord-token t         bearer token (also env AE_COORD_TOKEN)
#   --obfs-key k            obfuscation passphrase (also env AE_OBFS_KEY)
#   --smartplay-ips ip,ip   SmartPlay egress IPs
#   --skip-register         deploy but do not talk to coord
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${DEPLOY_DIR}/common.sh"

ROLE="" COUNTRY="XX" CITY="datacenter" NAME="" PORT="" SMARTPLAY_IPS="146.70.99.101,146.70.99.102" SKIP_REGISTER=0
REMOTE_ROOT="/opt/aegisvpn"

# ---------------------------------------------------------------- parse -----
while [[ $# -gt 0 ]]; do
    case "$1" in
        --host)          AE_HOST="${2:?}"; shift 2 ;;
        --local)         AE_HOST=""; shift ;;
        --role)          ROLE="${2:?}"; shift 2 ;;
        --country)       COUNTRY="${2:?}"; shift 2 ;;
        --city)          CITY="${2:?}"; shift 2 ;;
        --name)          NAME="${2:?}"; shift 2 ;;
        --port)          PORT="${2:?}"; shift 2 ;;
        --coord-host)    AE_COORD_HOST="${2:?}"; shift 2 ;;
        --coord-token)   AE_COORD_TOKEN="${2:?}"; shift 2 ;;
        --obfs-key)      AE_OBFS_KEY="${2:?}"; shift 2 ;;
        --smartplay-ips) SMARTPLAY_IPS="${2:?}"; shift 2 ;;
        --skip-register) SKIP_REGISTER=1; shift ;;
        -h|--help)       grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *)               ae_fail "unknown arg: $1 (see --help)" ;;
    esac
done

[[ -n "${ROLE}" ]] || ae_fail "usage: deploy-node.sh [--host user@ip|--local] --role <standard|p2p|double-entry|double-exit|obfs> --country US --city \"Ashburn\""
role_is_valid "${ROLE}" || ae_fail "invalid role: ${ROLE}"
[[ -z "${AE_HOST}" || "${AE_HOST}" == *@* ]] || ae_fail "--host expects user@ip (got: ${AE_HOST})"
[[ -n "${PORT}" ]] || PORT=$(role_wg_port "${ROLE}")

# ------------------------------------------------------------- remote hop ---
if [[ -n "${AE_HOST}" ]]; then
    ae_step "shipping server/ stack to ${AE_HOST}:${REMOTE_ROOT}"
    ssh "${AE_SSH_OPTS[@]}" "${AE_HOST}" "sudo -n mkdir -p ${REMOTE_ROOT} || mkdir -p ${REMOTE_ROOT}"
    rsync -a -e "ssh ${AE_SSH_OPTS[*]}" \
        --include 'docker-compose.yml' \
        --include 'coord/***' --include 'relay/***' --include 'obfsd/***' \
        --include 'threatdns/***' --include 'smartdns/***' \
        --include 'blocklists/***' --include 'wg-configs/***' \
        --exclude '*' \
        "${DEPLOY_DIR}/../server/" "${AE_HOST}:${REMOTE_ROOT}/"

    ae_put "${BASH_SOURCE[0]}" "/tmp/aegis-deploy-node.sh"
    ae_put "${DEPLOY_DIR}/common.sh" "/tmp/aegis-common.sh"
    ae_step "executing deployment on ${AE_HOST} (local mode)"
    # shellcheck disable=SC2086
    ssh "${AE_SSH_OPTS[@]}" "${AE_HOST}" \
        "AE_COORD_HOST='${AE_COORD_HOST}' AE_COORD_TOKEN='${AE_COORD_TOKEN}' AE_OBFS_KEY='${AE_OBFS_KEY}' \
         sudo -n bash /tmp/aegis-deploy-node.sh --local --role ${ROLE} --country '${COUNTRY}' --city '${CITY}' \
         ${NAME:+--name ${NAME}} --port ${PORT} --smartplay-ips '${SMARTPLAY_IPS}' ${SKIP_REGISTER:+--skip-register}"
    exit $?
fi

# ============================== LOCAL MODE (on the node) ====================
if [[ $EUID -ne 0 ]]; then ae_fail "local mode must run as root (sudo)"; fi

NODE_NAME="${NAME:-$(hostname -s 2>/dev/null || echo node)-${ROLE}}"
SERVERURL=$(curl -fsS --max-time 8 https://api.ipify.org 2>/dev/null \
    || curl -fsS --max-time 8 https://ifconfig.me 2>/dev/null \
    || hostname -I 2>/dev/null | awk '{print $1}')
[[ -n "${SERVERURL}" ]] || ae_fail "cannot determine public IP"
ae_info "node ${NODE_NAME} — public endpoint ${SERVERURL}:${PORT}"

STACK="${REMOTE_ROOT}"
if [[ ! -f "${STACK}/docker-compose.yml" ]]; then
    if [[ -f "${DEPLOY_DIR}/../server/docker-compose.yml" ]]; then
        STACK="${DEPLOY_DIR}/../server"   # developer running from a checkout
    else
        ae_fail "server stack not found at ${STACK} — deploy via --host so the stack is shipped"
    fi
fi
cd "${STACK}"

# 1. hardening + docker + firewall
harden_sysctl
ensure_docker
if [[ "${ROLE}" == "double-exit" ]]; then
    setup_ufw "${PORT}" "9050/tcp"
else
    setup_ufw "${PORT}"
fi
if [[ "${ROLE}" == "standard" ]]; then
    ufw allow 53 comment 'aegis threat dns' 2>/dev/null || true
    ufw allow 5353/udp comment 'aegis smartplay' 2>/dev/null || true
fi

# 2. REAL keys: server keypair for this node's wg interface
ae_step "generating WireGuard server keypair"
mkdir -p "wg-configs/entry" "wg-configs/exit"
case "${ROLE}" in
    double-exit) WGC="wg-configs/exit" ;;
    *)           WGC="wg-configs/entry" ;;
esac
if [[ ! -s "${WGC}/server.key" ]]; then
    ae_wg_genkey > "${WGC}/server.key"
    chmod 600 "${WGC}/server.key"
fi
PRIV=$(cat "${WGC}/server.key")
PUB=$(printf '%s' "${PRIV}" | ae_wg_pubkey)
[[ "${PUB}" =~ ^[A-Za-z0-9+/]{43}=$ ]] || ae_fail "public key derivation failed (got: '${PUB}')"
ae_info "server pubkey: ${PUB}"

# render server wg0.conf from the key (idempotent; peers appended by peer.sh)
SUBNET_BASE="10.8.0"
[[ "${ROLE}" == "double-exit" ]] && SUBNET_BASE="10.8.1"
cat > "${WGC}/wg0.conf" <<EOF
[Interface]
PrivateKey = ${PRIV}
Address = ${SUBNET_BASE}.1/24
ListenPort = ${PORT}
PostUp = iptables -A FORWARD -i wg0 -j ACCEPT; iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
PostDown = iptables -D FORWARD -i wg0 -j ACCEPT; iptables -t nat -D POSTROUTING -o eth0 -j MASQUERADE
EOF
chmod 600 "${WGC}/wg0.conf"

# 3. .env for compose substitution
ae_step "rendering .env"
if [[ -z "${AE_OBFS_KEY}" ]]; then
    AE_OBFS_KEY=$(head -c 24 /dev/urandom | base64 | tr -d '/+=' | head -c 24)
    ae_warn "generated ephemeral obfs key — set AE_OBFS_KEY to persist across redeploys"
fi
cat > .env <<EOF
SERVERURL=${SERVERURL}
AEGIS_OBFS_KEY=${AE_OBFS_KEY}
COORD_TOKEN=${AE_COORD_TOKEN:-$(head -c 24 /dev/urandom | base64 | tr -d '/+=' | head -c 24)}
EOF
chmod 600 .env

# 4. role-filtered compose
render_compose_override "${ROLE}" "${PUB}" "${PORT}" .
SERVICES=$(role_services "${ROLE}")
ae_step "starting services: ${SERVICES}"
# shellcheck disable=SC2086
docker compose -f docker-compose.yml -f compose.override.yml up -d --build ${SERVICES}

# 5. wait for a live wireguard interface inside the containers
ae_step "waiting for wg0 interface"
WG_OK=0
for _ in $(seq 1 30); do
    if docker compose exec -T node-entry wg show wg0 >/dev/null 2>&1 \
       || docker compose exec -T node-exit wg show wg0 >/dev/null 2>&1; then
        WG_OK=1; break
    fi
    sleep 1
done
if [[ "${WG_OK}" == "1" ]]; then
    ae_info "wireguard interface confirmed (wg0 up, port ${PORT})"
else
    ae_warn "wg interface not confirmed within 30s — check 'docker compose logs'"
fi

# 6. coord registration with the REAL pubkey
if [[ "${SKIP_REGISTER}" != "1" && -n "${AE_COORD_HOST}" ]]; then
    ae_step "registering with coord ${AE_COORD_HOST}"
    if coord_register_node "${NODE_NAME}" "${COUNTRY}" "${CITY}" "${SERVERURL}" "${PORT}" "${PORT}" "${PUB}" "${ROLE}"; then
        ae_info "registered: ${NODE_NAME} type=${ROLE}"
    else
        ae_warn "registration failed — node is up but not listed by coord"
    fi
elif [[ "${SKIP_REGISTER}" != "1" ]]; then
    ae_warn "no --coord-host given — node deployed but NOT registered (invisible to clients)"
fi

# 7. summary + client peer snippet
cat <<EOF

${C_G}────────────────────────────────────────────────────────${C_0}
  node        : ${NODE_NAME}
  role        : ${ROLE}
  endpoint    : ${SERVERURL}:${PORT}/udp
  pubkey      : ${PUB}
  services    : ${SERVICES}
${C_G}────────────────────────────────────────────────────────${C_0}

Add a client peer:
  deploy/peer.sh add --host <user@this-box> ${NODE_NAME} <client-name>

Client [Peer] block for that node:
  [Peer]
  PublicKey = ${PUB}
  AllowedIPs = 0.0.0.0/0, ::/0
  Endpoint = ${SERVERURL}:${PORT}
  PersistentKeepalive = 25
EOF
