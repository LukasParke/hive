import { useEffect, useState } from "react";
import { api, type ItemMap } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { StatusBadge } from "../../components/StatusBadge";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { LoadingState } from "../../components/LoadingState";

export function DeploymentsPage() {
  const { session } = useAuth();
  const toast = useToast();

  const [deployments, setDeployments] = useState<ItemMap[]>([]);
  const [loading, setLoading] = useState(true);
  const [deleteTarget, setDeleteTarget] = useState<ItemMap | null>(null);
  const [deleting, setDeleting] = useState(false);

  async function load() {
    if (!session) return;
    try {
      const res = await api.listDeployments(session);
      setDeployments(res.items);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { load(); }, [session]); // eslint-disable-line react-hooks/exhaustive-deps

  const columns: Column[] = [
    { key: "applicationName", label: "Application", render: (v) => String(v ?? "-") },
    { key: "imageTag", label: "Image Tag", render: (v) => String(v ?? "-") },
    { key: "status", label: "Status", render: (v) => <StatusBadge status={String(v ?? "")} /> },
    { key: "trigger", label: "Trigger", render: (v) => String(v ?? "-") },
    { key: "createdAt", label: "Date", render: (v) => (v ? new Date(String(v)).toLocaleString() : "") },
    {
      key: "id",
      label: "",
      render: (_v, row) => (
        <button onClick={(e) => { e.stopPropagation(); setDeleteTarget(row); }} style={{ color: "var(--danger-text)", fontSize: 12 }}>Delete</button>
      ),
    },
  ];

  async function handleDelete() {
    if (!session || !deleteTarget) return;
    setDeleting(true);
    try {
      await api.deleteDeployment(session, String(deleteTarget.id));
      toast.success("Deployment deleted");
      setDeleteTarget(null);
      await load();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleting(false);
    }
  }

  if (loading) return <LoadingState />;

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Deployments</h2>
        <button onClick={load} style={{ padding: "6px 14px" }}>Refresh</button>
      </div>
      <DataTable columns={columns} rows={deployments} emptyMessage="No deployments yet." />
      <ConfirmDialog open={!!deleteTarget} title="Delete Deployment" message="Are you sure you want to delete this deployment record?" destructive onConfirm={handleDelete} onCancel={() => setDeleteTarget(null)} loading={deleting} />
    </div>
  );
}
