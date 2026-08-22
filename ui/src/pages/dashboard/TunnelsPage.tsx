import { useEffect, useState } from "react";
import { api, type Tunnel, type TunnelIngressRule } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { FormDialog } from "../../components/FormDialog";
import { ConfirmDialog } from "../../components/ConfirmDialog";

const emptyRule = (): TunnelIngressRule => ({ hostname: "", service: "http://traefik:80" });

function statusBadge(t: Tunnel) {
  const colors: Record<string, string> = {
    deployed: "var(--success-text, #3fb26f)",
    creating: "var(--warning-text, #d9a441)",
    error: "var(--danger-text, #d9534f)",
    deleting: "var(--warning-text, #d9a441)",
  };
  const color = colors[t.status] ?? "var(--text-muted, #888)";
  return <span style={{ color, fontWeight: 600 }}>{t.status}</span>;
}

function connectorHealth(t: Tunnel) {
  if (!t.connector) return "-";
  const c = t.connector;
  const parts = [`${c.runningReplicas}/${c.desiredReplicas} replicas`];
  if (c.cloudflareStatus) parts.push(c.cloudflareStatus);
  return parts.join(" · ");
}

export function TunnelsPage() {
  const { session } = useAuth();
  const toast = useToast();

  const [tunnels, setTunnels] = useState<Tunnel[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [creating, setCreating] = useState(false);

  const [name, setName] = useState("");
  const [accountId, setAccountId] = useState("");
  const [zoneId, setZoneId] = useState("");
  const [apiToken, setApiToken] = useState("");
  const [ingress, setIngress] = useState<TunnelIngressRule[]>([emptyRule()]);

  const [deleteTarget, setDeleteTarget] = useState<Tunnel | null>(null);
  const [deleting, setDeleting] = useState(false);

  async function load() {
    if (!session) return;
    setLoading(true);
    try {
      const res = await api.listTunnels(session);
      setTunnels(res.items);
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session]);

  function setRule(idx: number, patch: Partial<TunnelIngressRule>) {
    setIngress((rules) => rules.map((r, i) => (i === idx ? { ...r, ...patch } : r)));
  }

  function resetForm() {
    setName("");
    setAccountId("");
    setZoneId("");
    setApiToken("");
    setIngress([emptyRule()]);
  }

  async function handleCreate() {
    if (!session) return;
    setCreating(true);
    try {
      await api.createTunnel(session, {
        name,
        accountId,
        zoneId: zoneId || undefined,
        apiToken,
        ingress: ingress.filter((r) => r.hostname),
      });
      toast.success("Tunnel created and connector deployed");
      setShowCreate(false);
      resetForm();
      await load();
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
      await api.deleteTunnel(session, deleteTarget.id);
      toast.success("Tunnel deleted");
      setDeleteTarget(null);
      await load();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleting(false);
    }
  }

  const inputStyle = { width: "100%", padding: "6px 10px", marginTop: 4 } as const;

  const columns: Column[] = [
    { key: "name", label: "Name" },
    { key: "cloudflareTunnelId", label: "Cloudflare Tunnel", render: (v) => String(v ?? "-") },
    { key: "status", label: "Status", render: (_v, row) => statusBadge(row as Tunnel) },
    { key: "connector", label: "Connector Health", render: (_v, row) => connectorHealth(row as Tunnel) },
    {
      key: "ingress",
      label: "Ingress",
      render: (_v, row) =>
        (row as Tunnel).ingress.map((r) => r.hostname).join(", ") || "-",
    },
    { key: "errorMessage", label: "Error", render: (v) => (v ? String(v) : "-") },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
    {
      key: "id",
      label: "",
      render: (_v, row) => (
        <button
          onClick={(e) => {
            e.stopPropagation();
            setDeleteTarget(row as Tunnel);
          }}
          style={{ color: "var(--danger-text)", fontSize: 12 }}
        >
          Delete
        </button>
      ),
    },
  ];

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Cloudflare Tunnels</h2>
        <button
          onClick={() => setShowCreate(true)}
          style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: "pointer" }}
        >
          Create Tunnel
        </button>
      </div>
      <DataTable columns={columns} rows={tunnels} loading={loading} emptyMessage="No tunnels yet." />

      <FormDialog
        open={showCreate}
        title="Create Cloudflare Tunnel"
        onClose={() => setShowCreate(false)}
        onSubmit={handleCreate}
        loading={creating}
      >
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="prod-edge (lowercase, hyphens)" style={inputStyle} />
        </label>
        <label>
          Account ID
          <input value={accountId} onChange={(e) => setAccountId(e.target.value)} placeholder="Cloudflare account ID" style={inputStyle} />
        </label>
        <label>
          Zone ID (optional — enables automatic CNAME routes)
          <input value={zoneId} onChange={(e) => setZoneId(e.target.value)} placeholder="Cloudflare zone ID" style={inputStyle} />
        </label>
        <label>
          API Token
          <input type="password" value={apiToken} onChange={(e) => setApiToken(e.target.value)} placeholder="Zone:DNS:Edit + Account:Cloudflare Tunnel:Edit" style={inputStyle} />
        </label>
        <div>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline" }}>
            <strong>Ingress rules</strong>
            <span style={{ fontSize: 12, color: "var(--text-muted, #888)" }}>
              Wildcards allowed (<code>*.example.com</code>); a catch-all 404 is implicit.
            </span>
          </div>
          {ingress.map((rule, idx) => (
            <div key={idx} style={{ display: "flex", gap: 8, marginTop: 8 }}>
              <input
                value={rule.hostname}
                onChange={(e) => setRule(idx, { hostname: e.target.value })}
                placeholder="app.example.com or *.example.com"
                style={{ flex: 1, padding: "6px 10px" }}
              />
              <input
                value={rule.service}
                onChange={(e) => setRule(idx, { service: e.target.value })}
                placeholder="http://traefik:80"
                style={{ flex: 1, padding: "6px 10px" }}
              />
              <button
                type="button"
                onClick={() => setIngress((rules) => (rules.length > 1 ? rules.filter((_, i) => i !== idx) : rules))}
                style={{ padding: "4px 10px", cursor: "pointer" }}
                title="Remove rule"
              >
                ✕
              </button>
            </div>
          ))}
          <button type="button" onClick={() => setIngress((rules) => [...rules, emptyRule()])} style={{ marginTop: 8, padding: "4px 12px", cursor: "pointer" }}>
            + Add rule
          </button>
        </div>
      </FormDialog>

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete Tunnel"
        message={`Delete tunnel "${deleteTarget?.name ?? ""}"? This removes the Cloudflare tunnel, its DNS routes, the connector service and stored credentials.`}
        destructive
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
        loading={deleting}
      />
    </div>
  );
}
