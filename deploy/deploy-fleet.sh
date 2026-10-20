#!/usr/bin/env bash
# deploy/deploy-fleet.sh — roll out the whole AegisVPN fleet in parallel.
#
#   cp deploy/fleet.conf.example deploy/fleet.conf   # edit hosts/roles
#   deploy/deploy-fleet.sh                # deploy everything, print summary
#   deploy/deploy-fleet.sh --limit de-fra-1   # redeploy one node
#   deploy/deploy-fleet.sh --dry-run      # show the plan, touch nothing
#
# fleet.conf format (one node per line):
#   name,host,role,country,city[,port]
# Environment (or deploy/env): AE_COORD_HOST, AE_COORD_TOKEN, AE_OBFS_KEY.
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${DEPLOY_DIR}/common.sh"

FLEET="${DEPLOY_DIR}/fleet.conf"
LIMIT="" DRY_RUN=0
LOGDIR="${DEPLOY_DIR}/logs"
mkdir -p "${LOGDIR}"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --fleet)   FLEET="${2:?}"; shift 2 ;;
        --limit)   LIMIT="${2:?}"; shift 2 ;;
        --dry-run) DRY_RUN=1; shift ;;
        -h|--help) grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *)         ae_fail "unknown arg: $1" ;;
    esac
done
[[ -f "${FLEET}" ]] || ae_fail "fleet config not found: ${FLEET} (copy fleet.conf.example)"

[[ -n "${AE_COORD_HOST}" ]] || ae_warn "AE_COORD_HOST not set — nodes will deploy but NOT register with coord"
[[ -n "${AE_COORD_TOKEN}" ]] || ae_warn "AE_COORD_TOKEN not set — registration will be refused by coord"

# ---------------------------------------------------------------- plan ------
declare -a NAMES HOSTS ROLES COUNTRIES CITIES PORTS
lineno=0
while IFS= read -r raw || [[ -n "${raw}" ]]; do
    lineno=$((lineno + 1))
    line="${raw%%#*}"                       # strip comments
    line="${line//[[:space:]]/}"
    [[ -n "${line}" ]] || continue
    IFS=',' read -r name host role country city port extra <<< "${line}"
    if [[ -n "${extra:-}" ]]; then ae_fail "${FLEET}:${lineno}: too many fields"; fi
    role_is_valid "${role}" || ae_fail "${FLEET}:${lineno}: invalid role '${role}'"
    if [[ -n "${LIMIT}" && "${name}" != "${LIMIT}" ]]; then continue; fi
    NAMES+=("${name}"); HOSTS+=("${host}"); ROLES+=("${role}")
    COUNTRIES+=("${country}"); CITIES+=("${city}"); PORTS+=("${port:-}")
done < "${FLEET}"

