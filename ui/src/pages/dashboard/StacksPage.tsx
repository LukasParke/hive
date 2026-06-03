import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, type ItemMap } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { FormDialog } from "../../components/FormDialog";
import { ConfirmDialog } from "../../components/ConfirmDialog";

export function StacksPage() {
  const { session } = useAuth();
  const { dashboard, refreshStacks } = useAppData();
  const toast = useToast();
  const navigate = useNavigate();

  const [showCreate, setShowCreate] = useState(false);
  const [projectId, setProjectId] = useState("");
  const [name, setName] = useState("");
  const [compose, setCompose] = useState("services:\n  web:\n    image: nginx:alpine\n");
  const [creating, setCreating] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<ItemMap | null>(null);
  const [deleting, setDeleting] = useState(false);

  const columns: Column[] = [
    { key: "name", label: "Name" },
    { key: "projectId", label: "Project", render: (v) => { const p = dashboard.projects.find((p) => p.id === v); return String(p?.name ?? v ?? "-"); } },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
    { key: "id", label: "", render: (_v, row) => <button onClick={(e) => { e.stopPropagation(); setDeleteTarget(row); }} style={{ color: "var(--danger-text)", fontSize: 12 }}>Delete</button> },
  ];

  async function handleCreate() {
    if (!session || !name || !projectId) return;
    setCreating(true);
    try {
      await api.createStack(session, { projectId, name, composeContent: compose });
      toast.success("Stack created");
      setShowCreate(false);
      setName("");
      await refreshStacks();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCreating(false);
    }
  }

  async function handleDelete() {
    if (!session || !deleteTarget) return;
    setDeleting(true);
    try {
      await api.deleteStack(session, String(deleteTarget.id));
      toast.success("Stack deleted");
      setDeleteTarget(null);
      await refreshStacks();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleting(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Stacks</h2>
        <button onClick={() => { setShowCreate(true); setProjectId(String(dashboard.projects[0]?.id ?? "")); }} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, cursor: "pointer", fontWeight: 600 }}>
          Create Stack
        </button>
      </div>

      <DataTable columns={columns} rows={dashboard.stacks} onRowClick={(row) => navigate(`/dashboard/services/stack/${row.id}`)} emptyMessage="No stacks yet." />

      <FormDialog open={showCreate} title="Create Stack" onClose={() => setShowCreate(false)} onSubmit={handleCreate} loading={creating}>
        <label>
          Project
          <select value={projectId} onChange={(e) => setProjectId(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}>
            {dashboard.projects.map((p) => <option key={String(p.id)} value={String(p.id)}>{String(p.name)}</option>)}
          </select>
        </label>
        <label>Name<input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-stack" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Compose Content<textarea value={compose} onChange={(e) => setCompose(e.target.value)} rows={10} style={{ width: "100%", fontFamily: "monospace", fontSize: 13, padding: 8, marginTop: 4 }} /></label>
      </FormDialog>

      <ConfirmDialog open={!!deleteTarget} title="Delete Stack" message={`Delete "${String(deleteTarget?.name ?? "")}"?`} destructive onConfirm={handleDelete} onCancel={() => setDeleteTarget(null)} loading={deleting} />
    </div>
  );
}
