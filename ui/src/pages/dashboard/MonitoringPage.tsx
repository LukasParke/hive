import { useEffect, useState } from "react";
import { api, type ItemMap } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";

export function MonitoringPage() {
  const { session } = useAuth();
  const { dashboard } = useAppData();
  const [metrics, setMetrics] = useState<ItemMap | null>(null);

  useEffect(() => {
    if (!session) return;
    const load = () => api.getMetrics(session).then(setMetrics).catch(() => {});
    load();
    const interval = setInterval(load, 30000);
    return () => clearInterval(interval);
  }, [session]);

  function bar(label: string, pct: number) {
    return (
      <div style={{ marginBottom: 12 }}>
        <div style={{ display: "flex", justifyContent: "space-between", fontSize: 14, marginBottom: 4 }}>
          <span>{label}</span>
          <span>{pct}%</span>
        </div>
        <div style={{ height: 8, background: "var(--border-primary)", borderRadius: 4 }}>
          <div style={{ height: 8, borderRadius: 4, width: `${pct}%`, background: pct > 80 ? "#dc2626" : pct > 60 ? "#f59e0b" : "var(--gold-500)" }} />
        </div>
      </div>
    );
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <h2 style={{ margin: 0 }}>Monitoring</h2>
      <p style={{ color: "var(--text-secondary)", fontSize: 13, margin: 0 }}>Auto-refreshes every 30s</p>

      {metrics && (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 12 }}>
          {[
            { label: "Projects", key: "projects" },
            { label: "Applications", key: "applications" },
            { label: "Build Queue", key: "buildQueue" },
            { label: "Services", key: "services" },
            { label: "Nodes", key: "nodes" },
          ].map((item) => (
            <div key={item.key} style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, textAlign: "center", background: "var(--bg-secondary)" }}>
              <div style={{ fontSize: 28, fontWeight: 700 }}>{String(metrics[item.key] ?? 0)}</div>
              <div style={{ color: "var(--text-secondary)", fontSize: 13 }}>{item.label}</div>
            </div>
          ))}
        </div>
      )}

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <h3 style={{ margin: "0 0 12px" }}>Node Breakdown</h3>
        {dashboard.nodes.length === 0 ? (
          <p style={{ color: "var(--text-secondary)" }}>No nodes available.</p>
        ) : (
          dashboard.nodes.map((node, i) => (
            <div key={String(node.id ?? i)} style={{ borderBottom: "1px solid var(--border-primary)", padding: "8px 0" }}>
              <div style={{ fontWeight: 600, fontSize: 14, marginBottom: 4 }}>{String(node.hostname ?? node.id ?? "Node")}</div>
              {bar("CPU", Number(node.cpuPercent ?? 0))}
              {bar("Memory", Number(node.memoryPercent ?? 0))}
            </div>
          ))
        )}
      </section>
    </div>
  );
}
