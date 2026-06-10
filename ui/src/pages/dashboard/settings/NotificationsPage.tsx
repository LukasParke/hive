import { useState } from "react";
import { api, type ItemMap } from "../../../api/client";
import { useAuth } from "../../../contexts/AuthContext";
import { useAppData } from "../../../contexts/AppContext";
import { useToast } from "../../../contexts/ToastContext";
import { DataTable, type Column } from "../../../components/DataTable";
import { FormDialog } from "../../../components/FormDialog";
import { ConfirmDialog } from "../../../components/ConfirmDialog";

export function NotificationsPage() {
  const { session } = useAuth();
  const { dashboard, refreshNotifications } = useAppData();
  const toast = useToast();

  const [showCreate, setShowCreate] = useState(false);
  const [channel, setChannel] = useState("webhook");
  const [target, setTarget] = useState("");
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ItemMap | null>(null);
  const [deleting, setDeleting] = useState(false);

  const columns: Column[] = [
    { key: "channel", label: "Channel" },
    { key: "target", label: "Target" },
    { key: "enabled", label: "Enabled", render: (v) => (v ? "Yes" : "No") },
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
    if (!session || !target) return;
    setCreating(true);
    try {
      await api.createNotification(session, { channel, target, enabled: true });
      toast.success("Notification created");
      setShowCreate(false);
      setTarget("");
      await refreshNotifications();
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
      await api.deleteNotification(session, String(deleteTarget.id));
      toast.success("Notification deleted");
      setDeleteTarget(null);
      await refreshNotifications();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleting(false);
    }
  }

  async function handleTest(id: string) {
    if (!session) return;
    try {
      await api.testNotification(session, id);
      toast.success("Test sent");
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Notifications</h2>
        <button onClick={() => setShowCreate(true)} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: "pointer" }}>Create Notification</button>
      </div>
      <DataTable columns={columns} rows={dashboard.notifications} loading={dashboard.loading} emptyMessage="No notification channels configured." />

      <FormDialog open={showCreate} title="Create Notification" onClose={() => setShowCreate(false)} onSubmit={handleCreate} loading={creating}>
        <label>Channel<select value={channel} onChange={(e) => setChannel(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}><option value="webhook">Webhook</option><option value="slack">Slack</option><option value="email">Email</option></select></label>
        <label>Target<input value={target} onChange={(e) => setTarget(e.target.value)} placeholder="https://hooks.example.com/..." style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      </FormDialog>

      <ConfirmDialog open={!!deleteTarget} title="Delete Notification" message={`Delete notification "${String(deleteTarget?.channel ?? "")}: ${String(deleteTarget?.target ?? "")}"?`} destructive onConfirm={handleDelete} onCancel={() => setDeleteTarget(null)} loading={deleting} />
    </div>
  );
}
