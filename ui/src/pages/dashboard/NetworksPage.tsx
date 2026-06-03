import { useState } from "react";
import { api } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { FormDialog } from "../../components/FormDialog";

export function NetworksPage() {
  const { session } = useAuth();
  const { dashboard, refreshNetworks } = useAppData();
  const toast = useToast();

  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);

  const columns: Column[] = [
    { key: "name", label: "Name" },
    { key: "driver", label: "Driver", render: (v) => String(v ?? "overlay") },
    { key: "id", label: "ID", render: (v) => String(v ?? "").slice(0, 12) },
  ];

  async function handleCreate() {
    if (!session || !name) return;
    setCreating(true);
    try {
      await api.createNetwork(session, { name });
      toast.success("Network created");
      setShowCreate(false);
      setName("");
      await refreshNetworks();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCreating(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Networks</h2>
        <button onClick={() => setShowCreate(true)} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: "pointer" }}>
          Create Network
        </button>
      </div>
      <DataTable columns={columns} rows={dashboard.networks} emptyMessage="No networks yet." />

      <FormDialog open={showCreate} title="Create Network" onClose={() => setShowCreate(false)} onSubmit={handleCreate} loading={creating}>
        <label>Name<input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-network" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      </FormDialog>
    </div>
  );
}
