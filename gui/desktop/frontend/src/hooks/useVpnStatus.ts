import { useEffect, useRef, useState } from "react";
import { ipc, errMsg } from "../lib/ipc";
import type { Status } from "../types";

export interface Speeds {
  /** MB/s computed from Status.rxBytes deltas. */
  down: number;
  /** MB/s computed from Status.txBytes deltas. */
  up: number;
}

const EMPTY_STATUS: Status = {
  state: "disconnected",
  hops: 0,
  killSwitch: false,
  threatProtection: false,
  rxBytes: 0,
  txBytes: 0,
  handshakeAt: 0,
};

/**
 * Polls GetStatus every `pollMs` (default 1.5s) and derives live transfer
 * rates from the rx/tx byte counters of consecutive samples.
 */
export function useVpnStatus(pollMs = 1500) {
  const [status, setStatus] = useState<Status>(EMPTY_STATUS);
  const [speeds, setSpeeds] = useState<Speeds>({ down: 0, up: 0 });
  const [bridgeError, setBridgeError] = useState<string | null>(null);
  const prev = useRef<{ rx: number; tx: number; at: number } | null>(null);

  useEffect(() => {
    let cancelled = false;

    const tick = async () => {
      try {
        const s = await ipc.getStatus();
        if (cancelled) return;
        setBridgeError(null);
        setStatus(s);

        const now = Date.now();
        if (prev.current && s.state === "connected") {
          const dt = (now - prev.current.at) / 1000;
          if (dt > 0) {
            setSpeeds({
              down: Math.max(0, (s.rxBytes - prev.current.rx) / dt / (1024 * 1024)),
              up: Math.max(0, (s.txBytes - prev.current.tx) / dt / (1024 * 1024)),
            });
          }
        } else {
          setSpeeds({ down: 0, up: 0 });
        }
        prev.current = { rx: s.rxBytes, tx: s.txBytes, at: now };
      } catch (e) {
        if (!cancelled) setBridgeError(errMsg(e));
      }
    };

    void tick();
    const timer = window.setInterval(tick, pollMs);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [pollMs]);

  return { status, speeds, bridgeError };
}
