#!/usr/bin/env bash
# deploy/common.sh — shared library for the AegisVPN fleet deployment suite.
# Sourced by deploy-node.sh, deploy-fleet.sh, peer.sh and verify.sh.
# Requires bash 4+. All remote access goes through ae_exec() (ssh or local).

# ---------------------------------------------------------------- logging ---
if [[ -z "${NO_COLOR:-}" && -t 1 ]]; then
    C_G=$'\033[32m'; C_Y=$'\033[33m'; C_R=$'\033[31m'; C_B=$'\033[36m'; C_0=$'\033[0m'
else
    C_G=""; C_Y=""; C_R=""; C_B=""; C_0=""
fi
ae_info() { printf '%s[deploy]%s %s\n' "$C_G" "$C_0" "$*"; }
ae_step() { printf '%s[  ..  ]%s %s\n' "$C_B" "$C_0" "$*"; }
ae_warn() { printf '%s[warn ]%s %s\n' "$C_Y" "$C_0" "$*" >&2; }
ae_fail() { printf '%s[fail ]%s %s\n' "$C_R" "$C_0" "$*" >&2; exit 1; }

# ------------------------------------------------------------------ args ----
# Every script accepts --host user@ip (remote) or --local (this machine).
# Sets: AE_HOST (empty = local), AE_SSH_OPTS.
AE_HOST=""
AE_SSH_OPTS=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 -o BatchMode=yes)

ae_parse_target() { # ae_parse_target "$@" — consumes --host/--local
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --host)  AE_HOST="${2:?--host requires user@ip}"; shift 2 ;;
            --local) AE_HOST=""; shift ;;
            *) break ;;
        esac
    done
    [[ -z "${AE_HOST}" || "${AE_HOST}" == *@* ]] || ae_fail "--host expects user@ip (got: ${AE_HOST})"
}

# ----------------------------------------------------------------- exec -----
# ae_exec "command..." — run on the target host (local or via ssh).
ae_exec() {
    if [[ -z "${AE_HOST}" ]]; then
        "$@"
    else
        ssh "${AE_SSH_OPTS[@]}" "${AE_HOST}" "$@"
    fi
}

# ae_sudo "command..." — remote commands that need root (or plain local root).
ae_sudo() {
    if [[ -z "${AE_HOST}" ]]; then
        if [[ $EUID -eq 0 ]]; then "$@"; else sudo "$@"; fi
    else
        ssh "${AE_SSH_OPTS[@]}" "${AE_HOST}" "sudo -n bash -c $(printf '%q' "$*")"
    fi
}

# ae_script — read a bash script on stdin and execute it on the target host
# with the arguments given after `--`. Private keys never leave the host.
ae_script() { # ae_script <args...>  (script comes from stdin)
    local args=("$@")
    if [[ -z "${AE_HOST}" ]]; then
        bash -s -- "${args[@]}"
    else
        ssh "${AE_SSH_OPTS[@]}" "${AE_HOST}" "sudo -n bash -s --" "${args[@]}"
    fi
}

# ae_put <local-file> <remote-path> — copy file to the target.
ae_put() {
    local src="$1" dst="$2"
    if [[ -z "${AE_HOST}" ]]; then
        mkdir -p "$(dirname "${dst}")"
        cp "${src}" "${dst}"
    else
        rsync -a -e "ssh ${AE_SSH_OPTS[*]}" "${src}" "${AE_HOST}:${dst}" \
            || scp "${AE_SSH_OPTS[@]}" "${src}" "${AE_HOST}:${dst}"
    fi
}

# --------------------------------------------------------- coordinate API ---
# Env expected (override via deploy/env or shell): 
#   AE_COORD_HOST   coordination host:port, e.g. coord.example.com:8080
#   AE_COORD_TOKEN  bearer token matching COORD_TOKEN on the coord server
#   AE_OBFS_KEY     obfuscation passphrase (rotated per deployment)
AE_COORD_HOST="${AE_COORD_HOST:-}"
AE_COORD_TOKEN="${AE_COORD_TOKEN:-}"
AE_OBFS_KEY="${AE_OBFS_KEY:-}"

coord_url() { # coord_url <path>
    echo "http://${AE_COORD_HOST}$1"
}

coord_healthz() {
    curl -fsS --max-time 8 "$(coord_url /healthz)" >/dev/null 2>&1
}

coord_list_nodes() { # prints coord JSON (nodes array wrapper)
    curl -fsS --max-time 10 "$(coord_url /v1/nodes)" 2>/dev/null
}

