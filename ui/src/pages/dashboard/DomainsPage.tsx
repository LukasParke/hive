import { useState } from "react";
import { api, type ItemMap } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { FormDialog } from "../../components/FormDialog";
import { ConfirmDialog } from "../../components/ConfirmDialog";

export function DomainsPage() {
  const { session } = useAuth();
  const { dashboard, refreshDomains } = useAppData();
  const toast = useToast();

  const [showCreate, setShowCreate] = useState(false);
  const [appId, setAppId] = useState("");
  const [hostname, setHostname] = useState("");
  const [routeType, setRouteType] = useState<"host" | "wildcard" | "path">("host");
  const [pathPrefix, setPathPrefix] = useState("");
  const [stripPrefix, setStripPrefix] = useState(false);
  const [priority, setPriority] = useState("");
  const [creating, setCreating] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<ItemMap | null>(null);
  const [deleting, setDeleting] = useState(false);

  const columns: Column[] = [
    { key: "routeType", label: "Route", render: (v, row) => {
        const t = String(v || "host");
        const prefix = String(row.pathPrefix ?? "");
        return t === "path" ? `${prefix} → ${t}` : t;
      } },
    { key: "priority", label: "Priority", render: (v) => (v ? String(v) : "auto") },
    { key: "hostname", label: "Hostname" },
    { key: "applicationId", label: "Application", render: (v) => { const a = dashboard.applications.find((a) => a.id === v); return String(a?.name ?? v ?? "-"); } },
    { key: "tlsEnabled", label: "TLS", render: (v) => (v ? "Yes" : "No") },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
    { key: "id", label: "", render: (_v, row) => <button onClick={(e) => { e.stopPropagation(); setDeleteTarget(row); }} style={{ color: "var(--danger-text)", fontSize: 12 }}>Delete</button> },
  ];

  async function handleCreate() {
    if (!session || !hostname || !appId) return;
    setCreating(true);
    try {
      await api.createDomain(session, {
        applicationId: appId,
        hostname,
        tlsEnabled: true,
        routeType,
        ...(routeType === "path"
          ? { pathPrefix, stripPrefix, ...(priority ? { priority: Number(priority) } : {}) }
          : priority ? { priority: Number(priority) } : {}),
      });
      toast.success("Domain created");
      setShowCreate(false);
      setHostname("");
      setRouteType("host");
      setPathPrefix("");
      setStripPrefix(false);
      setPriority("");
      await refreshDomains();
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
      await api.deleteDomain(session, String(deleteTarget.id));
      toast.success("Domain deleted");
      setDeleteTarget(null);
      await refreshDomains();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleting(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Domains</h2>
        <button onClick={() => { setShowCreate(true); setAppId(String(dashboard.applications[0]?.id ?? "")); }} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: "pointer" }}>
          Create Domain
        </button>
      </div>
      <DataTable columns={columns} rows={dashboard.domains} loading={dashboard.loading} emptyMessage="No domains yet." />

      <FormDialog open={showCreate} title="Create Domain" onClose={() => setShowCreate(false)} onSubmit={handleCreate} loading={creating}>
        <label>
          Application
          <select value={appId} onChange={(e) => setAppId(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}>
            {dashboard.applications.map((a) => <option key={String(a.id)} value={String(a.id)}>{String(a.name)}</option>)}
          </select>
        </label>
        <label>Hostname<input value={hostname} onChange={(e) => setHostname(e.target.value)} placeholder="app.example.com  (*.example.com for wildcard)" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>
          Route Type
          <select value={routeType} onChange={(e) => setRouteType(e.target.value as "host" | "wildcard" | "path")} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}>
            <option value="host">host (exact hostname)</option>
            <option value="wildcard">wildcard (*.example.com)</option>
            <option value="path">path (host + path prefix)</option>
          </select>
        </label>
        {routeType === "path" && (
          <label>Path Prefix<input value={pathPrefix} onChange={(e) => setPathPrefix(e.target.value)} placeholder="/api" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        )}
        {routeType === "path" && (
          <label style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <input type="checkbox" checked={stripPrefix} onChange={(e) => setStripPrefix(e.target.checked)} />
            Strip path prefix before forwarding
          </label>
        )}
        <label>Priority<input type="number" min={0} value={priority} onChange={(e) => setPriority(e.target.value)} placeholder="auto" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      </FormDialog>

      <ConfirmDialog open={!!deleteTarget} title="Delete Domain" message={`Delete "${String(deleteTarget?.hostname ?? "")}"?`} destructive onConfirm={handleDelete} onCancel={() => setDeleteTarget(null)} loading={deleting} />
    </div>
  );
}
