import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, type ItemMap } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { StatusBadge } from "../../components/StatusBadge";
import { FormDialog } from "../../components/FormDialog";
import { ConfirmDialog } from "../../components/ConfirmDialog";

export function RuntimePage() {
  const { session } = useAuth();
  const { dashboard, refreshBuildQueue, refreshNodes } = useAppData();
  const toast = useToast();
  const navigate = useNavigate();

  const [clusterResources, setClusterResources] = useState<ItemMap | null>(null);

  // Node action state
  const [labelsTarget, setLabelsTarget] = useState<ItemMap | null>(null);
  const [labelsText, setLabelsText] = useState("");
  const [savingLabels, setSavingLabels] = useState(false);
  const [confirmAction, setConfirmAction] = useState<{ kind: "drain" | "promote" | "demote"; node: ItemMap } | null>(null);
  const [removeTarget, setRemoveTarget] = useState<ItemMap | null>(null);
  const [removing, setRemoving] = useState(false);

  useEffect(() => {
    if (!session) return;
    api.getClusterResources(session).then(setClusterResources).catch(() => {});
  }, [session]);

  function parseLabels(text: string): Record<string, string> {
    const labels: Record<string, string> = {};
    for (const line of text.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      const eq = trimmed.indexOf("=");
      if (eq <= 0) continue;
      labels[trimmed.slice(0, eq).trim()] = trimmed.slice(eq + 1).trim();
    }
    return labels;
  }

  function openLabelsDialog(node: ItemMap) {
    const labels = (node.labels ?? {}) as Record<string, unknown>;
    setLabelsText(Object.entries(labels).map(([k, v]) => `${k}=${String(v)}`).join("\n"));
    setLabelsTarget(node);
  }

  async function handleSaveLabels() {
    if (!session || !labelsTarget) return;
    setSavingLabels(true);
    try {
      await api.updateNodeLabels(session, String(labelsTarget.id), { labels: parseLabels(labelsText) });
      toast.success("Node labels updated");
      setLabelsTarget(null);
      await refreshNodes();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSavingLabels(false);
    }
  }

  async function handleNodeAction(kind: "drain" | "promote" | "demote", node: ItemMap) {
    if (!session) return;
    try {
      if (kind === "drain") await api.drainNode(session, String(node.id));
      else if (kind === "promote") await api.promoteNode(session, String(node.id));
      else await api.demoteNode(session, String(node.id));
      toast.success(`Node ${kind}ed`);
      await refreshNodes();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setConfirmAction(null);
    }
  }

  async function handleRemoveNode() {
    if (!session || !removeTarget) return;
    setRemoving(true);
    try {
      await api.removeNode(session, String(removeTarget.id), false);
      toast.success("Node removed");
      await refreshNodes();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setRemoving(false);
    }
  }

  const nodeColumns: Column[] = [
    { key: "hostname", label: "Hostname", render: (v) => String(v ?? "-") },
    { key: "status", label: "Status", render: (v) => <StatusBadge status={String(v ?? "")} /> },
    { key: "id", label: "ID", render: (v) => String(v ?? "").slice(0, 12) },
    {
      key: "actions",
      label: "Actions",
      render: (_v, row) => (
        <div style={{ display: "flex", gap: 4 }} onClick={(e) => e.stopPropagation()}>
          <button style={{ fontSize: 12 }} onClick={() => openLabelsDialog(row)}>Labels</button>
          <button style={{ fontSize: 12 }} onClick={() => setConfirmAction({ kind: "drain", node: row })}>Drain</button>
          <button style={{ fontSize: 12 }} onClick={() => setConfirmAction({ kind: "promote", node: row })}>Promote</button>
          <button style={{ fontSize: 12 }} onClick={() => setConfirmAction({ kind: "demote", node: row })}>Demote</button>
          <button style={{ fontSize: 12, color: "var(--danger, #c0392b)" }} onClick={() => setRemoveTarget(row)}>Remove</button>
        </div>
      ),
    },
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

  const actionLabels = { drain: "Drain", promote: "Promote", demote: "Demote" } as const;

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

      <FormDialog open={!!labelsTarget} title={`Edit Labels — ${String(labelsTarget?.hostname ?? "")}`} onClose={() => setLabelsTarget(null)} onSubmit={handleSaveLabels} loading={savingLabels}>
        <p style={{ fontSize: 13, color: "var(--text-secondary)", margin: "0 0 8px" }}>One label per line as key=value. Existing labels are merged with the node's set.</p>
        <label>Labels<textarea value={labelsText} onChange={(e) => setLabelsText(e.target.value)} rows={6} placeholder={"role=edge\nzone=eu-west"} style={{ width: "100%", padding: "6px 10px", marginTop: 4, fontFamily: "monospace" }} /></label>
      </FormDialog>

      <ConfirmDialog
        open={!!confirmAction}
        title={confirmAction ? `${actionLabels[confirmAction.kind]} Node` : ""}
        message={confirmAction ? `${actionLabels[confirmAction.kind]} "${String(confirmAction.node.hostname ?? confirmAction.node.id)}"?` : ""}
        onConfirm={() => confirmAction && handleNodeAction(confirmAction.kind, confirmAction.node)}
        onCancel={() => setConfirmAction(null)}
      />

      <ConfirmDialog
        open={!!removeTarget}
        title="Remove Node"
        destructive
        message={`Remove "${String(removeTarget?.hostname ?? removeTarget?.id ?? "")}" from the swarm? Running workloads will be rescheduled.`}
        onConfirm={handleRemoveNode}
        onCancel={() => setRemoveTarget(null)}
        loading={removing}
      />
    </div>
  );
}