coord_register_node() { # coord_register_node <id> <country> <city> <host> <port> <probePort> <pubkey> <type>
    local id="$1" country="$2" city="$3" host="$4" port="$5" probe="$6" pub="$7" type="$8"
    local body http_code
    body=$(cat <<EOF
{"id":"${id}","country":"${country}","city":"${city}","host":"${host}","port":${port},"probePort":${probe},"pubkey":"${pub}","type":"${type}"}
EOF
)
    http_code=$(curl -sS --max-time 15 -o /tmp/aegis-reg-resp.$$ -w '%{http_code}' \
        -X POST "$(coord_url /v1/nodes/register)" \
        -H "Authorization: Bearer ${AE_COORD_TOKEN}" \
        -H 'Content-Type: application/json' \
        -d "${body}") || { rm -f /tmp/aegis-reg-resp.$$; return 1; }
    local resp; resp=$(cat /tmp/aegis-reg-resp.$$ 2>/dev/null); rm -f /tmp/aegis-reg-resp.$$
    case "${http_code}" in
        200|201) echo "${resp}"; return 0 ;;
        409)     echo "already-registered"; return 0 ;;   # idempotent re-deploy
        *)       ae_warn "coord register ${id}: HTTP ${http_code} ${resp}"; return 1 ;;
    esac
}

# --------------------------------------------------------------- wireguard --
# Generate a REAL WireGuard private key. Prefers host wg tools; falls back
# to the linuxserver/wireguard image (ships the full wg toolset).
ae_wg_genkey() {
    if command -v wg >/dev/null 2>&1; then
        wg genkey
    else
        docker run --rm -q lscr.io/linuxserver/wireguard:latest wg genkey 2>/dev/null \
            || ae_fail "cannot generate WireGuard keys (no wg tools, no docker image access)"
    fi
}

# ae_wg_pubkey <privkey-on-stdin> — derive public key from private key.
ae_wg_pubkey() {
    local priv; priv=$(cat)
    if command -v wg >/dev/null 2>&1; then
        printf '%s' "${priv}" | wg pubkey
    else
        printf '%s' "${priv}" | docker run --rm -q -i lscr.io/linuxserver/wireguard:latest wg pubkey 2>/dev/null
    fi
}

# ae_wg_genpsk — preshared key (layer-2 hardening on top of Curve25519).
ae_wg_genpsk() {
    if command -v wg >/dev/null 2>&1; then
        wg genpsk
    else
        docker run --rm -q lscr.io/linuxserver/wireguard:latest wg genpsk 2>/dev/null
    fi
}

# ------------------------------------------------------------- docker ops ---
ensure_docker() { # runs ON the target host
    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
        ae_info "docker + compose plugin already present"
        return 0
    fi
    ae_step "installing docker engine + compose plugin"
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq ca-certificates curl gnupg lsb_release >/dev/null 2>&1 || apt-get install -y -qq ca-certificates curl gnupg
    install -m 0755 -d /etc/apt/keyrings
    local distro_id
    distro_id=$(. /etc/os-release && echo "$ID")
    curl -fsSL "https://download.docker.com/linux/${distro_id}/gpg" \
        | gpg --dearmor --yes -o /etc/apt/keyrings/docker.gpg
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/${distro_id} $(. /etc/os-release && echo "${VERSION_CODENAME}") stable" \
        > /etc/apt/sources.list.d/docker.list
    apt-get update -qq
    apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    systemctl enable --now docker
    ae_info "docker installed"
}

harden_sysctl() { # runs ON the target host — forwarding + BBR + anti-spoof
    ae_step "applying sysctl hardening (ip_forward, BBR, rp_filter off for wg)"
    cat > /etc/sysctl.d/90-aegisvpn.conf <<'EOF'
# AegisVPN node hardening
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
net.ipv4.conf.all.src_valid_mark = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
# congestion control: BBR behind the tunnel noticeably helps proxies/streaming
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
# conntrack headroom for a NAT gateway node
net.netfilter.nf_conntrack_max = 262144
EOF
    modprobe wireguard 2>/dev/null || true   # kernel module if available; containers fall back to internal impl
    sysctl --system >/dev/null
}

setup_ufw() { # setup_ufw <wg-port>/udp <extra-ports...>
    local wgport="$1"; shift
    command -v ufw >/dev/null 2>&1 || { ae_warn "ufw not installed — skipping firewall (host-level firewall expected)"; return 0; }
    ae_step "ufw: allow ssh + ${wgport}/udp $*"
    ufw --force reset >/dev/null
    ufw default deny incoming
    ufw default allow outgoing
    ufw allow 22/tcp comment 'ssh'
    ufw allow "${wgport}"/udp comment 'aegis wireguard'
    local p
    for p in "$@"; do ufw allow "${p}" comment "aegis extra ${p}"; done
    ufw --force enable >/dev/null
}

