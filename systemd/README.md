# AegisVPN systemd service (Linux)

Runs the real engine as a root daemon: WireGuard TUN, routing + kill-switch
firewall takeover, DNS control, and the JSON-line control API on the unix
socket `/var/lib/aegisvpn/engine.sock` (contract: `docs/PROTOCOL.md` §2).
The CLI (`aegisvpn status`) and any GUI talk to that socket as unprivileged
users — keys stay in the root-owned data dir.

## Install

```bash
# build first (or pass --binary to point at an existing build)
cd engine && go build -o ../dist/aegisvpn-linux-amd64 ./cmd/aegisvpn

sudo systemd/install.sh                       # discovers ../dist binary
sudo systemd/install.sh --binary ./aegisvpn   # explicit path
```

The installer is idempotent — re-running upgrades the binary and restarts
the unit (auto-connect state and keys are preserved in `/var/lib/aegisvpn`).

## Operate

```bash
systemctl status aegisvpn
journalctl -u aegisvpn -f
aegisvpn status            # status JSON via the socket
aegisvpn nodes             # registry listing via the socket
sudo systemctl stop aegisvpn   # graceful: tunnel down + kill-switch reverted
```

## Uninstall

```bash
sudo systemd/uninstall.sh          # keep keys/profiles
sudo systemd/uninstall.sh --purge  # also wipe /var/lib/aegisvpn
```

## Why root?

TUN creation (`/dev/net/tun`), routing-table ownership and the kill switch's
default-deny firewall rules require CAP_NET_ADMIN. The unit applies
systemd hardening (ProtectSystem=strict, restricted syscall surface,
DeviceAllow scoped to the tun device) so root is confined to exactly what
the engine needs.
