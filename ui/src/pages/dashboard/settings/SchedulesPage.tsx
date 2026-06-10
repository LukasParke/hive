import { useState } from "react";
import { api, type ItemMap } from "../../../api/client";
import { useAuth } from "../../../contexts/AuthContext";
import { useAppData } from "../../../contexts/AppContext";
import { useToast } from "../../../contexts/ToastContext";
import { DataTable, type Column } from "../../../components/DataTable";
import { FormDialog } from "../../../components/FormDialog";
import { ConfirmDialog } from "../../../components/ConfirmDialog";

export function SchedulesPage() {
  const { session } = useAuth();
  const { dashboard, refreshSchedules } = useAppData();
  const toast = useToast();

  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [cronExpr, setCronExpr] = useState("0 2 * * *");
  const [targetType, setTargetType] = useState("backup");
  const [targetId, setTargetId] = useState("");
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<ItemMap | null>(null);
  const [deleting, setDeleting] = useState(false);

  const columns: Column[] = [
    { key: "name", label: "Name" },
    { key: "cronExpr", label: "Cron" },
    { key: "targetType", label: "Target Type" },
    { key: "enabled", label: "Enabled", render: (v) => (v ? "Yes" : "No") },
    { key: "lastRunAt", label: "Last Run", render: (v) => (v ? new Date(String(v)).toLocaleString() : "Never") },
    {
      key: "id",
      label: "Actions",
      render: (_v, row) => (
        <div style={{ display: "flex", gap: 8 }}>
          <button onClick={(e) => { e.stopPropagation(); handleRunNow(String(row.id)); }} style={{ fontSize: 12 }}>Run Now</button>
          <button onClick={(e) => { e.stopPropagation(); setDeleteTarget(row); }} style={{ color: "var(--danger-text)", fontSize: 12 }}>Delete</button>
        </div>
      ),
    },
  ];

  async function handleCreate() {
    if (!session || !name || !targetId) return;
    setCreating(true);
    try {
      await api.createSchedule(session, { name, cronExpr, targetType, targetId, enabled: true });
      toast.success("Schedule created");
      setShowCreate(false);
      setName("");
      setTargetId("");
      await refreshSchedules();
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
      await api.deleteSchedule(session, String(deleteTarget.id));
      toast.success("Schedule deleted");
      setDeleteTarget(null);
      await refreshSchedules();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleting(false);
    }
  }

  async function handleRunNow(id: string) {
    if (!session) return;
    try {
      await api.runScheduleNow(session, id);
      toast.success("Schedule triggered");
      await refreshSchedules();
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Schedules</h2>
        <button onClick={() => setShowCreate(true)} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: "pointer" }}>Create Schedule</button>
      </div>
      <DataTable columns={columns} rows={dashboard.schedules} loading={dashboard.loading} emptyMessage="No schedules configured." />

      <FormDialog open={showCreate} title="Create Schedule" onClose={() => setShowCreate(false)} onSubmit={handleCreate} loading={creating}>
        <label>Name<input value={name} onChange={(e) => setName(e.target.value)} placeholder="nightly-backup" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Cron Expression<input value={cronExpr} onChange={(e) => setCronExpr(e.target.value)} placeholder="0 2 * * *" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Target Type<select value={targetType} onChange={(e) => setTargetType(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}><option value="backup">Backup</option><option value="deploy">Deploy</option></select></label>
        <label>Target ID<input value={targetId} onChange={(e) => setTargetId(e.target.value)} placeholder="Resource ID" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      </FormDialog>

      <ConfirmDialog open={!!deleteTarget} title="Delete Schedule" message={`Delete "${String(deleteTarget?.name ?? "")}"?`} destructive onConfirm={handleDelete} onCancel={() => setDeleteTarget(null)} loading={deleting} />
    </div>
  );
}
