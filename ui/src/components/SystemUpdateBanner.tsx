import { useEffect, useState } from "react";
import { api, type UpdateStatus } from "../api/client";
import { useAuth } from "../contexts/AuthContext";
import { useToast } from "../contexts/ToastContext";

export function SystemUpdateBanner() {
  const { session } = useAuth();
  const toast = useToast();
  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [updating, setUpdating] = useState(false);

  useEffect(() => {
    if (!session) return;
    let cancelled = false;

    const check = async () => {
      try {
        const s = await api.getUpdateStatus(session);
        if (!cancelled) setStatus(s);
      } catch {
        // ignore
      }
    };

    check();
    const id = setInterval(check, 60000); // check every minute
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [session]);

  if (!status?.updateAvailable) return null;

  async function handleUpdate() {
    if (!session) return;
    setUpdating(true);
    try {
      await api.triggerUpdate(session);
      toast.success("Update triggered — Swarm will roll out the new version shortly.");
      setStatus((prev) => (prev ? { ...prev, updateAvailable: false } : prev));
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setUpdating(false);
    }
  }

  return (
    <div
      style={{
        background: "var(--gold-alpha-10)",
        border: "1px solid var(--gold-500)",
        borderRadius: 4,
        padding: "10px 14px",
        display: "flex",
        justifyContent: "space-between",
        alignItems: "center",
        gap: 12,
        marginBottom: 12,
      }}
    >
      <div style={{ fontSize: 13, color: "var(--text-primary)" }}>
        <strong>Update available:</strong> {status.latestVersion} (current: {status.currentVersion})
      </div>
      <button
        onClick={handleUpdate}
        disabled={updating}
        style={{
          padding: "4px 12px",
          fontSize: 12,
          fontWeight: 600,
          background: "var(--gold-500)",
          color: "var(--text-on-gold)",
          border: "none",
          borderRadius: 4,
          cursor: updating ? "not-allowed" : "pointer",
          opacity: updating ? 0.7 : 1,
        }}
      >
        {updating ? "Updating…" : "Update Now"}
      </button>
    </div>
  );
}
