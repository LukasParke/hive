import { useEffect, useState } from "react";
import { useParams, Link } from "react-router-dom";
import { api, type ItemMap } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { LoadingState } from "../../components/LoadingState";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { StatusBadge } from "../../components/StatusBadge";

export function NodeDetailPage() {
  const { id = "" } = useParams();
  const { session } = useAuth();
  const toast = useToast();

  const [metrics, setMetrics] = useState<ItemMap | null>(null);
  const [packages, setPackages] = useState<ItemMap | null>(null);
  const [loading, setLoading] = useState(true);
  const [confirmMaintenance, setConfirmMaintenance] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);

  async function loadData() {
    if (!session || !id) return;
    try {
      const [m, p] = await Promise.all([
        api.getNodeMetrics(session, id).catch(() => null),
        api.getNodePackages(session, id).catch(() => null),
      ]);
      setMetrics(m);
      setPackages(p);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 30000);
    return () => clearInterval(interval);
  }, [id, session]); // eslint-disable-line react-hooks/exhaustive-deps

  if (loading) return <LoadingState />;
  if (!session) return <p>Not authenticated.</p>;

  async function handleCheckUpdates() {
    try {
      await api.triggerPackageCheck(session!, id);
      toast.success("Package check triggered");
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  async function handleMaintenance() {
    setActionLoading(true);
    try {
      await api.triggerNodeMaintenance(session!, id, { operations: ["security_updates"], rebootIfNeeded: false });
      toast.success("Maintenance triggered");
      setConfirmMaintenance(false);
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setActionLoading(false);
    }
  }

  function bar(label: string, used: number, total: number, unit: string) {
    const pct = total > 0 ? Math.round((used / total) * 100) : 0;
    return (
      <div style={{ marginBottom: 12 }}>
        <div style={{ display: "flex", justifyContent: "space-between", fontSize: 14, marginBottom: 4 }}>
          <span>{label}</span>
          <span>{pct}% ({used.toFixed(1)} / {total.toFixed(1)} {unit})</span>
        </div>
        <div style={{ height: 8, background: "var(--border-primary)", borderRadius: 4 }}>
          <div style={{ height: 8, borderRadius: 4, width: `${pct}%`, background: pct > 80 ? "#dc2626" : pct > 60 ? "#f59e0b" : "var(--gold-500)" }} />
        </div>
      </div>
    );
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <Link to="/dashboard/runtime" style={{ color: "var(--text-secondary)" }}>Nodes</Link>
        <span style={{ color: "var(--text-faint)" }}>/</span>
        <h2 style={{ margin: 0 }}>Node {id.slice(0, 12)}</h2>
      </div>

      {metrics && (
        <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
          <h3 style={{ margin: "0 0 12px" }}>Host Metrics</h3>
          <div style={{ fontSize: 14, marginBottom: 8 }}>Hostname: <strong>{String(metrics.hostname ?? "")}</strong></div>
          {bar("CPU", Number(metrics.cpuUsedPercent ?? 0), 100, "%")}
          {bar("Memory", Number(metrics.memoryUsedBytes ?? 0) / 1e9, Number(metrics.memoryTotalBytes ?? 0) / 1e9, "GB")}
          {bar("Disk", Number(metrics.diskUsedBytes ?? 0) / 1e9, Number(metrics.diskTotalBytes ?? 0) / 1e9, "GB")}
          <div style={{ fontSize: 13, color: "var(--text-secondary)" }}>Uptime: {String(metrics.uptime ?? "-")}</div>
        </section>
      )}

      {packages && (
        <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
          <h3 style={{ margin: "0 0 12px" }}>
            Packages
            {Boolean(packages.rebootRequired) && <StatusBadge status="Reboot Required" />}
          </h3>
          <div style={{ fontSize: 14, marginBottom: 8 }}>
            Upgradable: <strong>{String(packages.upgradableCount ?? 0)}</strong>
          </div>
          {Array.isArray(packages.upgradable) && (packages.upgradable as ItemMap[]).length > 0 && (
            <pre style={{ background: "var(--code-bg)", padding: 10, borderRadius: 6, fontSize: 12, maxHeight: 200, overflow: "auto" }}>
              {(packages.upgradable as ItemMap[]).map((p) => String(p.name ?? p)).join("\n")}
            </pre>
          )}
        </section>
      )}

      {!metrics && !packages && <p style={{ color: "var(--text-secondary)" }}>No metrics available for this node. The agent may not be connected.</p>}

      <div style={{ display: "flex", gap: 8 }}>
        <button onClick={handleCheckUpdates} style={{ padding: "6px 14px" }}>Check Updates</button>
        <button onClick={() => setConfirmMaintenance(true)} style={{ padding: "6px 14px" }}>Run Maintenance</button>
      </div>

      <ConfirmDialog
        open={confirmMaintenance}
        title="Run Maintenance"
        message="This will trigger system maintenance on the node. Continue?"
        onConfirm={handleMaintenance}
        onCancel={() => setConfirmMaintenance(false)}
        loading={actionLoading}
      />
    </div>
  );
}
