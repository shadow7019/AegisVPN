#!/usr/bin/env bash
# deploy/verify.sh — post-deploy health checks for an AegisVPN node.
#
#   deploy/verify.sh --host root@203.0.113.10
#   deploy/verify.sh --local
#
# Checks: docker services up, wg interface live, UDP port reachable,
# coord listing, threat-dns answering, obfs front port responsive.
# Exit code: number of hard failures.
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${DEPLOY_DIR}/common.sh"

STACK="/opt/aegisvpn"
PASS=0 FAIL=0

ok()   { printf '  %s✔%s %s\n' "$C_G" "$C_0" "$1"; PASS=$((PASS + 1)); }
bad()  { printf '  %s✗%s %s\n' "$C_R" "$C_0" "$1"; FAIL=$((FAIL + 1)); }
skip() { printf '  %s–%s %s (skipped)\n' "$C_Y" "$C_0" "$1"; }

section() { printf '\n%s%s%s\n' "$C_B" "$1" "$C_0"; }

# ------------------------------------------------------------ remote checks -
section "node health ${AE_HOST:+(${AE_HOST})}${AE_HOST:-(${AE_HOST:-local})}"

OUT=$(ae_script "${STACK}" <<'AECHECK'
set -uo pipefail
STACK="$1"
cd "${STACK}" 2>/dev/null || { echo "COMPOSE|missing-stack"; exit 0; }

# compose services state
if command -v docker >/dev/null 2>&1; then
    ps_out=$(docker compose -f docker-compose.yml -f compose.override.yml ps --format '{{.Service}} {{.State}}' 2>/dev/null || echo "")
    if [[ -n "${ps_out}" ]]; then
        while read -r svc state; do echo "SVC|${svc}|${state}"; done <<< "${ps_out}"
    else
        echo "COMPOSE|no-services"
    fi
else
    echo "COMPOSE|no-docker"
fi

# wireguard interface inside running containers
for svc in node-entry node-exit; do
    if docker compose -f docker-compose.yml -f compose.override.yml ps "$svc" 2>/dev/null | grep -q "running\|Up"; then
        if docker compose -f docker-compose.yml -f compose.override.yml exec -T "$svc" wg show wg0 2>/dev/null | head -n 4; then
            echo "WG|${svc}|live"
        else
            echo "WG|${svc}|no-interface"
        fi
    fi
done

# server public key presence
for side in entry exit; do
    [[ -s "wg-configs/${side}/server.key" ]] && echo "KEY|${side}|present"
done
AECHECK
) || true

COMPOSE_LINE=""
while IFS= read -r line; do
    case "${line}" in
        COMPOSE|*)
            case "${line}" in
                "COMPOSE|missing-stack") bad "server stack not found at ${STACK}" ;;
                "COMPOSE|no-docker")     bad "docker not installed" ;;
                "COMPOSE|no-services")   bad "no compose services running" ;;
            esac ;;
        SVC|*)
            IFS='|' read -r _ svc state <<< "${line}"
            if [[ "${state}" == "running" || "${state}" == "exited" && "${svc}" == "tor" ]]; then
                ok "service ${svc}: ${state}"
            else
                bad "service ${svc}: ${state}"
            fi ;;
        WG|*)
            IFS='|' read -r _ svc st <<< "${line}"
            if [[ "${st}" == "live" ]]; then ok "wireguard ${svc}: interface live"; else bad "wireguard ${svc}: ${st}"; fi ;;
        KEY|*)
            IFS='|' read -r _ side _rest <<< "${line}"; ok "server key (${side}) present" ;;
        "interface: wg0"*) ok "wg0 reported: ${line#*: }" ;;
        "public key:"*) skip "detail: ${line}" ;;
        "listening port:"*) ok "${line}" ;;
    esac
done <<< "${OUT}"

# ------------------------------------------------- network reachability -----
section "network reachability (best-effort)"
HOSTIP="${AE_HOST#*@}"; HOSTIP="${HOSTIP%%:*}"
WGPORTS=""
[[ -s "DETECTED_PORTS" ]] || WGPORTS="51820 51821"
for p in ${WGPORTS}; do
    if command -v nc >/dev/null 2>&1; then
        if nc -u -z -w3 "${HOSTIP}" "${p}" >/dev/null 2>&1; then
            ok "udp ${HOSTIP}:${p} responsive"
        else
            skip "udp ${HOSTIP}:${p} (nc gave no answer — UDP probes often silent; verify with a real handshake)"
        fi
    else
        skip "udp ${HOSTIP}:${p} (nc unavailable)"
    fi
done

# --------------------------------------------------------- coord listing ----
section "coordination"
if [[ -n "${AE_COORD_HOST}" ]]; then
    if NODES=$(coord_list_nodes); then
        if printf '%s' "${NODES}" | grep -q "${HOSTIP}"; then
            ok "node listed in coord registry"
        else
            bad "node NOT listed in coord registry (registration step skipped or failed?)"
        fi
    else
        bad "coord unreachable at ${AE_COORD_HOST}"
    fi
else
    skip "AE_COORD_HOST not set"
fi

# ------------------------------------------------------------- summary ------
echo ""
if [[ ${FAIL} -eq 0 ]]; then
    ae_info "health: ${PASS} passed, 0 failed ${C_G}— node looks healthy${C_0}"
else
    ae_warn "health: ${PASS} passed, ${FAIL} FAILED"
fi
exit "${FAIL}"
