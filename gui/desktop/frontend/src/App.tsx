import { useCallback, useEffect, useRef, useState } from "react";
import { Sidebar } from "./components/Sidebar";
import { TopBar } from "./components/TopBar";
import { Dashboard } from "./components/Dashboard";
import { ServersView } from "./components/ServersView";
import { SettingsView } from "./components/SettingsView";
import { MonitorView } from "./components/MonitorView";
import { Skeleton } from "./components/ui/Skeleton";
import { useVpnStatus } from "./hooks/useVpnStatus";
import { ipc, errMsg } from "./lib/ipc";
import type { ConnectOptions, Settings, View, VpnNode } from "./types";

export default function App() {
  const [view, setView] = useState<View>("dashboard");
  const { status, speeds, bridgeError } = useVpnStatus(1500);

  const [settings, setSettings] = useState<Settings | null>(null);
  const [nodes, setNodes] = useState<VpnNode[] | null>(null);
  const [nodesLoading, setNodesLoading] = useState(true);
  const [nodesError, setNodesError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const noticeTimer = useRef<number | null>(null);

  const notify = useCallback((msg: string) => {
    setNotice(msg);
    if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current);
    noticeTimer.current = window.setTimeout(() => setNotice(null), 4500);
  }, []);

  // Initial settings load.
  useEffect(() => {
    ipc
      .getSettings()
      .then(setSettings)
      .catch((e) => notify(errMsg(e)));
  }, [notify]);

  const refreshNodes = useCallback(async () => {
    setNodesLoading(true);
    setNodesError(null);
    try {
      setNodes(await ipc.getNodes());
    } catch (e) {
      setNodesError(errMsg(e));
    } finally {
      setNodesLoading(false);
    }
  }, []);

  useEffect(() => {
    void refreshNodes();
  }, [refreshNodes]);

  // --- settings mutations ---------------------------------------------------

  const patchSettings = useCallback(
    async (patch: Partial<Settings>) => {
      if (!settings) return;
      const next: Settings = { ...settings, ...patch };
      setSettings(next); // optimistic
      try {
        setSettings(await ipc.setSettings(next));
      } catch (e) {
        notify(errMsg(e));
      }
    },
    [settings, notify],
  );

  const toggleKillSwitch = useCallback(
    async (v: boolean) => {
      try {
        const applied = await ipc.setKillSwitch(v);
        setSettings((s) => (s ? { ...s, killSwitch: applied } : s));
        notify(applied ? "Kill Switch enabled" : "Kill Switch disabled");
      } catch (e) {
        notify(errMsg(e));
      }
    },
    [notify],
  );

  const toggleThreatProtection = useCallback(
    async (v: boolean) => {
      try {
        const applied = await ipc.setThreatProtection(v);
        setSettings((s) => (s ? { ...s, threatProtection: applied } : s));
        notify(
          applied ? "Threat Protection enabled" : "Threat Protection disabled",
        );
      } catch (e) {
        notify(errMsg(e));
      }
    },
    [notify],
  );

  const applySplitTunnel = useCallback(
    async (exclude: string[]) => {
      try {
        await ipc.setSplitTunnel(exclude);
        setSettings((s) => (s ? { ...s, splitExcludeCidrs: exclude } : s));
        notify("Split tunnel updated");
      } catch (e) {
        notify(errMsg(e));
      }
    },
    [notify],
  );

  const pickApp = useCallback(async () => {
    try {
      const path = await ipc.pickAppFile();
      if (path && settings && !settings.splitApps.includes(path)) {
        await patchSettings({ splitApps: [...settings.splitApps, path] });
      }
    } catch (e) {
      notify(errMsg(e));
    }
  }, [settings, patchSettings, notify]);

  const removeApp = useCallback(
    async (path: string) => {
      if (!settings) return;
      await patchSettings({
        splitApps: settings.splitApps.filter((a) => a !== path),
      });
    },
    [settings, patchSettings],
  );

  // --- tunnel controls --------------------------------------------------------

  const connect = useCallback(
    async (nodeId?: string, relayNode?: string) => {
      if (!settings || busy) return;
      const target = nodeId ?? settings.lastNodeId ?? nodes?.[0]?.id;
      if (!target) {
        notify(
          "No server available — check the coordination server URL in Settings.",
        );
        setView("servers");
        return;
      }
      setBusy(true);
      try {
        const node = nodes?.find((n) => n.id === target);
        const opts: ConnectOptions = {
          killSwitch: settings.killSwitch,
          threatProtection: settings.threatProtection,
          obfuscate: settings.obfuscatedOnly || node?.type === "obfuscated",
          relayNode:
            relayNode && relayNode !== target ? relayNode : undefined,
        };
        const s = await ipc.connect(target, opts);
        setSettings((prev) => (prev ? { ...prev, lastNodeId: target } : prev));
        if (s.error) {
          notify(`Connect failed: ${s.error}`);
        } else if (s.state === "connected") {
          notify(opts.relayNode ? `Chain up: ${target} → ${opts.relayNode}` : `Connected to ${target}`);
        } else {
          notify(`Connecting to ${target}…`);
        }
      } catch (e) {
        notify(errMsg(e));
      } finally {
        setBusy(false);
      }
    },
    [settings, nodes, busy, notify],
  );

  const disconnect = useCallback(async () => {
    if (busy) return;
    setBusy(true);
    try {
      await ipc.disconnect();
      notify("Disconnected");
    } catch (e) {
      notify(errMsg(e));
    } finally {
      setBusy(false);
    }
  }, [busy, notify]);

  // --- render -----------------------------------------------------------------

  return (
    <div className="flex h-screen overflow-hidden bg-night-900 text-mist-300">
      <Sidebar
        view={view}
        onNavigate={setView}
        connected={status.state === "connected"}
      />

      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar
          view={view}
          status={status}
          busy={busy}
          bridgeError={bridgeError}
        />

        <main className="min-h-0 flex-1 overflow-y-auto px-6 py-6 lg:px-8">
          {bridgeError && (
            <div className="mb-5 rounded-xl border border-warn-500/30 bg-warn-500/10 px-4 py-3 text-xs text-warn-500">
              Backend bridge unavailable: {bridgeError}
            </div>
          )}

          {!settings ? (
            <div className="mx-auto grid max-w-5xl gap-4 lg:grid-cols-[auto_1fr]">
              <Skeleton className="h-96 w-72 rounded-3xl" />
              <div className="flex flex-col gap-4">
                <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
                  {Array.from({ length: 4 }).map((_, i) => (
                    <Skeleton key={i} className="h-24" />
                  ))}
                </div>
                <Skeleton className="h-40" />
              </div>
            </div>
          ) : view === "dashboard" ? (
            <Dashboard
              status={status}
              speeds={speeds}
              settings={settings}
              nodes={nodes}
              busy={busy}
              onConnect={(nodeId) => void connect(nodeId)}
              onDisconnect={() => void disconnect()}
              onToggleKillSwitch={(v) => void toggleKillSwitch(v)}
              onToggleThreatProtection={(v) => void toggleThreatProtection(v)}
              onGoto={(v) => setView(v)}
            />
          ) : view === "servers" ? (
            <ServersView
              nodes={nodes}
              error={nodesError}
              loading={nodesLoading}
              settings={settings}
              status={status}
              busy={busy}
              onConnect={(nodeId, relayNode) =>
                void connect(nodeId, relayNode)
              }
              onRetry={() => void refreshNodes()}
            />
          ) : view === "settings" ? (
            <SettingsView
              settings={settings}
              onPatch={(p) => void patchSettings(p)}
              onToggleKillSwitch={(v) => void toggleKillSwitch(v)}
              onToggleThreatProtection={(v) => void toggleThreatProtection(v)}
              onApplySplitTunnel={(exclude) => void applySplitTunnel(exclude)}
              onPickApp={() => void pickApp()}
              onRemoveApp={(p) => void removeApp(p)}
            />
          ) : (
            <MonitorView />
          )}
        </main>
      </div>

      {notice && (
        <div className="view-enter fixed bottom-6 right-6 z-50 max-w-sm rounded-xl border border-white/10 bg-night-800 px-4 py-3 text-sm text-mist-300 shadow-2xl">
          {notice}
        </div>
      )}
    </div>
  );
}
