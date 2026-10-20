import type {
  BreachReport,
  ConnectOptions,
  Settings,
  Status,
  VpnNode,
} from "../types";

/**
 * Typed access to the Wails v2 Go bridge (window.go.main.App, bound in
 * main.go). When the UI runs outside the desktop shell (plain `vite dev` in a
 * browser) this throws instead of inventing state — no fake VPN data, ever.
 */
function bridge(): Window["go"]["main"]["App"] {
  if (typeof window === "undefined" || !window.go?.main?.App) {
    throw new Error(
      "Wails bridge unavailable — run inside the AegisVPN desktop shell (wails dev / wails build).",
    );
  }
  return window.go.main.App;
}

export const ipc = {
  connect: (nodeID: string, opts: ConnectOptions): Promise<Status> =>
    bridge().Connect(nodeID, opts),
  disconnect: (): Promise<Status> => bridge().Disconnect(),
  getStatus: (): Promise<Status> => bridge().GetStatus(),
  getNodes: (): Promise<VpnNode[]> => bridge().GetNodes(),
  setKillSwitch: (enabled: boolean): Promise<boolean> =>
    bridge().SetKillSwitch(enabled),
  setThreatProtection: (enabled: boolean): Promise<boolean> =>
    bridge().SetThreatProtection(enabled),
  setSplitTunnel: (exclude: string[]): Promise<boolean> =>
    bridge().SetSplitTunnel(exclude),
  checkBreaches: (email: string): Promise<BreachReport> =>
    bridge().CheckBreaches(email),
  getSettings: (): Promise<Settings> => bridge().GetSettings(),
  setSettings: (settings: Settings): Promise<Settings> =>
    bridge().SetSettings(settings),
  pickAppFile: (): Promise<string> => bridge().PickAppFile(),
};

/** Normalise any thrown value into a human-readable message. */
export function errMsg(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === "string") return e;
  return String(e);
}