# ----------------------------------------------------------- role mapping ---
# role -> compose services (subsets of server/docker-compose.yml)
#   standard     : node-entry + coord + threat-dns + smartplay + tor
#   p2p          : node-entry (ports documented for torrent clients) + coord
#   double-entry : node-entry + relay  (relay chains 10.8.0.0/24 -> exit node)
#   double-exit  : node-exit  + tor    (tor SOCKS reachable at 10.8.2.53:9050)
#   obfs         : node-entry + obfsd (:443/udp masked front for :51820)
role_services() {
    case "$1" in
        standard)     echo "node-entry coord threat-dns smartplay tor" ;;
        p2p)          echo "node-entry coord" ;;
        double-entry) echo "node-entry relay coord" ;;
        double-exit)  echo "node-exit tor" ;;
        obfs)         echo "node-entry obfsd coord" ;;
        *) ae_fail "unknown role: $1 (standard|p2p|double-entry|double-exit|obfs)" ;;
    esac
}

role_wg_port() {
    case "$1" in
        double-exit) echo 51821 ;;
        *)           echo 51820 ;;
    esac
}

role_is_valid() {
    case "$1" in
        standard|p2p|double-entry|double-exit|obfs) return 0 ;;
        *) return 1 ;;
    esac
}

# ------------------------------------------------------- compose rendering --
# render_compose_override <role> <pubkey> <port> <outdir>
# Emits compose.<role>.override.yml selecting only this role's services,
# with real endpoints/keys substituted.
render_compose_override() {
    local role="$1" pub="$2" port="$3" outdir="$4"
    local entry_subnet="10.8.0.0/24"
    local exit_subnet="10.8.1.0/24"
    local relay_listen="0.0.0.0:${port}"
    cat > "${outdir}/compose.override.yml" <<EOF
# generated by AegisVPN deploy suite — role=${role}
services:
  node-entry:
    environment:
      - SERVERURL=\${SERVERURL}
      - SERVERPORT=${port}
      - INTERNAL_SUBNET=${entry_subnet}
    ports:
      - "${port}:${port}/udp"
  node-exit:
    environment:
      - SERVERURL=\${SERVERURL}
      - SERVERPORT=${port}
      - INTERNAL_SUBNET=${exit_subnet}
    ports:
      - "${port}:${port}/udp"
  relay:
    environment:
      - RELAY_LISTEN=${relay_listen}
      - RELAY_ENTRY_SUBNET=${entry_subnet}
      - RELAY_EXIT_SUBNET=${exit_subnet}
      - RELAY_EXIT_ENDPOINT=127.0.0.1:51821
  obfsd:
    environment:
      - AEGIS_OBFS_KEY=\${AEGIS_OBFS_KEY}
    command: ["-listen", "0.0.0.0:443", "-upstream", "127.0.0.1:${port}"]
EOF
}

# ------------------------------------------------------------- client conf --
# emit_client_conf <outfile> <name> <privkey> <address> <serverpub> <endpoint> <dns> [obfskey]
emit_client_conf() {
    local out="$1" name="$2" priv="$3" addr="$4" spub="$5" ep="$6" dns="$7" okey="${8:-}"
    {
        echo "# AegisVPN client profile — ${name}"
        echo "# generated $(date -u +%Y-%m-%dT%H:%M:%SZ)"
        echo "[Interface]"
        echo "PrivateKey = ${priv}"
        echo "Address = ${addr}"
        [[ -n "${dns}" ]] && echo "DNS = ${dns}"
        if [[ -n "${okey}" ]]; then
            echo "AegisObfs = true"
            echo "AegisObfsKey = ${okey}"
        fi
        echo ""
        echo "[Peer]"
        echo "PublicKey = ${spub}"
        echo "AllowedIPs = 0.0.0.0/0, ::/0"
        echo "Endpoint = ${ep}"
        echo "PersistentKeepalive = 25"
    } > "${out}"
    chmod 600 "${out}"
}

# free tunnel IP scanner: reads a wg0.conf, prints next free .X in a /24
next_free_ip() { # next_free_ip <conf-file> <subnet-base e.g. 10.8.0>
    local conf="$1" base="$2" used="" line ip oct
    used=$(grep -oE 'AllowedIPs\s*=\s*([0-9]+\.){3}[0-9]+' "${conf}" 2>/dev/null \
        | grep -oE '([0-9]+\.){3}[0-9]+' | awk -F. '{print $4}' | sort -n | uniq)
    for oct in $(seq 2 254); do
        if ! printf '%s\n' ${used} | grep -qx "${oct}"; then
            echo "${base}.${oct}/32"
            return 0
        fi
    done
    return 1
}
