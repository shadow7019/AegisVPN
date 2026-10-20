// Shared types mirroring the Go backend (docs/PROTOCOL.md §1 + app.go).

export type VpnState = "disconnected" | "connecting" | "connected" | "disconnecting";

/** Serialized aegis.Status (JSON tags defined in docs/PROTOCOL.md §1). */
export interface Status {
  state: VpnState;
  profile?: string;
  node?: string;
  localIp?: string;
  endpoint?: string;
  hops: number;
  killSwitch: boolean;
  threatProtection: boolean;
  rxBytes: number;
  txBytes: number;
  handshakeAt: number;
  latencyMs?: number;
  error?: string;
}

/** Per-connection toggles (app.go ConnectOptions). */
export interface ConnectOptions {
  killSwitch: boolean;
  threatProtection: boolean;
  obfuscate: boolean;
  relayNode?: string;
}

export type NodeType = "standard" | "p2p" | "double" | "onion" | "obfuscated" | "dedicated";

/** Node registry entry (docs/PROTOCOL.md §4). */
export interface VpnNode {
  id: string;
  country: string;
  city: string;
  host: string;
  port: number;
  pubkey: string;
  type: NodeType;
  load: number;
  latencyMs?: number;
}

/** Persisted GUI settings (app.go Settings / settings.json). */
export interface Settings {
  killSwitch: boolean;
  threatProtection: boolean;
  obfuscatedOnly: boolean;
  autoConnectUntrustedWiFi: boolean;
  dnsOverride: string;
  splitApps: string[];
  splitExcludeCidrs: string[];
  coordUrl: string;
  obfsKey: string;
  lastNodeId: string;
}

export interface Breach {
  source: string;
  date: string;
  email?: string;
  dataClasses: string[];
  description?: string;
}

export interface BreachReport {
  email: string;
  count: number;
  breaches: Breach[];
}

export type View = "dashboard" | "servers" | "settings" | "monitor";
