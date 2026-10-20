import { useMemo, useState } from "react";
import { Badge, NODE_TYPE_LABEL } from "./ui/Badge";
import { LoadBar } from "./ui/LoadBar";
import { Skeleton } from "./ui/Skeleton";
import { countryFlag, countryName } from "../lib/flags";
import { formatLatency } from "../lib/format";
import type { Settings, Status, VpnNode } from "../types";

interface Props {
  nodes: VpnNode[] | null;
  error: string | null;
  loading: boolean;
  settings: Settings;
  status: Status;
  busy: boolean;
  onConnect: (nodeId: string, relayNode?: string) => void;
  onRetry: () => void;
}

export function ServersView({
  nodes,
  error,
  loading,
  settings,
  status,
  busy,
  onConnect,
  onRetry,
}: Props) {
  const [query, setQuery] = useState("");
  const [chainEntry, setChainEntry] = useState("");
  const [chainExit, setChainExit] = useState("");

  const visible = useMemo(() => {
    if (!nodes) return [];
    const q = query.trim().toLowerCase();
    return nodes.filter((n) => {
      if (settings.obfuscatedOnly && n.type !== "obfuscated") return false;
      if (!q) return true;
      return (
        n.id.toLowerCase().includes(q) ||
        n.city.toLowerCase().includes(q) ||
        n.country.toLowerCase().includes(q) ||
        countryName(n.country).toLowerCase().includes(q)
      );
    });
  }, [nodes, query, settings.obfuscatedOnly]);

  const grouped = useMemo(() => {
    const byCountry = new Map<string, VpnNode[]>();
    for (const n of visible) {
      const list = byCountry.get(n.country);
      if (list) list.push(n);
      else byCountry.set(n.country, [n]);
    }
    return Array.from(byCountry.entries()).sort((a, b) =>
      countryName(a[0]).localeCompare(countryName(b[0])),
    );
  }, [visible]);

  // Nodes eligible as Double VPN hops (registry type "double" or plain relays).
  const chainNodes = useMemo(
    () =>
      (nodes ?? []).filter(
        (n) => n.type === "double" || n.type === "standard" || n.type === "dedicated",
      ),
    [nodes],
  );

  const isCurrent = (n: VpnNode) =>
    status.state !== "disconnected" && status.node === n.id;

  const selectClass =
    "min-w-44 rounded-lg border border-white/10 bg-night-800 px-3 py-2 text-sm text-mist-300 outline-none transition-colors focus:border-accent-500/60";

  return (
    <div className="view-enter mx-auto flex max-w-5xl flex-col gap-6">
      {/* Double VPN chain picker */}
      <section className="card p-5">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h2 className="text-sm font-semibold text-mist-300">
              Double VPN chain
            </h2>
            <p className="mt-1 text-xs text-mist-400">
              Route through two hops: traffic is encrypted twice — the entry
              node never sees your destination.
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <select
              value={chainEntry}
              onChange={(e) => setChainEntry(e.target.value)}
              className={selectClass}
              aria-label="Entry node"
            >
              <option value="">Entry node…</option>
              {chainNodes.map((n) => (
                <option key={n.id} value={n.id}>
                  {countryFlag(n.country)} {n.city} ({n.id})
                </option>
              ))}
            </select>
            <span className="text-mist-400" aria-hidden="true">
              →
            </span>
            <select
              value={chainExit}
              onChange={(e) => setChainExit(e.target.value)}
              className={selectClass}
              aria-label="Exit node"
            >
              <option value="">Exit node…</option>
              {chainNodes.map((n) => (
                <option key={n.id} value={n.id}>
                  {countryFlag(n.country)} {n.city} ({n.id})
                </option>
              ))}
            </select>
            <button
              type="button"
              disabled={busy || !chainEntry || !chainExit || chainEntry === chainExit}
              onClick={() => onConnect(chainEntry, chainExit)}
              className="rounded-lg bg-accent-500 px-4 py-2 text-sm font-semibold text-night-950 transition-colors hover:bg-accent-400 disabled:cursor-not-allowed disabled:opacity-40"
            >
              Connect chain
            </button>
          </div>
        </div>
      </section>

      {/* Search + filters */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="relative min-w-64 flex-1">
          <svg
            className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-mist-400"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            aria-hidden="true"
          >
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-3.5-3.5" />
          </svg>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search country, city or node id…"
            className="w-full rounded-xl border border-white/10 bg-night-800 py-2.5 pl-9 pr-3 text-sm text-mist-300 outline-none transition-colors placeholder:text-mist-400/60 focus:border-accent-500/60"
          />
        </div>
        {settings.obfuscatedOnly && (
          <span className="rounded-full border border-accent-500/30 bg-accent-500/10 px-3 py-1.5 text-xs font-medium text-accent-300">
            Obfuscated only
          </span>
        )}
        <button
          type="button"
          onClick={onRetry}
          className="rounded-xl border border-white/10 bg-night-800 px-3 py-2.5 text-xs text-mist-400 transition-colors hover:text-mist-300"
        >
          Refresh
        </button>
      </div>

      {error && (
        <div className="card border-danger-500/30 bg-danger-500/10 p-4 text-sm text-danger-500">
          {error}{" "}
          <button
            type="button"
            onClick={onRetry}
            className="ml-2 font-semibold underline underline-offset-2"
          >
            Retry
          </button>
        </div>
      )}

      {loading && !nodes && (
        <div className="flex flex-col gap-3">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      )}

      {!loading && nodes && grouped.length === 0 && !error && (
        <div className="card p-10 text-center text-sm text-mist-400">
          No servers match {query ? <>“{query}”</> : "the current filters"}.
        </div>
      )}

      {grouped.map(([cc, list]) => (
        <section key={cc}>
          <header className="mb-2 flex items-center gap-2 px-1">
            <span className="text-lg leading-none" aria-hidden="true">
              {countryFlag(cc)}
            </span>
            <h3 className="text-sm font-semibold text-mist-300">
              {countryName(cc)}
            </h3>
            <span className="text-xs text-mist-400">
              · {list.length} {list.length === 1 ? "node" : "nodes"}
            </span>
          </header>
          <div className="card divide-y divide-white/5 overflow-hidden">
            {list.map((n) => (
              <div
                key={n.id}
                className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 transition-colors hover:bg-white/[0.03]"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium text-mist-300">
                      {n.city}
                    </span>
                    {n.type !== "standard" && (
                      <Badge kind={n.type}>{NODE_TYPE_LABEL[n.type]}</Badge>
                    )}
                  </div>
                  <div className="mt-0.5 truncate text-xs text-mist-400">
                    {n.id} · {n.host}
                  </div>
                </div>
                <LoadBar load={n.load} />
                <span className="w-16 text-right text-xs tabular-nums text-mist-400">
                  {formatLatency(n.latencyMs)}
                </span>
                {isCurrent(n) ? (
                  <span className="w-24 rounded-lg border border-accent-500/40 bg-accent-500/10 px-3 py-1.5 text-center text-xs font-semibold text-accent-300">
                    {status.state === "connected" ? "Connected" : status.state}
                  </span>
                ) : (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => onConnect(n.id)}
                    className="w-24 rounded-lg border border-white/10 bg-night-700 px-3 py-1.5 text-center text-xs font-semibold text-mist-300 transition-colors hover:border-accent-500/50 hover:text-accent-300 disabled:cursor-not-allowed disabled:opacity-40"
                  >
                    Connect
                  </button>
                )}
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
