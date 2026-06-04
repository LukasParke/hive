const badgeMap: Record<string, string> = {
  running: "badge-success",
  complete: "badge-success",
  active: "badge-success",
  ready: "badge-success",
  healthy: "badge-success",
  failed: "badge-error",
  error: "badge-error",
  down: "badge-error",
  cancelled: "badge-warning",
  stopped: "badge-neutral",
  queued: "badge-info",
  building: "badge-info",
  pushing: "badge-info",
  deploying: "badge-deploying",
};

export function StatusBadge({ status }: { status?: string }) {
  const s = (status ?? "unknown").toLowerCase();
  const cls = badgeMap[s] ?? "badge-neutral";
  return <span className={`badge ${cls}`}>{s}</span>;
}
