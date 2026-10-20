import type { ReactNode } from "react";
import type { NodeType } from "../../types";

export const NODE_TYPE_LABEL: Record<NodeType, string> = {
  standard: "Standard",
  p2p: "P2P",
  double: "Double VPN",
  onion: "Onion over VPN",
  obfuscated: "Obfuscated",
  dedicated: "Dedicated IP",
};

const STYLES: Record<NodeType, string> = {
  standard: "bg-white/5 text-mist-400",
  p2p: "bg-ok-500/15 text-ok-500",
  double: "bg-accent-500/15 text-accent-300",
  onion: "bg-warn-500/15 text-warn-500",
  obfuscated: "bg-cyan-400/15 text-cyan-300",
  dedicated: "bg-amber-400/15 text-amber-300",
};

export function Badge({
  kind,
  children,
}: {
  kind: NodeType;
  children?: ReactNode;
}) {
  return (
    <span
      className={`inline-flex items-center whitespace-nowrap rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${
        STYLES[kind]
      }`}
    >
      {children ?? NODE_TYPE_LABEL[kind]}
    </span>
  );
}
