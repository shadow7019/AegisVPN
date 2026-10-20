import { useState } from "react";
import { ipc, errMsg } from "../lib/ipc";
import { Skeleton } from "./ui/Skeleton";
import type { BreachReport } from "../types";

function ShieldOk() {
  return (
    <svg width="34" height="34" viewBox="0 0 64 64" fill="none" aria-hidden="true">
      <path
        d="M32 3.5 56 13v18.2C56 46.6 45.8 56.7 32 60.5 18.2 56.7 8 46.6 8 31.2V13Z"
        stroke="#35c98e"
        strokeWidth="3.5"
        strokeLinejoin="round"
      />
      <path
        d="m22 32.5 7 7 14-15"
        stroke="#35c98e"
        strokeWidth="5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function ShieldAlert() {
  return (
    <svg width="34" height="34" viewBox="0 0 64 64" fill="none" aria-hidden="true">
      <path
        d="M32 3.5 56 13v18.2C56 46.6 45.8 56.7 32 60.5 18.2 56.7 8 46.6 8 31.2V13Z"
        stroke="#ef5b69"
        strokeWidth="3.5"
        strokeLinejoin="round"
      />
      <path d="M32 20v14" stroke="#ef5b69" strokeWidth="5" strokeLinecap="round" />
      <circle cx="32" cy="43" r="3" fill="#ef5b69" />
    </svg>
  );
}

export function MonitorView() {
  const [email, setEmail] = useState("");
  const [loading, setLoading] = useState(false);
  const [report, setReport] = useState<BreachReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  const scan = async () => {
    if (!email.trim() || loading) return;
    setError(null);
    setLoading(true);
    setReport(null);
    try {
      setReport(await ipc.checkBreaches(email.trim()));
    } catch (e) {
      setError(errMsg(e));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="view-enter mx-auto flex max-w-3xl flex-col gap-6">
      <section className="card p-6">
        <h2 className="text-sm font-semibold text-mist-300">Dark Web Monitor</h2>
        <p className="mt-1 text-xs text-mist-400">
          Check whether your email address appears in known data breaches. The
          lookup runs against the AegisVPN breach intelligence index.
        </p>
        <div className="mt-4 flex gap-2">
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void scan();
            }}
            placeholder="you@example.com"
            className="flex-1 rounded-lg border border-white/10 bg-night-800 px-3 py-2.5 text-sm text-mist-300 outline-none transition-colors placeholder:text-mist-400/50 focus:border-accent-500/60"
          />
          <button
            type="button"
            onClick={() => void scan()}
            disabled={loading || !email.trim()}
            className="rounded-lg bg-accent-500 px-5 py-2.5 text-sm font-semibold text-night-950 transition-colors hover:bg-accent-400 disabled:cursor-not-allowed disabled:opacity-40"
          >
            {loading ? "Scanning…" : "Scan now"}
          </button>
        </div>
        {error && <div className="mt-3 text-sm text-danger-500">{error}</div>}
      </section>

      {loading && (
        <div className="flex flex-col gap-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-24 w-full" />
          ))}
        </div>
      )}

      {report && report.breaches.length === 0 && (
        <section className="card flex items-center gap-4 border-ok-500/30 bg-ok-500/10 p-6">
          <ShieldOk />
          <div>
            <div className="text-sm font-semibold text-ok-500">All clear</div>
            <div className="mt-0.5 text-xs text-mist-400">
              No known breaches found for {report.email}.
            </div>
          </div>
        </section>
      )}

      {report && report.breaches.length > 0 && (
        <>
          <section className="card flex items-center gap-4 border-danger-500/30 bg-danger-500/10 p-6">
            <ShieldAlert />
            <div>
              <div className="text-sm font-semibold text-danger-500">
                {report.count} breach{report.count === 1 ? "" : "es"} found
              </div>
              <div className="mt-0.5 text-xs text-mist-400">
                Involving {report.email}. Rotate passwords and enable two-factor
                authentication for the affected services.
              </div>
            </div>
          </section>

          {report.breaches.map((b, i) => (
            <section
              key={`${b.source}-${i}`}
              className="card border-danger-500/25 p-5"
            >
              <div className="flex items-center justify-between gap-3">
                <h3 className="text-sm font-semibold text-mist-300">
                  {b.source}
                </h3>
                <span className="text-xs text-mist-400">{b.date}</span>
              </div>
              {b.description && (
                <p className="mt-2 text-xs leading-relaxed text-mist-400">
                  {b.description}
                </p>
              )}
              <div className="mt-3 flex flex-wrap gap-1.5">
                {b.dataClasses.map((dc) => (
                  <span
                    key={dc}
                    className="rounded-full border border-danger-500/30 bg-danger-500/10 px-2.5 py-0.5 text-[11px] font-medium text-danger-500"
                  >
                    {dc}
                  </span>
                ))}
              </div>
            </section>
          ))}
        </>
      )}
    </div>
  );
}
