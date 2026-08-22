import { useEffect, useState } from "react";
import { api, type ItemMap } from "../../../api/client";
import { useAuth } from "../../../contexts/AuthContext";
import { useToast } from "../../../contexts/ToastContext";
import { LoadingState } from "../../../components/LoadingState";

export function GlobalSettingsPage() {
  const { session } = useAuth();
  const toast = useToast();

  const [, setSettings] = useState<ItemMap | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [editJson, setEditJson] = useState("");

  useEffect(() => {
    if (!session) return;
    api.getSettings(session).then((res) => {
      setSettings(res);
      setEditJson(JSON.stringify(res, null, 2));
    }).catch((err) => {
      toast.error((err as Error).message);
    }).finally(() => setLoading(false));
  }, [session]); // eslint-disable-line react-hooks/exhaustive-deps

  if (loading) return <LoadingState />;

  async function handleSave() {
    if (!session) return;
    setSaving(true);
    try {
      const parsed = JSON.parse(editJson) as ItemMap;
      await api.putSettings(session, parsed);
      setSettings(parsed);
      toast.success("Settings saved");
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <h2 style={{ margin: 0 }}>Global Settings</h2>
      <textarea
        value={editJson}
        onChange={(e) => setEditJson(e.target.value)}
        rows={20}
        style={{ fontFamily: "monospace", fontSize: 13, padding: 12, borderRadius: 6, border: "1px solid var(--border-input)" }}
      />
      <button onClick={handleSave} disabled={saving} style={{ padding: "8px 20px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: saving ? "wait" : "pointer", justifySelf: "start" }}>
        {saving ? "Saving..." : "Save Settings"}
      </button>
    </div>
  );
}
