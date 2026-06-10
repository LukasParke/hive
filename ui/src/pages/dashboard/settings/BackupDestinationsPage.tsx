import { useState } from "react";
import { api, type ItemMap } from "../../../api/client";
import { useAuth } from "../../../contexts/AuthContext";
import { useAppData } from "../../../contexts/AppContext";
import { useToast } from "../../../contexts/ToastContext";
import { DataTable, type Column } from "../../../components/DataTable";
import { FormDialog } from "../../../components/FormDialog";
import { ConfirmDialog } from "../../../components/ConfirmDialog";

export function BackupDestinationsPage() {
  const { session } = useAuth();
  const { dashboard, refreshBackupDestinations } = useAppData();
  const toast = useToast();

  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [type, setType] = useState("local");
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ItemMap | null>(null);
  const [deleting, setDeleting] = useState(false);

  const columns: Column[] = [
    { key: "name", label: "Name" },
    { key: "type", label: "Type" },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
    {
      key: "id",
      label: "Actions",
      render: (_v, row) => (
        <div style={{ display: "flex", gap: 8 }}>
          <button onClick={(e) => { e.stopPropagation(); handleTest(String(row.id)); }} style={{ fontSize: 12 }}>Test</button>
          <button onClick={(e) => { e.stopPropagation(); setDeleteTarget(row); }} style={{ color: "var(--danger-text)", fontSize: 12 }}>Delete</button>
        </div>
      ),
    },
  ];

  async function handleCreate() {
    if (!session || !name) return;
    setCreating(true);
    try {
      await api.createBackupDestination(session, { name, type, config: {} });
      toast.success("Backup destination created");
      setShowCreate(false);
      setName("");
      await refreshBackupDestinations();
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
      await api.deleteBackupDestination(session, String(deleteTarget.id));
      toast.success("Backup destination deleted");
      setDeleteTarget(null);
      await refreshBackupDestinations();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleting(false);
    }
  }

  async function handleTest(id: string) {
    if (!session) return;
    try {
      await api.testBackupDestination(session, id);
      toast.success("Backup destination test successful");
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Backup Destinations</h2>
        <button onClick={() => setShowCreate(true)} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: "pointer" }}>Create Destination</button>
      </div>
      <DataTable columns={columns} rows={dashboard.backupDestinations} loading={dashboard.loading} emptyMessage="No backup destinations configured." />

      <FormDialog open={showCreate} title="Create Backup Destination" onClose={() => setShowCreate(false)} onSubmit={handleCreate} loading={creating}>
        <label>Name<input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-backup-dest" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Type<select value={type} onChange={(e) => setType(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}><option value="local">Local</option><option value="shared">Shared</option><option value="s3">S3</option></select></label>
      </FormDialog>

      <ConfirmDialog open={!!deleteTarget} title="Delete Destination" message={`Delete "${String(deleteTarget?.name ?? "")}"?`} destructive onConfirm={handleDelete} onCancel={() => setDeleteTarget(null)} loading={deleting} />
    </div>
  );
}
