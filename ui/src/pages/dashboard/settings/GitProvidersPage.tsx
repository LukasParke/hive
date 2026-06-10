import { useState } from "react";
import { api } from "../../../api/client";
import { useAuth } from "../../../contexts/AuthContext";
import { useAppData } from "../../../contexts/AppContext";
import { useToast } from "../../../contexts/ToastContext";
import { DataTable, type Column } from "../../../components/DataTable";
import { FormDialog } from "../../../components/FormDialog";

export function GitProvidersPage() {
  const { session } = useAuth();
  const { dashboard, refreshGitProviders } = useAppData();
  const toast = useToast();

  const [showCreate, setShowCreate] = useState(false);
  const [type, setType] = useState("github");
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("https://github.com");
  const [token, setToken] = useState("");
  const [creating, setCreating] = useState(false);

  const columns: Column[] = [
    { key: "name", label: "Name" },
    { key: "type", label: "Type" },
    { key: "baseUrl", label: "Base URL", render: (v) => String(v ?? "-") },
    { key: "enabled", label: "Enabled", render: (v) => (v !== false ? "Yes" : "No") },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
  ];

  async function handleCreate() {
    if (!session || !name) return;
    setCreating(true);
    try {
      await api.createGitProvider(session, { type, name, baseUrl, tokenSecretName: token || undefined, enabled: true });
      toast.success("Git provider created");
      setShowCreate(false);
      setName("");
      setToken("");
      await refreshGitProviders();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCreating(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Git Providers</h2>
        <button onClick={() => setShowCreate(true)} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: "pointer" }}>Create Provider</button>
      </div>
      <DataTable columns={columns} rows={dashboard.gitProviders} loading={dashboard.loading} emptyMessage="No git providers configured." />

      <FormDialog open={showCreate} title="Create Git Provider" onClose={() => setShowCreate(false)} onSubmit={handleCreate} loading={creating}>
        <label>Type<select value={type} onChange={(e) => setType(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}><option value="github">GitHub</option><option value="gitlab">GitLab</option><option value="gitea">Gitea</option><option value="bitbucket">Bitbucket</option></select></label>
        <label>Name<input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-github" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Base URL<input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://github.com" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Token (optional)<input type="password" value={token} onChange={(e) => setToken(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      </FormDialog>
    </div>
  );
}
