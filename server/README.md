# AegisVPN Server Infrastructure

Deployable, real WireGuard infrastructure for the AegisVPN clients.

## Topology

```
                       Internet
                          |
        +-----------------+------------------+
        |                 |                  |
  node-entry:51820   obfsd:443 (masked)   coord:8080 (HTTP API)
        |                 |
        |             node-entry (same WG)
        v
     relay (WG iface A: 10.8.0.0/24  <->  WG iface B: 10.8.1.0/24)
        |
        v
     node-exit:51821   --->   threat-dns:53   smartplay:5353   tor:9050
```

* **Single hop** — client peers with `node-entry`, traffic NATs out.
* **Double VPN** — client peers with `relay` (entry side), the relay is also
  a WireGuard *peer* of `node-exit`; packets are encapsulated twice.
  The client's config carries both hops (engine `buildDoubleHop`).
* **Onion over VPN** — connect to the exit, then use Tor's SOCKS port at
  `10.8.2.53:9050` (routable inside the exit subnet).
* **P2P nodes** — `node-p2p` template enables inbound port forwarding
  (docker `network_mode: host` + no egress filtering) for torrents.
* **Dedicated IP** — a peer entry with a fixed `/32` from the dedicated
  pool (10.8.3.0/24) and no other peers on that port.
* **Obfuscated servers** — `obfsd` fronts WireGuard with the XOR-BLAKE2s
  mask; clients enable `AegisObfs` + passphrase (engine `internal/obfs`).
* **Threat Protection** — `threat-dns` blocklists ads/trackers/malware for
  all clients whose DNS points at it (10.8.0.1 inside the tunnel).
* **SmartPlay** — `smartdns` routes streaming domains to optimized resolvers.

## Quick start (fresh Ubuntu 22.04+/Debian 12+)

```bash
sudo ./scripts/server-provision.sh entry.example.com
```

The script installs docker, generates real WireGuard keypairs into
`server/wg-configs/`, templatizes this compose stack, brings it up and
registers the nodes with the coordination API.

## Manual deploy

```bash
cd server
cp .env.example .env && $EDITOR .env
docker compose up -d
curl -s localhost:8080/v1/nodes | jq
```

## Coordination API

| Endpoint | Method | Description |
|---|---|---|
| `/v1/nodes` | GET | node list (registry + measured load/latency) |
| `/v1/nodes/register` | POST | node self-registration (Bearer `COORD_TOKEN`) |
| `/v1/mesh/register` | POST | Meshnet device registration |
| `/v1/mesh/peers` | GET | Meshnet peer list |
| `/v1/mesh/invite` | POST | Meshnet peer invite |
| `/healthz` | GET | liveness |

## Key management

No private keys are ever committed. `wg-configs/*.example` are templates;
`server-provision.sh` generates real keys with `wg genkey`, writes them to
`wg-configs/<node>/` (chmod 700, git-ignored) and prints the client
profile. Rotate with `provision --rotate`.

## Node types reference

| Type | Port | Notes |
|---|---|---|
| standard | 51820 | general purpose, no logs |
| p2p | 51822 | inbound ports allowed, torrent-friendly |
| double | 51820 | relay entry for multi-hop chains |
| onion | 51821 | exit with Tor SOCKS reachable |
| obfuscated | 443/udp | behind obfsd, pass `AegisObfsKey` |
| dedicated | 51823 | static /32 per customer |
