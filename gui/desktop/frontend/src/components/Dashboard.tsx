import { useEffect, useState } from "react";
import { ConnectButton } from "./ConnectButton";
import { Badge } from "./ui/Badge";
import { LoadBar } from "./ui/LoadBar";
import { Skeleton } from "./ui/Skeleton";
import { Toggle } from "./ui/Toggle";
import { countryFlag, countryName } from "../lib/flags";
import { formatBytes, formatDuration, formatLatency, formatSpeed } from "../lib/format";
import type { Speeds } from "../hooks/useVpnStatus";
import type { Settings, Status, VpnNode } from "../types";

interface Props {
  status: Status;
  speeds: Speeds;
  settings: Settings;
  nodes: VpnNode[] | null;
  busy: boolean;
  onConnect: (nodeId?: string) => void;
  onDisconnect: () => void;
  onToggleKillSwitch: (v: boolean) => void;
  onToggleThreatProtection: (v: boolean) => void;
  onGoto: (view: "servers" | "settings" | "monitor") => void;
}

function StatTile({
  label,
  value,
  sub,
}: {
  label: string;
  value: string;
  sub?: string;
}) {
  return (
    <div className="card p-4">
      <div className="text-[11px] font-medium uppercase tracking-wider text-mist-400">
        {label}
      </div>
      <div className="mt-1 text-2xl font-semibold tabular-nums text-mist-300">
        {value}
      </div>
      {sub && <div className="mt-0.5 text-xs text-mist-400">{sub}</div>}
    </div>
  );
}

export function Dashboard({
  status,
  speeds,
  settings,
  nodes,
  busy,
  onConnect,
  onDisconnect,
  onToggleKillSwitch,
  onToggleThreatProtection,
  onGoto,
}: Props) {
  const connected = status.state === "connected";
  const currentNode = status.node
    ? (nodes?.find((n) => n.id === status.node) ?? null)
    : null;

  // Session timer: starts when the state first flips to "connected".
  const [connectedAt, setConnectedAt] = useState<number | null>(null);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (status.state === "connected") {
      setConnectedAt((prev) => prev ?? Date.now());
    } else {
      setConnectedAt(null);
    }
  }, [status.state]);

  useEffect(() => {
    if (connectedAt == null) return;
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [connectedAt]);

  const sessionSeconds =
    connectedAt != null ? Math.max(0, Math.floor((now - connectedAt) / 1000)) : 0;

  return (
    <div className="view-enter mx-auto grid max-w-5xl gap-6 lg:grid-cols-[auto_1fr]">
      {/* Connect column */}
      <section className="card flex flex-col items-center gap-4 self-start p-8">
        <ConnectButton
          state={status.state}
          busy={busy}
          onClick={() => (connected ? onDisconnect() : onConnect())}
        />
        <div className="text-center">
          <div className="text-sm font-medium text-mist-300">
            {currentNode
              ? `${countryFlag(currentNode.country)}  ${currentNode.city}, ${countryName(currentNode.country)}`
              : settings.lastNodeId
                ? settings.lastNodeId
                : "No server selected"}
          </div>
          <div className="mt-1 text-xs text-mist-400">
            {connected
              ? `Session ${formatDuration(sessionSeconds)}${status.endpoint ? ` · ${status.endpoint}` : ""}`
              : "Quick connect or pick a server"}
          </div>
        </div>
        <button
          type="button"
          onClick={() => onGoto("servers")}
          className="text-xs font-medium text-accent-400 underline-offset-4 transition-colors hover:text-accent-300 hover:underline"
        >
          Browse servers →
        </button>
      </section>

      {/* Info column */}
      <section className="flex min-w-0 flex-col gap-6">
        {status.error && (
          <div className="card border-danger-500/40 bg-danger-500/10 p-4 text-sm text-danger-500">
            {status.error}
          </div>
        )}

        <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
          <StatTile label="Download" value={formatSpeed(speeds.down)} sub="MB/s" />
          <StatTile label="Upload" value={formatSpeed(speeds.up)} sub="MB/s" />
          <StatTile
            label="Session"
            value={connected ? formatDuration(sessionSeconds) : "—"}
            sub={connected ? "elapsed" : "idle"}
          />
          <StatTile
            label="Latency"
            value={connected ? formatLatency(status.latencyMs) : "—"}
            sub={connected ? (status.hops === 2 ? "via Double VPN" : "direct") : undefined}
          />
        </div>

        {/* Current node */}
        <div className="card p-5">
          <div className="flex items-center justify-between">
            <h2 className="text-[11px] font-semibold uppercase tracking-wider text-mist-400">
              Current node
            </h2>
            {status.hops === 2 && <Badge kind="double">2 hops</Badge>}
          </div>

          {nodes === null ? (
            <div className="mt-4 flex flex-col gap-2">
              <Skeleton className="h-5 w-44" />
              <Skeleton className="h-4 w-64" />
            </div>
          ) : currentNode ? (
            <div className="mt-3 flex flex-wrap items-center gap-x-6 gap-y-3">
              <div className="flex items-center gap-3">
                <span className="text-3xl leading-none">
                  {countryFlag(currentNode.country)}
                </span>
                <div>
                  <div className="text-base font-semibold text-mist-300">
                    {currentNode.city}, {countryName(currentNode.country)}
                  </div>
                  <div className="text-xs text-mist-400">
                    {currentNode.id} · {currentNode.host}
                  </div>
                </div>
                <Badge kind={currentNode.type} />
              </div>
              <LoadBar load={currentNode.load} />
              <div className="text-xs text-mist-400">
                {status.localIp && (
                  <div>
                    Tunnel IP:{" "}
                    <span className="text-mist-300">{status.localIp}</span>
                  </div>
                )}
                {status.endpoint && (
                  <div>
                    Endpoint:{" "}
                    <span className="text-mist-300">{status.endpoint}</span>
                  </div>
                )}
              </div>
              <div className="text-xs text-mist-400">
                <div>
                  Rx total:{" "}
                  <span className="tabular-nums text-mist-300">
                    {formatBytes(status.rxBytes)}
                  </span>
                </div>
                <div>
                  Tx total:{" "}
                  <span className="tabular-nums text-mist-300">
                    {formatBytes(status.txBytes)}
                  </span>
                </div>
              </div>
            </div>
          ) : (
            <p className="mt-3 text-sm text-mist-400">
              Not connected. Quick connect or pick a server to establish the
              tunnel.
            </p>
          )}
        </div>

        {/* Quick toggles */}
        <div className="card divide-y divide-white/5 px-5 py-2">
          <Toggle
            checked={settings.killSwitch}
            onChange={onToggleKillSwitch}
            label="Kill Switch"
            description="Block all network traffic if the tunnel drops."
          />
          <Toggle
            checked={settings.threatProtection}
            onChange={onToggleThreatProtection}
            label="Threat Protection"
            description="DNS-level ad, tracker and malware blocking."
          />
        </div>
      </section>
    </div>
  );
}
