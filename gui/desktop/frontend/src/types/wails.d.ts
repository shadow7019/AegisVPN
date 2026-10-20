import type {
  BreachReport,
  ConnectOptions,
  Settings,
  Status,
  VpnNode,
} from "../types";

/**
 * Global typings for the Wails v2 bridge objects injected into the webview.
 *
 * `window.go.main.App` mirrors the Go methods bound in main.go (`Bind: …`),
 * `window.runtime` is the Wails runtime. Declaring them here keeps the app
 * independent of the generated `wailsjs/` bindings folder.
 */
declare global {
  interface Window {
    go: {
      main: {
        App: {
          StartUp(): Promise<void>;
          Connect(nodeID: string, opts: ConnectOptions): Promise<Status>;
          Disconnect(): Promise<Status>;
          GetStatus(): Promise<Status>;
          GetNodes(): Promise<VpnNode[]>;
          SetKillSwitch(enabled: boolean): Promise<boolean>;
          SetThreatProtection(enabled: boolean): Promise<boolean>;
          SetSplitTunnel(exclude: string[]): Promise<boolean>;
          CheckBreaches(email: string): Promise<BreachReport>;
          GetSettings(): Promise<Settings>;
          SetSettings(settings: Settings): Promise<Settings>;
          PickAppFile(): Promise<string>;
        };
      };
    };
    runtime: {
      EventsOn(name: string, callback: (data?: unknown) => void): void;
      EventsOff(name: string): void;
      WindowMinimise(): void;
      WindowToggleMaximise(): void;
      Quit(): void;
    };
  }
}

export {};
