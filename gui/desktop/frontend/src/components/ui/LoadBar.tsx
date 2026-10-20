export function LoadBar({ load }: { load: number }) {
  const pct = Math.round(Math.min(1, Math.max(0, load)) * 100);
  const color =
    pct < 60 ? "bg-ok-500" : pct < 85 ? "bg-warn-500" : "bg-danger-500";
  return (
    <div className="flex items-center gap-2" title={`Node load ${pct}%`}>
      <div className="h-1.5 w-24 overflow-hidden rounded-full bg-night-600">
        <div
          className={`h-full rounded-full transition-all duration-500 ${color}`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="w-9 text-right text-[11px] tabular-nums text-mist-400">
        {pct}%
      </span>
    </div>
  );
}
