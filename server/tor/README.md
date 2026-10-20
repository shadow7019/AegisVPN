# Onion over VPN (server/tor)

Tor SOCKS entry point reachable **inside** the VPN tunnel — the
NordVPN "Onion over VPN" pattern: your traffic is encrypted into WireGuard,
then exits through the Tor network. Neither the exit node nor Tor sees both
your IP and your traffic; the VPN provider (you) can't see destinations.

## Chain

```
client ──WireGuard──▶ node (standard / double-exit) ──▶ tor SOCKS ──▶ onion/clearnet
        encrypted                          10.8.2.53:9050 (inside tunnel)
```

The `tor` service in `server/docker-compose.yml` publishes SOCKS on the
host's loopback (`127.0.0.1:9050`); clients reach it through the tunnel at
the node's tunnel address. On a `double-exit` node the Tor SOCKS is reachable
after the second hop — a genuine two-hop + Tor chain.

## Using it from a client

1. Connect normally (`aegisvpn connect -node <exit-node>`).
2. Point applications at the SOCKS proxy:
   * socks5 host `10.8.0.1` (standard node entry subnet) or `10.8.1.1`
     (double-exit subnet), port `9050`;
   * curl: `curl --socks5-hostname 10.8.0.1:9050 https://check.torproject.org`;
   * browser: configure SOCKSv5 proxy + DNS through proxy (avoids DNS leaks).

## Files

* `torrc` — hardening defaults (SOCKS only inside the tunnel, no relay,
  isolated circuits per destination).
* `docker-compose.yml` (repo root `server/`) — the `tor` service
  (`dperson/torproxy`) with `TOR_SocksPort=0.0.0.0:9050`.

## Deploy

```bash
sudo deploy/deploy-node.sh --local --role double-exit --country CH --city Zurich
# tor comes up with the exit node; SOCKS reachable at 10.8.1.1:9050 in-tunnel
```

## Notes

* Tor inside the tunnel is DNS-leak-safe only if DNS also goes through SOCKS
  (`--socks5-hostname`, not plain `--socks5`).
* Hidden-service hosting is out of scope here — this is a client-anonymity path.
* The engine's Threat Protection DNS still applies to non-SOCKS traffic.
