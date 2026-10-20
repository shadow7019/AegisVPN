# AegisVPN Fleet Deployment Suite

Turns raw Ubuntu 22.04+/Debian 12+ VPS boxes into real WireGuard nodes —
with hardening, firewall, key generation, service orchestration, coordination
registration and client peer management. Pure bash, idempotent, parallel.

```
 operator laptop ──ssh──▶ node A (standard)      WireGuard :51820/udp
                 ──ssh──▶ node B (double-entry)  WireGuard :51820/udp
                 ──ssh──▶ node C (double-exit)   WireGuard :51821/udp   ◀─ chained by B's relay
                 ──ssh──▶ node D (obfs)          :443/udp masked → :51820
                                            │
                                            ▼
                                    coord API :8080  (node registry, meshnet, breaches)
```

## Quickstart (zero → connected client in 3 commands)

```bash
# 1. deploy one standard node (on the VPS itself)
sudo deploy/deploy-node.sh --local --role standard --country DE --city Falkenstein

# 2. issue a client peer (from your laptop)
deploy/peer.sh add --host root@203.0.113.10 my-node alice

# 3. connect with the real engine
aegisvpn connect deploy/peers/my-node/alice.conf -killswitch -threat
```

## Single node

```bash
deploy-node.sh --host root@IP --role <standard|p2p|double-entry|double-exit|obfs> \
               --country US --city "Ashburn" [--name id] [--port 51820] \
               [--coord-host h:p] [--coord-token t] [--obfs-key k] [--skip-register]
```

What it does on the box:

| Step | Detail |
|---|---|
| hardening | `ip_forward`, `src_valid_mark`, BBR + fq, conntrack headroom (`sysctl.d/90-aegisvpn.conf`) |
| firewall | UFW default-deny; allows ssh, the WG port, role extras (53, 5353/udp, 9050) |
| docker | installs docker-ce + compose plugin if missing |
| keys | real WireGuard server keypair; **private key stays on the node**, public key derived there |
| stack | renders `compose.override.yml` filtered to the role's services; `docker compose up -d --build` |
| verify | waits for a live `wg0` inside the container |
| register | `POST /v1/nodes/register` to coord with the real pubkey (409 = already registered = ok) |

## Whole fleet

```bash
cp deploy/fleet.conf.example deploy/fleet.conf   # fill in your hosts
deploy/deploy-fleet.sh                 # parallel rollout + coord summary table
deploy/deploy-fleet.sh --limit de-fra-1    # redeploy one node
deploy/deploy-fleet.sh --dry-run
```

Per-node logs land in `deploy/logs/<name>.log`.

## Roles

| Role | Services | Exposed | Use case |
|---|---|---|---|
| `standard` | node-entry, coord, threat-dns, smartplay, tor | WG :51820, DNS :53, SmartDNS :5353/udp | everyday privacy exit |
| `p2p` | node-entry, coord | WG :51820 | torrent-friendly (forward 6881/udp to clients as needed) |
| `double-entry` | node-entry, relay, coord | WG :51820 | first hop of a Double VPN chain |
| `double-exit` | node-exit, tor | WG :51821, Tor SOCKS :9050 | second hop; Tor reachable at `10.8.2.53:9050` (Onion over VPN) |
| `obfs` | node-entry, obfsd | :443/udp (masked) → :51820 | DPI-hostile networks; pair with client `AegisObfs = true` |

## Client peer lifecycle

```bash
deploy/peer.sh add    --host root@IP [--conf entry|exit] [--ip 10.8.0.X] node alice
deploy/peer.sh list   --host root@IP [node]
deploy/peer.sh show   node alice
deploy/peer.sh revoke node alice
```

`add` generates a real client keypair + preshared key, allocates the next free
tunnel IP, appends a `# AegisPeer: <name>`-tagged block to the node's
`wg0.conf`, applies it **live** into the running container (`wg set`), and
writes the ready profile to `deploy/peers/<node>/<client>.conf` (0600).
`revoke` removes the peer live and from config, archiving the profile.

## Health checks

```bash
deploy/verify.sh --host root@IP    # compose states, wg0 live, keys, coord listing
```

## Configuration

Secrets come from the environment (or `deploy/env`, chmod 600 — see
`deploy/env.example`): `AE_COORD_HOST`, `AE_COORD_TOKEN`, `AE_OBFS_KEY`.

Key rotation: re-deploy with `--port` unchanged but wipe
`/opt/aegisvpn/wg-configs/<side>/server.key` first, then re-register; clients
re-issue via `peer.sh`. Rotate `AE_COORD_TOKEN` on the coord server and in
`deploy/env` together.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `wg interface not confirmed` | `docker compose logs node-entry` — usually missing `NET_ADMIN` or kernel module; containers fall back to the internal impl automatically |
| client connects, no internet | MASQUERADE interface name: if egress is not `eth0`, edit `PostUp` in `wg0.conf` (common on some providers: `ens3`, `venet0`) |
| registration 401 | coord token mismatch — coord reads `COORD_TOKEN` from `server/.env` |
| registration 403 | `COORD_TOKEN` unset on coord → registration endpoint disabled by design |
| UDP silent in verify | expected — UDP probes often get no reply; confirm with a real handshake from a client |
| obfs client can't connect | `AegisObfsKey` must equal `AEGIS_OBFS_KEY` on the node |

## Port matrix

| Port/protocol | Role(s) | Purpose |
|---|---|---|
| 22/tcp | all | ssh management |
| 51820/udp | entry roles | WireGuard (entry/standard/p2p/obfs-backing) |
| 51821/udp | double-exit | WireGuard exit |
| 443/udp | obfs | masked WireGuard front |
| 53/udp+tcp | standard | Threat Protection DNS |
| 5353/udp | standard | SmartPlay DNS |
| 9050/tcp | double-exit | Tor SOCKS (Onion over VPN) |
| 8080/tcp | standard/entry/p2p/obfs | coord API (restrict to your IPs in production) |
