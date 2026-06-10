import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, type ItemMap } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { StatusBadge } from "../../components/StatusBadge";

export function RuntimePage() {
  const { session } = useAuth();
  const { dashboard, refreshBuildQueue, refreshNodes } = useAppData();
  const toast = useToast();
  const navigate = useNavigate();

  const [clusterResources, setClusterResources] = useState<ItemMap | null>(null);

  useEffect(() => {
    if (!session) return;
    api.getClusterResources(session).then(setClusterResources).catch(() => {});
  }, [session]);

  const nodeColumns: Column[] = [
    { key: "hostname", label: "Hostname", render: (v) => String(v ?? "-") },
    { key: "status", label: "Status", render: (v) => <StatusBadge status={String(v ?? "")} /> },
    { key: "id", label: "ID", render: (v) => String(v ?? "").slice(0, 12) },
  ];

  const serviceColumns: Column[] = [
    { key: "name", label: "Name" },
    { key: "mode", label: "Mode", render: (v) => String(v ?? "-") },
    { key: "id", label: "ID", render: (v) => String(v ?? "").slice(0, 12) },
  ];

  const buildQueueColumns: Column[] = [
    { key: "id", label: "ID", render: (v) => String(v ?? "").slice(0, 8) },
    { key: "status", label: "Status", render: (v) => <StatusBadge status={String(v ?? "")} /> },
    { key: "imageTag", label: "Image Tag", render: (v) => String(v ?? "-") },
    {
      key: "id",
      label: "Actions",
      render: (_v, row) => (
        <div style={{ display: "flex", gap: 4 }}>
          <button onClick={(e) => { e.stopPropagation(); handleCancelBuild(String(row.id)); }} style={{ fontSize: 12 }}>Cancel</button>
          <button onClick={(e) => { e.stopPropagation(); handleRetryBuild(String(row.id)); }} style={{ fontSize: 12 }}>Retry</button>
        </div>
      ),
    },
  ];

  async function handleCancelBuild(id: string) {
    if (!session) return;
    try {
      await api.cancelBuild(session, id);
      toast.success("Build cancelled");
      await refreshBuildQueue();
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  async function handleRetryBuild(id: string) {
    if (!session) return;
    try {
      await api.retryBuild(session, id);
      toast.success("Build retried");
      await refreshBuildQueue();
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div className="page-header">
        <h2 style={{ margin: 0 }}>Runtime</h2>
        <button onClick={() => { refreshBuildQueue(); refreshNodes(); }} style={{ padding: "6px 14px" }}>Refresh</button>
      </div>

      {clusterResources && (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(140px, 1fr))", gap: 12 }}>
          {Object.entries(clusterResources).filter(([k]) => typeof clusterResources[k] === "number").map(([key, val]) => (
            <div key={key} style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 12, textAlign: "center", background: "var(--bg-secondary)" }}>
              <div style={{ fontSize: 24, fontWeight: 700 }}>{String(val)}</div>
              <div style={{ fontSize: 12, color: "var(--text-secondary)", textTransform: "capitalize" }}>{key.replace(/([A-Z])/g, " $1")}</div>
            </div>
          ))}
        </div>
      )}

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <h3 style={{ margin: "0 0 12px" }}>Nodes ({dashboard.nodes.length})</h3>
        <DataTable columns={nodeColumns} rows={dashboard.nodes} loading={dashboard.loading} onRowClick={(row) => navigate(`/dashboard/nodes/${row.id}`)} emptyMessage="No nodes." />
      </section>

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <h3 style={{ margin: "0 0 12px" }}>Services ({dashboard.services.length})</h3>
        <DataTable columns={serviceColumns} rows={dashboard.services} loading={dashboard.loading} emptyMessage="No services." />
      </section>

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <h3 style={{ margin: "0 0 12px" }}>Build Queue ({dashboard.buildQueue.length})</h3>
        <DataTable columns={buildQueueColumns} rows={dashboard.buildQueue} loading={dashboard.loading} emptyMessage="No builds in queue." />
      </section>
    </div>
  );
}