COUNT=${#NAMES[@]}
[[ ${COUNT} -gt 0 ]] || ae_fail "no nodes selected${LIMIT:+ (limit ${LIMIT})}"
ae_info "fleet plan: ${COUNT} node(s)${DRY_RUN:+ (dry run)}"
for i in $(seq 0 $((COUNT - 1))); do
    printf '  %-18s %-22s %-13s %s / %s\n' "${NAMES[$i]}" "${HOSTS[$i]}" "${ROLES[$i]}" "${COUNTRIES[$i]}" "${CITIES[$i]}"
done
[[ "${DRY_RUN}" == "1" ]] && exit 0

# --------------------------------------------------------------- launch -----
declare -a PIDS LOGFILES
ae_info "rolling out in parallel (logs: ${LOGDIR}/)"
for i in $(seq 0 $((COUNT - 1))); do
    name="${NAMES[$i]}"; host="${HOSTS[$i]}"; role="${ROLES[$i]}"
    country="${COUNTRIES[$i]}"; city="${CITIES[$i]}"; port="${PORTS[$i]}"
    log="${LOGDIR}/${name}.log"
    LOGFILES+=("${log}")
    {
        echo "== ${name} @ ${host} role=${role} =="
        if [[ "${host}" == "local" ]]; then
            sudo -n env AE_COORD_HOST="${AE_COORD_HOST}" AE_COORD_TOKEN="${AE_COORD_TOKEN}" AE_OBFS_KEY="${AE_OBFS_KEY}" \
                bash "${DEPLOY_DIR}/deploy-node.sh" --local --role "${role}" \
                --name "${name}" --country "${country}" --city "${city}" ${port:+--port "${port}"} \
                || sudo env AE_COORD_HOST="${AE_COORD_HOST}" AE_COORD_TOKEN="${AE_COORD_TOKEN}" AE_OBFS_KEY="${AE_OBFS_KEY}" \
                bash "${DEPLOY_DIR}/deploy-node.sh" --local --role "${role}" \
                --name "${name}" --country "${country}" --city "${city}" ${port:+--port "${port}"}
        else
            AE_COORD_HOST="${AE_COORD_HOST}" AE_COORD_TOKEN="${AE_COORD_TOKEN}" AE_OBFS_KEY="${AE_OBFS_KEY}" \
                bash "${DEPLOY_DIR}/deploy-node.sh" --host "${host}" --role "${role}" \
                --name "${name}" --country "${country}" --city "${city}" ${port:+--port "${port}"}
        fi
    } > "${log}" 2>&1 &
    PIDS+=($!)
    ae_step "launched ${name} (pid ${PIDS[-1]})"
done

# ------------------------------------------------------------------ reap ----
FAILED=0
ae_info "waiting for ${COUNT} deployment(s)..."
for i in $(seq 0 $((COUNT - 1))); do
    if wait "${PIDS[$i]}"; then
        ae_info "${C_G}✔${C_0} ${NAMES[$i]}"
    else
        ae_warn "✗ ${NAMES[$i]} — see ${LOGFILES[$i]}"
        FAILED=$((FAILED + 1))
    fi
done

# -------------------------------------------------------------- summary -----
echo ""
ae_info "fleet summary (from coord):"
NODES_JSON=$(coord_list_nodes || echo "")
if [[ -n "${NODES_JSON}" ]]; then
    printf '  %-18s %-13s %-6s %-26s %-6s %-8s %s\n' "NODE" "ROLE" "CC" "ENDPOINT" "LOAD" "LAT(ms)" "STATE"
    for i in $(seq 0 $((COUNT - 1))); do
        name="${NAMES[$i]}"
        row=$(printf '%s' "${NODES_JSON}" | python3 -c "
import json,sys
try:
    d=json.load(sys.stdin)
    n=[x for x in d.get('nodes',[]) if x.get('id')=='${name}']
    if not n: print('${name}|?|?|?|?|?|MISSING'); sys.exit()
    x=n[0]
    print(f\"{x['id']}|{x.get('type','?')}|{x.get('country','?')}|{x['host']}:{x['port']}|{x.get('load',0):.2f}|{x.get('latencyMs',0)}|registered\")
except Exception:
    print('${name}|?|?|?|?|?|parse-error')
" 2>/dev/null || echo "${name}|?|?|?|?|?|query-failed")
        IFS='|' read -r rname rrole rcc rendpoint rload rlat rstate <<< "${row}"
        printf '  %-18s %-13s %-6s %-26s %-6s %-8s %s\n' "${rname}" "${rrole}" "${rcc}" "${rendpoint}" "${rload}" "${rlat}" "${rstate}"
    done
else
    ae_warn "coord unreachable — per-node logs: ${LOGDIR}/<name>.log"
fi

echo ""
if [[ ${FAILED} -gt 0 ]]; then
    ae_fail "${FAILED}/${COUNT} node(s) failed — inspect ${LOGDIR}/"
else
    ae_info "all ${COUNT} node(s) deployed."
    ae_info "issue client peers next:  deploy/peer.sh add <node> <client-name>"
fi
exit "${FAILED}"
