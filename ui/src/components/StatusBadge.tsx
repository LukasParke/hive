const colors: Record<string, { bg: string; fg: string }> = {
  running: { bg: "#0f2419", fg: "#4ade80" },
  complete: { bg: "#0f2419", fg: "#4ade80" },
  active: { bg: "#0f2419", fg: "#4ade80" },
  ready: { bg: "#0f2419", fg: "#4ade80" },
  healthy: { bg: "#0f2419", fg: "#4ade80" },
  failed: { bg: "#2a1215", fg: "#f87171" },
  error: { bg: "#2a1215", fg: "#f87171" },
  cancelled: { bg: "#2a2008", fg: "#fbbf24" },
  stopped: { bg: "#222220", fg: "#9a9489" },
  queued: { bg: "#0c1a2e", fg: "#60a5fa" },
  building: { bg: "#0c1a2e", fg: "#60a5fa" },
  pushing: { bg: "#0c1a2e", fg: "#60a5fa" },
  deploying: { bg: "#1a1530", fg: "#a78bfa" },
  down: { bg: "#2a1215", fg: "#f87171" },
};

export function StatusBadge({ status }: { status?: string }) {
  const s = (status ?? "unknown").toLowerCase();
  const c = colors[s] ?? { bg: "#222220", fg: "#9a9489" };
  return (
    <span style={{ display: "inline-block", padding: "2px 10px", borderRadius: 9999, fontSize: 12, fontWeight: 600, background: c.bg, color: c.fg }}>
      {s}
    </span>
  );
}
