import { useEffect, useState } from "react";
import { api, type Profile } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { LoadingState } from "../../components/LoadingState";

export function ProfilePage() {
  const { session } = useAuth();
  const toast = useToast();

  const [profile, setProfile] = useState<Profile | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [changingPassword, setChangingPassword] = useState(false);

  const [displayName, setDisplayName] = useState("");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  async function load() {
    if (!session) return;
    try {
      const p = await api.getProfile(session);
      setProfile(p);
      setDisplayName(p.displayName ?? "");
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { load(); }, [session]); // eslint-disable-line react-hooks/exhaustive-deps

  async function handleSave() {
    if (!session) return;
    setSaving(true);
    try {
      const updated = await api.updateProfile(session, { displayName });
      setProfile(updated);
      toast.success("Profile updated");
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSaving(false);
    }
  }

  async function handleChangePassword() {
    if (!session) return;
    if (newPassword !== confirmPassword) {
      toast.error("New passwords do not match");
      return;
    }
    if (newPassword.length < 8) {
      toast.error("Password must be at least 8 characters");
      return;
    }
    setChangingPassword(true);
    try {
      await api.changePassword(session, { currentPassword, newPassword });
      toast.success("Password changed successfully");
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setChangingPassword(false);
    }
  }

  if (loading) return <LoadingState />;
  if (!profile) return <p>Failed to load profile.</p>;

  return (
    <div style={{ display: "grid", gap: 24, maxWidth: 560 }}>
      <h2 style={{ margin: 0 }}>Profile</h2>

      <div style={{ display: "grid", gap: 12, background: "var(--bg-secondary)", padding: 16, borderRadius: 8, border: "1px solid var(--border-primary)" }}>
        <h3 style={{ margin: 0, fontSize: 14 }}>Account Information</h3>
        <div>
          <label style={{ fontSize: 12, color: "var(--text-faint)" }}>Email</label>
          <div style={{ fontSize: 14, padding: "6px 0" }}>{profile.email}</div>
        </div>
        <div>
          <label style={{ fontSize: 12, color: "var(--text-faint)" }}>Display Name</label>
          <input type="text" value={displayName} onChange={(e) => setDisplayName(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
        </div>
        <div style={{ display: "flex", gap: 8 }}>
          <button onClick={handleSave} disabled={saving}>{saving ? "Saving..." : "Save"}</button>
        </div>
      </div>

      <div style={{ display: "grid", gap: 12, background: "var(--bg-secondary)", padding: 16, borderRadius: 8, border: "1px solid var(--border-primary)" }}>
        <h3 style={{ margin: 0, fontSize: 14 }}>Change Password</h3>
        <div>
          <label style={{ fontSize: 12, color: "var(--text-faint)" }}>Current Password</label>
          <input type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
        </div>
        <div>
          <label style={{ fontSize: 12, color: "var(--text-faint)" }}>New Password</label>
          <input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
        </div>
        <div>
          <label style={{ fontSize: 12, color: "var(--text-faint)" }}>Confirm New Password</label>
          <input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} style={{ width: "100%", marginTop: 4 }} />
        </div>
        <div style={{ display: "flex", gap: 8 }}>
          <button onClick={handleChangePassword} disabled={changingPassword}>{changingPassword ? "Changing..." : "Change Password"}</button>
        </div>
      </div>
    </div>
  );
}
