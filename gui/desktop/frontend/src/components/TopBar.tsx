import type { Status, View } from "../types";

const TITLES: Record<View, string> = {
  dashboard: "Dashboard",
  servers: "Servers",
  settings: "Settings",
  monitor: "Dark Web Monitor",
};

export function TopBar({
  view,
  status,
  busy,
  bridgeError,
}: {
  view: View;
  status: Status;
  busy: boolean;
  bridgeError: string | null;
}) {
  const transitioning =
    status.state === "connecting" || status.state === "disconnecting" || busy;

  const pillClass =
    status.state === "connected"
      ? "border-accent-500/30 bg-accent-500/10 text-accent-300"
      : transitioning
        ? "border-warn-500/30 bg-warn-500/10 text-warn-500"
        : "border-white/5 bg-night-700 text-mist-400";

  const dotClass =
    status.state === "connected"
      ? "bg-accent-500"
      : transitioning
        ? "bg-warn-500"
        : "bg-mist-400/50";

  const label =
    status.state === "connected"
      ? status.hops === 2
        ? "Protected · Double VPN"
        : "Protected"
      : status.state === "connecting"
        ? "Connecting…"
        : status.state === "disconnecting"
          ? "Disconnecting…"
          : "Unprotected";

  return (
    <header className="flex h-14 shrink-0 items-center justify-between border-b border-white/5 bg-night-850/80 px-6">
      <h1 className="text-sm font-semibold tracking-wide text-mist-300">
        {TITLES[view]}
      </h1>
      <div className="flex items-center gap-3">
        {bridgeError && (
          <span className="hidden text-xs text-warn-500 md:inline">
            bridge offline
          </span>
        )}
        <span
          className={`inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-medium ${pillClass}`}
        >
          <span
            className={`h-1.5 w-1.5 rounded-full ${dotClass} ${
              transitioning ? "animate-pulse" : ""
            }`}
          />
          {label}
        </span>
      </div>
    </header>
  );
}
