/** Formatting helpers for the dashboard stats. */

/** Speed given in MB/s. */
export function formatSpeed(mbs: number): string {
  if (!Number.isFinite(mbs) || mbs <= 0) return "0.00";
  if (mbs >= 100) return mbs.toFixed(0);
  if (mbs >= 10) return mbs.toFixed(1);
  return mbs.toFixed(2);
}

export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${pad(h)}:${pad(m)}:${pad(sec)}` : `${pad(m)}:${pad(sec)}`;
}

export function formatBytes(bytes: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  const digits = v >= 100 || i === 0 ? 0 : 1;
  return `${v.toFixed(digits)} ${units[i]}`;
}

export function formatLatency(ms?: number): string {
  return ms == null ? "—" : `${ms} ms`;
}
