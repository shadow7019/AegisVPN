import type { VpnState } from "../types";

function PowerIcon({ className }: { className?: string }) {
  return (
    <svg
      width="30"
      height="30"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      className={className}
      aria-hidden="true"
    >
      <path d="M12 3v8" />
      <path d="M6.3 6.5a8 8 0 1 0 11.4 0" />
    </svg>
  );
}

/**
 * NordVPN-style oversized circular connect button with an animated teal glow
 * ring while the tunnel is connected.
 */
export function ConnectButton({
  state,
  busy,
  onClick,
}: {
  state: VpnState;
  busy: boolean;
  onClick: () => void;
}) {
  const connected = state === "connected";
  const transitioning =
    state === "connecting" || state === "disconnecting" || busy;

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={transitioning}
      className={`group relative flex h-60 w-60 items-center justify-center rounded-full transition-transform duration-200 ${
        connected ? "ring-glow" : ""
      } ${transitioning ? "cursor-wait" : "hover:scale-[1.02] active:scale-[0.99]"}`}
      aria-label={connected ? "Disconnect" : "Quick connect"}
    >
      <span
        className={`absolute inset-0 rounded-full border ${
          connected
            ? "border-accent-500/60 bg-accent-500/10"
            : "border-night-600 bg-night-800"
        }`}
      />
      {transitioning && (
        <span className="absolute inset-0 animate-spin rounded-full border-2 border-accent-500/30 border-t-accent-500" />
      )}
      <span className="relative z-10 flex flex-col items-center gap-2 px-10 text-center">
        <PowerIcon
          className={
            connected
              ? "text-accent-400"
              : "text-mist-400 transition-colors group-hover:text-mist-300"
          }
        />
        <span className="text-base font-semibold tracking-wide text-mist-300">
          {connected
            ? "CONNECTED"
            : state === "connecting"
              ? "CONNECTING"
              : state === "disconnecting"
                ? "DISCONNECTING"
                : "QUICK CONNECT"}
        </span>
        <span className="text-xs text-mist-400">
          {connected ? "Click to disconnect" : "Secured tunnel · WireGuard"}
        </span>
      </span>
    </button>
  );
}
