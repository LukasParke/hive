import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api, type PreviewDeployment, type Session } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { StatusBadge } from "../../components/StatusBadge";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { LoadingState } from "../../components/LoadingState";

export function PreviewsPage() {
  const { id = "" } = useParams();
  const { session } = useAuth();
  const toast = useToast();

  const [previews, setPreviews] = useState<PreviewDeployment[]>([]);
  const [loading, setLoading] = useState(true);
  const [deleteTarget, setDeleteTarget] = useState<PreviewDeployment | null>(null);
  const [deleting, setDeleting] = useState(false);

  async function load() {
    if (!session || !id) return;
    try {
      const res = await api.listPreviewDeployments(session, id);
      setPreviews(res.items);
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { load(); }, [session, id]); // eslint-disable-line react-hooks/exhaustive-deps

  const columns: Column[] = [
    { key: "prNumber", label: "PR", render: (v) => `#${v}` },
    { key: "branch", label: "Branch", render: (v) => String(v ?? "-") },
    { key: "commitSha", label: "Commit", render: (v) => String(v ?? "").slice(0, 7) },
    { key: "status", label: "Status", render: (v) => <StatusBadge status={String(v ?? "")} /> },
    { key: "url", label: "URL", render: (v) => v ? <a href={String(v)} target="_blank" rel="noreferrer">{String(v)}</a> : "-" },
    { key: "createdAt", label: "Date", render: (v) => (v ? new Date(String(v)).toLocaleString() : "") },
    {
      key: "id",
      label: "",
      render: (_v, row) => (
        <button onClick={(e) => { e.stopPropagation(); setDeleteTarget(row as unknown as PreviewDeployment); }} style={{ color: "var(--danger-text)", fontSize: 12 }}>Delete</button>
      ),
    },
  ];

  async function handleDelete() {
    if (!session || !id || !deleteTarget) return;
    setDeleting(true);
    try {
      await api.deletePreviewDeployment(session, id, String(deleteTarget.id));
      toast.success("Preview deployment deleted");
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
        <h2 style={{ margin: 0 }}>Preview Deployments</h2>
        <button onClick={load} style={{ padding: "6px 14px" }}>Refresh</button>
      </div>
      <DataTable columns={columns} rows={previews as unknown as Record<string, unknown>[]} emptyMessage="No preview deployments yet." />
      <ConfirmDialog open={!!deleteTarget} title="Delete Preview Deployment" message="Are you sure you want to delete this preview deployment?" destructive onConfirm={handleDelete} onCancel={() => setDeleteTarget(null)} loading={deleting} />
    </div>
  );
}
