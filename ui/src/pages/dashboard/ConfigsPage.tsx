import { useState } from "react";
import { api } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { FormDialog } from "../../components/FormDialog";

export function ConfigsPage() {
  const { session } = useAuth();
  const { dashboard, refreshConfigs } = useAppData();
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
      await api.createConfig(session, { name, data });
      toast.success("Config created");
      setShowCreate(false);
      setName("");
      setData("");
      await refreshConfigs();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCreating(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Configs</h2>
        <button onClick={() => setShowCreate(true)} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: "pointer" }}>
          Create Config
        </button>
      </div>
      <DataTable columns={columns} rows={dashboard.configs} loading={dashboard.loading} emptyMessage="No configs yet." />

      <FormDialog open={showCreate} title="Create Config" onClose={() => setShowCreate(false)} onSubmit={handleCreate} loading={creating}>
        <label>Name<input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-config" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Data<textarea value={data} onChange={(e) => setData(e.target.value)} placeholder="Config data..." rows={6} style={{ width: "100%", padding: "6px 10px", marginTop: 4, fontFamily: "monospace" }} /></label>
      </FormDialog>
    </div>
  );
}
