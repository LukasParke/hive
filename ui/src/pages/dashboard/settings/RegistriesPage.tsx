import { useState } from "react";
import { api, type ItemMap } from "../../../api/client";
import { useAuth } from "../../../contexts/AuthContext";
import { useAppData } from "../../../contexts/AppContext";
import { useToast } from "../../../contexts/ToastContext";
import { DataTable, type Column } from "../../../components/DataTable";
import { FormDialog } from "../../../components/FormDialog";
import { ConfirmDialog } from "../../../components/ConfirmDialog";

export function RegistriesPage() {
  const { session } = useAuth();
  const { dashboard, refreshRegistries } = useAppData();
  const toast = useToast();

  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [username, setUsername] = useState("");
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ItemMap | null>(null);
  const [deleting, setDeleting] = useState(false);

  const columns: Column[] = [
    { key: "name", label: "Name" },
    { key: "url", label: "URL" },
    { key: "username", label: "Username", render: (v) => String(v ?? "-") },
    { key: "isDefault", label: "Default", render: (v) => (v ? "Yes" : "No") },
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
    if (!session || !name || !url) return;
    setCreating(true);
    try {
      await api.createRegistry(session, { name, url, username: username || undefined, isDefault: false });
      toast.success("Registry created");
      setShowCreate(false);
      setName("");
      setUrl("");
      setUsername("");
      await refreshRegistries();
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
      await api.deleteRegistry(session, String(deleteTarget.id));
      toast.success("Registry deleted");
      setDeleteTarget(null);
      await refreshRegistries();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleting(false);
    }
  }

  async function handleTest(id: string) {
    if (!session) return;
    try {
      await api.testRegistry(session, id);
      toast.success("Registry connection successful");
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Registries</h2>
        <button onClick={() => setShowCreate(true)} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: "pointer" }}>Create Registry</button>
      </div>
      <DataTable columns={columns} rows={dashboard.registries} loading={dashboard.loading} emptyMessage="No registries configured." />

      <FormDialog open={showCreate} title="Create Registry" onClose={() => setShowCreate(false)} onSubmit={handleCreate} loading={creating}>
        <label>Name<input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-registry" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>URL<input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://registry.example.com" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Username (optional)<input value={username} onChange={(e) => setUsername(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      </FormDialog>

      <ConfirmDialog open={!!deleteTarget} title="Delete Registry" message={`Delete "${String(deleteTarget?.name ?? "")}"?`} destructive onConfirm={handleDelete} onCancel={() => setDeleteTarget(null)} loading={deleting} />
    </div>
  );
}
