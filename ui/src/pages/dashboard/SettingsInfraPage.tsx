import { useEffect, useState } from "react";
import { api, type ItemMap } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { FormDialog } from "../../components/FormDialog";

export function SettingsInfraPage() {
  const { session } = useAuth();
  const toast = useToast();

  const [servers, setServers] = useState<ItemMap[]>([]);
  const [sshKeys, setSSHKeys] = useState<ItemMap[]>([]);
  const [certs, setCerts] = useState<ItemMap[]>([]);
  const [requests, setRequests] = useState<ItemMap[]>([]);
  const [cluster, setCluster] = useState<ItemMap | null>(null);

  const [showCreateServer, setShowCreateServer] = useState(false);
  const [serverName, setServerName] = useState("");
  const [serverHost, setServerHost] = useState("");
  const [creating, setCreating] = useState(false);

  async function refresh() {
    if (!session) return;
    const [s, k, c, r, cl] = await Promise.all([
      api.listSettingsServers(session),
      api.listSSHKeys(session),
      api.listCertificates(session),
      api.listRequestEvents(session),
      api.getClusterInfo(session),
    ]);
    setServers(s.items);
    setSSHKeys(k.items);
    setCerts(c.items);
    setRequests(r.items);
    setCluster(cl);
  }

  useEffect(() => { refresh(); }, [session]); // eslint-disable-line react-hooks/exhaustive-deps

  const serverColumns: Column[] = [
    { key: "name", label: "Name" },
    { key: "host", label: "Host" },
    { key: "sshPort", label: "SSH Port", render: (v) => String(v ?? 22) },
  ];

  const sshKeyColumns: Column[] = [
    { key: "name", label: "Name" },
    { key: "publicKey", label: "Public Key", render: (v) => String(v ?? "").slice(0, 40) + "..." },
  ];

  const certColumns: Column[] = [
    { key: "domain", label: "Domain" },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
  ];

  const requestColumns: Column[] = [
    { key: "category", label: "Category" },
    { key: "message", label: "Message" },
    { key: "createdAt", label: "Date", render: (v) => (v ? new Date(String(v)).toLocaleString() : "") },
  ];

  async function handleCreateServer() {
    if (!session || !serverName || !serverHost) return;
    setCreating(true);
    try {
      await api.createSettingsServer(session, { name: serverName, host: serverHost, sshPort: 22 });
      toast.success("Server created");
      setShowCreateServer(false);
      setServerName("");
      setServerHost("");
      await refresh();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCreating(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Infrastructure</h2>
        <button onClick={refresh} style={{ padding: "6px 14px" }}>Refresh</button>
      </div>

      {cluster && (
        <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
          <h3 style={{ margin: "0 0 8px" }}>Cluster Info</h3>
          <pre style={{ background: "var(--code-bg)", padding: 10, borderRadius: 6, fontSize: 13 }}>{JSON.stringify(cluster, null, 2)}</pre>
        </section>
      )}

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
          <h3 style={{ margin: 0 }}>Servers</h3>
          <button onClick={() => setShowCreateServer(true)} style={{ padding: "4px 12px" }}>Add Server</button>
        </div>
        <DataTable columns={serverColumns} rows={servers} emptyMessage="No servers." />
      </section>

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <h3 style={{ margin: "0 0 12px" }}>SSH Keys</h3>
        <DataTable columns={sshKeyColumns} rows={sshKeys} emptyMessage="No SSH keys." />
      </section>

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <h3 style={{ margin: "0 0 12px" }}>Certificates</h3>
        <DataTable columns={certColumns} rows={certs} emptyMessage="No certificates." />
      </section>

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <h3 style={{ margin: "0 0 12px" }}>Audit Log</h3>
        <DataTable columns={requestColumns} rows={requests} emptyMessage="No request events." />
      </section>

      <FormDialog open={showCreateServer} title="Add Server" onClose={() => setShowCreateServer(false)} onSubmit={handleCreateServer} loading={creating}>
        <label>Name<input value={serverName} onChange={(e) => setServerName(e.target.value)} placeholder="edge-01" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Host<input value={serverHost} onChange={(e) => setServerHost(e.target.value)} placeholder="10.10.10.51" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      </FormDialog>
    </div>
  );
}
