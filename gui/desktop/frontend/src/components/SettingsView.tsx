import { useEffect, useState, type ReactNode } from "react";
import { Toggle } from "./ui/Toggle";
import type { Settings } from "../types";

interface Props {
  settings: Settings;
  onPatch: (patch: Partial<Settings>) => void;
  onToggleKillSwitch: (v: boolean) => void;
  onToggleThreatProtection: (v: boolean) => void;
  onApplySplitTunnel: (exclude: string[]) => void;
  onPickApp: () => void;
  onRemoveApp: (path: string) => void;
}

/** Text input that only commits its value on blur / Enter (avoids a settings
 *  write per keystroke). */
function CommitInput({
  value,
  onCommit,
  placeholder,
  type = "text",
}: {
  value: string;
  onCommit: (v: string) => void;
  placeholder?: string;
  type?: string;
}) {
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);

  return (
    <input
      type={type}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={() => {
        if (draft !== value) onCommit(draft);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter") (e.target as HTMLInputElement).blur();
      }}
      placeholder={placeholder}
      className="mt-1.5 w-full rounded-lg border border-white/10 bg-night-800 px-3 py-2 text-sm text-mist-300 outline-none transition-colors placeholder:text-mist-400/50 focus:border-accent-500/60"
    />
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="card p-5">
      <h2 className="mb-2 text-sm font-semibold text-mist-300">{title}</h2>
      {children}
    </section>
  );
}

const inputClass =
  "mt-1.5 w-full rounded-lg border border-white/10 bg-night-800 px-3 py-2 text-sm text-mist-300 outline-none transition-colors placeholder:text-mist-400/50 focus:border-accent-500/60";

export function SettingsView({
  settings,
  onPatch,
  onToggleKillSwitch,
  onToggleThreatProtection,
  onApplySplitTunnel,
  onPickApp,
  onRemoveApp,
}: Props) {
  const [cidr, setCidr] = useState("");

  const addCidr = () => {
    const v = cidr.trim();
    if (!v) return;
    onApplySplitTunnel([...settings.splitExcludeCidrs, v]);
    setCidr("");
  };

  return (
    <div className="view-enter mx-auto flex max-w-3xl flex-col gap-6">
      <Section title="Protection">
        <div className="divide-y divide-white/5">
          <Toggle
            checked={settings.killSwitch}
            onChange={onToggleKillSwitch}
            label="Kill Switch"
            description="Instantly block all network traffic if the tunnel drops."
          />
          <Toggle
            checked={settings.threatProtection}
            onChange={onToggleThreatProtection}
            label="Threat Protection"
            description="Block ads, trackers and malware domains at the DNS level."
          />
          <Toggle
            checked={settings.obfuscatedOnly}
            onChange={(v) => onPatch({ obfuscatedOnly: v })}
            label="Obfuscated servers only"
            description="Only show obfuscated servers and wrap the tunnel in the UDP obfuscation layer — for networks where VPNs are restricted."
          />
          <Toggle
            checked={settings.autoConnectUntrustedWiFi}
            onChange={(v) => onPatch({ autoConnectUntrustedWiFi: v })}
            label="Auto-connect on untrusted Wi-Fi"
            description="Connect to the last used server whenever the engine detects an untrusted network."
          />
        </div>
      </Section>

      <Section title="Split tunneling">
        <p className="mb-3 text-xs text-mist-400">
          Applications below are remembered for per-app exclusion. Excluded IP
          ranges are applied to the live tunnel immediately.
        </p>
        <div className="flex flex-col gap-2">
          {settings.splitApps.length === 0 && (
            <div className="rounded-lg border border-dashed border-white/10 px-4 py-6 text-center text-xs text-mist-400">
              No applications excluded yet.
            </div>
          )}
          {settings.splitApps.map((app) => (
            <div
              key={app}
              className="flex items-center justify-between gap-3 rounded-lg bg-night-800/70 px-3 py-2"
            >
              <span className="truncate text-xs text-mist-300">{app}</span>
              <button
                type="button"
                onClick={() => onRemoveApp(app)}
                className="shrink-0 text-xs font-medium text-danger-500 transition-opacity hover:opacity-80"
              >
                Remove
              </button>
            </div>
          ))}
        </div>
        <button
          type="button"
          onClick={onPickApp}
          className="mt-3 rounded-lg border border-white/10 bg-night-700 px-4 py-2 text-sm font-medium text-mist-300 transition-colors hover:border-accent-500/50 hover:text-accent-300"
        >
          + Add application
        </button>

        <div className="mt-5">
          <div className="mb-2 text-xs font-semibold uppercase tracking-wider text-mist-400">
            Excluded IP ranges (CIDR)
          </div>
          <div className="flex gap-2">
            <input
              value={cidr}
              onChange={(e) => setCidr(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") addCidr();
              }}
              placeholder="e.g. 192.168.1.0/24"
              className={inputClass}
            />
            <button
              type="button"
              onClick={addCidr}
              disabled={!cidr.trim()}
              className="mt-1.5 shrink-0 rounded-lg bg-accent-500 px-4 text-sm font-semibold text-night-950 transition-colors hover:bg-accent-400 disabled:cursor-not-allowed disabled:opacity-40"
            >
              Add
            </button>
          </div>
          {settings.splitExcludeCidrs.length > 0 && (
            <div className="mt-3 flex flex-wrap gap-2">
              {settings.splitExcludeCidrs.map((c) => (
                <span
                  key={c}
                  className="inline-flex items-center gap-1.5 rounded-full bg-night-700 px-3 py-1 text-xs text-mist-300"
                >
                  {c}
                  <button
                    type="button"
                    aria-label={`Remove ${c}`}
                    onClick={() =>
                      onApplySplitTunnel(
                        settings.splitExcludeCidrs.filter((x) => x !== c),
                      )
                    }
                    className="text-mist-400 transition-colors hover:text-danger-500"
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          )}
        </div>
      </Section>

      <Section title="DNS & network">
        <label className="block">
          <span className="text-xs font-semibold uppercase tracking-wider text-mist-400">
            DNS override
          </span>
          <CommitInput
            value={settings.dnsOverride}
            onCommit={(v) => onPatch({ dnsOverride: v })}
            placeholder="e.g. 10.8.0.1, 1.1.1.1"
          />
          <span className="mt-1 block text-[11px] text-mist-400">
            Applied system-wide while connected (ApplyDNS). Empty = use the
            tunnel's DNS.
          </span>
        </label>
      </Section>

      <Section title="Advanced">
        <label className="block">
          <span className="text-xs font-semibold uppercase tracking-wider text-mist-400">
            Coordination server
          </span>
          <CommitInput
            value={settings.coordUrl}
            onCommit={(v) => onPatch({ coordUrl: v })}
            placeholder="https://coord.example.com"
          />
          <span className="mt-1 block text-[11px] text-mist-400">
            Source of the node registry (/v1/nodes) and breach lookups.
          </span>
        </label>
        <label className="mt-4 block">
          <span className="text-xs font-semibold uppercase tracking-wider text-mist-400">
            Obfuscation passphrase
          </span>
          <CommitInput
            type="password"
            value={settings.obfsKey}
            onCommit={(v) => onPatch({ obfsKey: v })}
            placeholder="AegisObfsKey shared with the nodes"
          />
          <span className="mt-1 block text-[11px] text-mist-400">
            Must match the node's AegisObfsKey. Auto-generated on the first
            obfuscated connect if left empty.
          </span>
        </label>
      </Section>
    </div>
  );
}
