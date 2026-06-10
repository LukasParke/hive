import { useState } from "react";
import { api } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { FormDialog } from "../../components/FormDialog";

export function SecretsPage() {
  const { session } = useAuth();
  const { dashboard, refreshSecrets } = useAppData();
  const toast = useToast();

  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [data, setData] = useState("");
  const [creating, setCreating] = useState(false);

  const columns: Column[] = [
    { key: "name", label: "Name" },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
  ];

  async function handleCreate() {
    if (!session || !name) return;
    setCreating(true);
    try {
      await api.createSecret(session, { name, data });
      toast.success("Secret created");
      setShowCreate(false);
      setName("");
      setData("");
      await refreshSecrets();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCreating(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Secrets</h2>
        <button onClick={() => setShowCreate(true)} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: "pointer" }}>
          Create Secret
        </button>
      </div>
      <p style={{ fontSize: 13, color: "var(--text-secondary)", margin: 0 }}>Swarm secrets are immutable. Once created, they cannot be edited or deleted.</p>
      <DataTable columns={columns} rows={dashboard.secrets} loading={dashboard.loading} emptyMessage="No secrets yet." />

      <FormDialog open={showCreate} title="Create Secret" onClose={() => setShowCreate(false)} onSubmit={handleCreate} loading={creating}>
        <label>Name<input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-secret" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Data<textarea value={data} onChange={(e) => setData(e.target.value)} placeholder="Secret value..." rows={4} style={{ width: "100%", padding: "6px 10px", marginTop: 4, fontFamily: "monospace" }} /></label>
      </FormDialog>
    </div>
  );
}
