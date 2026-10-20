import type { ReactNode } from "react";
import { Logo } from "./Logo";
import type { View } from "../types";

function GaugeIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M20.5 15.5a8.5 8.5 0 1 0-17 0" />
      <path d="m12 15 3.5-5.5" />
      <circle cx="12" cy="15" r="1.2" />
    </svg>
  );
}

function GlobeIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18" />
      <path d="M12 3c2.7 2.6 4 5.6 4 9s-1.3 6.4-4 9c-2.7-2.6-4-5.6-4-9s1.3-6.4 4-9z" />
    </svg>
  );
}

function SlidersIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M4 8h10M18 8h2M4 16h2M10 16h10" />
      <circle cx="16" cy="8" r="2" />
      <circle cx="8" cy="16" r="2" />
    </svg>
  );
}

function EyeIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}

const NAV: { id: View; label: string; icon: ReactNode }[] = [
  { id: "dashboard", label: "Dashboard", icon: <GaugeIcon /> },
  { id: "servers", label: "Servers", icon: <GlobeIcon /> },
  { id: "settings", label: "Settings", icon: <SlidersIcon /> },
  { id: "monitor", label: "Dark Web Monitor", icon: <EyeIcon /> },
];

export function Sidebar({
  view,
  onNavigate,
  connected,
}: {
  view: View;
  onNavigate: (v: View) => void;
  connected: boolean;
}) {
  return (
    <aside className="flex w-60 shrink-0 flex-col border-r border-white/5 bg-night-950/70">
      <div className="flex items-center gap-3 px-5 py-5">
        <Logo size={30} />
        <div>
          <div className="text-sm font-bold tracking-wide text-mist-300">
            AegisVPN
          </div>
          <div className="text-[10px] uppercase tracking-[0.2em] text-mist-400">
            WireGuard core
          </div>
        </div>
      </div>

      <nav className="mt-2 flex flex-1 flex-col gap-1 px-3">
        {NAV.map((item) => {
          const active = view === item.id;
          return (
            <button
              key={item.id}
              type="button"
              onClick={() => onNavigate(item.id)}
              className={`relative flex items-center gap-3 rounded-xl px-3.5 py-2.5 text-left text-sm transition-colors duration-150 ${
                active
                  ? "bg-night-800 font-medium text-accent-300"
                  : "text-mist-400 hover:bg-white/[0.04] hover:text-mist-300"
              }`}
            >
              {active && (
                <span className="absolute left-0 top-1/2 h-5 w-0.5 -translate-y-1/2 rounded-full bg-accent-500" />
              )}
              <span className={active ? "text-accent-400" : "text-mist-400"}>
                {item.icon}
              </span>
              {item.label}
            </button>
          );
        })}
      </nav>

      <div className="border-t border-white/5 px-5 py-4">
        <div className="flex items-center gap-2 text-xs">
          <span
            className={`h-2 w-2 rounded-full ${
              connected
                ? "bg-accent-500 shadow-[0_0_8px_rgba(23,199,189,0.8)]"
                : "bg-night-600"
            }`}
          />
          <span className="text-mist-400">
            {connected ? "Tunnel active" : "Tunnel down"}
          </span>
        </div>
        <div className="mt-1.5 text-[10px] text-mist-400/60">
          v1.0.0 · engine-bound
        </div>
      </div>
    </aside>
  );
}
