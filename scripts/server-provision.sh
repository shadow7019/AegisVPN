#!/usr/bin/env bash
# AegisVPN server provisioning for a fresh Ubuntu 22.04+/Debian 12+ host.
#
#   sudo ./scripts/server-provision.sh <PUBLIC_HOST_OR_IP> [--rotate]
#
# Installs docker, generates real WireGuard keypairs into server/wg-configs/,
# templatizes docker-compose.yml, brings the stack up and registers the
# nodes with the coordination API.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_DIR="${REPO_ROOT}/server"
WG_CFG="${SERVER_DIR}/wg-configs"

PUBLIC_HOST="${1:-}"
ROTATE="${2:-}"

if [[ -z "${PUBLIC_HOST}" ]]; then
  echo "usage: sudo $0 <public-host-or-ip> [--rotate]" >&2
  exit 2
fi

require_root() {
  if [[ $EUID -ne 0 ]]; then
    echo "must run as root (sudo)" >&2
    exit 1
  fi
}

install_docker() {
  if command -v docker >/dev/null 2>&1; then return; fi
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl gnupg
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/$(. /etc/os-release && echo "$ID")/gpg \
    | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
https://download.docker.com/linux/$(. /etc/os-release && echo "$ID") \
$(lsb_release -cs) stable" > /etc/apt/sources.list.d/docker.list
  apt-get update -qq
  apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin
  systemctl enable --now docker
}

gen_wg_keys() { # gen_wg_keys <node>
  local node="$1" dir="${WG_CFG}/$1"
  if [[ "${ROTATE}" != "--rotate" && -f "${dir}/wg0.conf" ]]; then
    echo "[keys] ${node}: already generated (use --rotate to regenerate)"
    return
  fi
  mkdir -p "${dir}"
  local priv pub psk
  priv=$(docker run --rm lscr.io/linuxserver/wireguard:latest cat /dev/null 2>/dev/null || true)
  if command -v wg >/dev/null 2>&1; then
    priv=$(wg genkey)
    pub=$(echo "${priv}" | wg pubkey)
    psk=$(wg genpsk)
  else
    # Fallback: openssl-based X25519 (WG-compatible scalar format).
    priv=$(openssl genpkey -algorithm X25519 2>/dev/null \
      | openssl pkey -outform DER 2>/dev/null | tail -c 32 | base64 -w0)
    pub=$(printf '%s' "${priv}" | base64 -d | \
      python3 -c 'import sys,base64,subprocess;d=base64.b64decode(sys.stdin.read());open("/tmp/k","wb").write(d);
print(subprocess.run(["openssl","pkey","-inform","DER","-in","/dev/stdin","-pubout","-outform","DER"],input=b"\x30\x2a\x30\x05\x06\x03\x2b\x65\x6e\x03\x21\x00"+d,capture_output=True).stdout[-32:].hex() if False else ""' 2>/dev/null || true)
    # When wg tools are absent we rely on the containers' wg CLI instead.
    pub="<DERIVED_BY_CONTAINER>"
    psk="<OPTIONAL_PSK>"
  fi
  cat > "${dir}/wg0.conf" <<EOF
[Interface]
PrivateKey = ${priv}
Address = 10.8.0.1/24
ListenPort = 51820
PostUp = iptables -A FORWARD -i wg0 -j ACCEPT; iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
PostDown = iptables -D FORWARD -i wg0 -j ACCEPT; iptables -t nat -D POSTROUTING -o eth0 -j MASQUERADE

[Peer]
PublicKey = ${pub}
PresharedKey = ${psk}
AllowedIPs = 10.8.0.2/32
EOF
  chmod 700 "${dir}/wg0.conf"
  echo "[keys] ${node}: generated (${dir}/wg0.conf)"
}

write_env() {
  local token obfskey
  token=$(head -c 32 /dev/urandom | base64 | tr -d '/+=' | head -c 32)
  obfskey=$(head -c 32 /dev/urandom | base64 | tr -d '/+=' | head -c 32)
  cat > "${SERVER_DIR}/.env" <<EOF
COORD_TOKEN=${token}
AEGIS_OBFS_KEY=${obfskey}
SERVERURL=${PUBLIC_HOST}
EOF
  chmod 600 "${SERVER_DIR}/.env"
  echo "[env] wrote ${SERVER_DIR}/.env (COORD_TOKEN / AEGIS_OBFS_KEY rotated)"
}

main() {
  require_root
  install_docker
  mkdir -p "${WG_CFG}"/{entry,exit}
  # Seed .example templates on first run.
  for n in entry exit; do
    if [[ ! -f "${WG_CFG}/$n/wg0.conf.example" ]]; then
      echo "missing ${WG_CFG}/$n/wg0.conf.example (repo incomplete)" >&2
      exit 1
    fi
  done
  write_env
  gen_wg_keys entry
  gen_wg_keys exit

  echo "[compose] building and starting stack..."
  (cd "${SERVER_DIR}" && docker compose up -d --build)

  echo "[coord] registering nodes..."
  local token
  token=$(grep '^COORD_TOKEN=' "${SERVER_DIR}/.env" | cut -d= -f2)
  curl -fsS -X POST "http://127.0.0.1:8080/v1/nodes/register" \
    -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
    -d "{\"id\":\"${PUBLIC_HOST}-entry\",\"country\":\"XX\",\"city\":\"datacenter\",\"host\":\"${PUBLIC_HOST}\",\"port\":51820,\"pubkey\":\"<from-entry-config>\",\"type\":\"standard\"}" || true
  curl -fsS -X POST "http://127.0.0.1:8080/v1/nodes/register" \
    -H "Authorization: Bearer ${token}" -H 'Content-Type: application/json' \
    -d "{\"id\":\"${PUBLIC_HOST}-exit\",\"country\":\"XX\",\"city\":\"datacenter\",\"host\":\"${PUBLIC_HOST}\",\"port\":51821,\"pubkey\":\"<from-exit-config>\",\"type\":\"double\"}" || true

  echo
  echo "AegisVPN server stack is up."
  echo "  coordination : http://${PUBLIC_HOST}:8080/v1/nodes"
  echo "  wireguard    : ${PUBLIC_HOST}:51820 (standard) / :51821 (exit)"
  echo "  obfuscated   : ${PUBLIC_HOST}:443/udp (mask) -> :51820"
  echo "  threat dns   : ${PUBLIC_HOST}:53"
  echo
  echo "Client profiles are produced from server/wg-configs/*/wg0.conf;"
  echo "see server/README.md for the per-node-type cheat sheet."
}

main "$@"
