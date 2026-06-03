import { useEffect, useState } from "react";
import { api, type ItemMap } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { FormDialog } from "../../components/FormDialog";
import { ConfirmDialog } from "../../components/ConfirmDialog";

export function OrganizationAdminPage() {
  const { session } = useAuth();
  const toast = useToast();

  const [members, setMembers] = useState<ItemMap[]>([]);
  const [invitations, setInvitations] = useState<ItemMap[]>([]);
  const [apiKeys, setAPIKeys] = useState<ItemMap[]>([]);

  const [showInvite, setShowInvite] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState("member");
  const [inviting, setInviting] = useState(false);

  const [showCreateKey, setShowCreateKey] = useState(false);
  const [keyName, setKeyName] = useState("");
  const [creatingKey, setCreatingKey] = useState(false);

  const [deleteInvite, setDeleteInvite] = useState<ItemMap | null>(null);
  const [deleteKey, setDeleteKey] = useState<ItemMap | null>(null);

  const orgId = session?.orgId ?? "";

  async function refresh() {
    if (!session || !orgId) return;
    const [m, i, k] = await Promise.all([
      api.listOrganizationMembers(session, orgId),
      api.listOrganizationInvitations(session, orgId),
      api.listAPIKeys(session, orgId),
    ]);
    setMembers(m.items);
    setInvitations(i.items);
    setAPIKeys(k.items);
  }

  useEffect(() => { refresh(); }, [orgId]); // eslint-disable-line react-hooks/exhaustive-deps

  const memberColumns: Column[] = [
    { key: "email", label: "Email" },
    { key: "role", label: "Role" },
    { key: "createdAt", label: "Joined", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
    {
      key: "userId",
      label: "",
      render: (_v, row) => (
        <select
          value={String(row.role ?? "member")}
          onChange={async (e) => {
            try {
              await api.updateOrganizationMemberRole(session!, orgId, String(row.userId), { role: e.target.value as "owner" | "admin" | "member" });
              toast.success("Role updated");
              await refresh();
            } catch (err) {
              toast.error((err as Error).message);
            }
          }}
          style={{ fontSize: 12, padding: "2px 4px" }}
        >
          <option value="owner">owner</option>
          <option value="admin">admin</option>
          <option value="member">member</option>
        </select>
      ),
    },
  ];

  const invitationColumns: Column[] = [
    { key: "email", label: "Email" },
    { key: "role", label: "Role" },
    { key: "status", label: "Status" },
    { key: "expiresAt", label: "Expires", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
    { key: "id", label: "", render: (_v, row) => (
      <div style={{ display: "flex", gap: 8 }}>
        {String(row.status ?? "") === "pending" && (
          <button onClick={async () => {
            if (!session) return;
            try {
              await api.resendOrganizationInvitation(session, orgId, String(row.id));
              toast.success("Invitation resent");
              await refresh();
            } catch (err) {
              toast.error((err as Error).message);
            }
          }} style={{ fontSize: 12 }}>Resend</button>
        )}
        {String(row.status ?? "") === "pending" && (
          <button onClick={async () => {
            if (!session) return;
            try {
              await api.revokeOrganizationInvitation(session, orgId, String(row.id));
              toast.success("Invitation revoked");
              await refresh();
            } catch (err) {
              toast.error((err as Error).message);
            }
          }} style={{ color: "var(--warning-text)", fontSize: 12 }}>Revoke</button>
        )}
        <button onClick={() => setDeleteInvite(row)} style={{ color: "var(--danger-text)", fontSize: 12 }}>Delete</button>
      </div>
    )},
  ];

  const apiKeyColumns: Column[] = [
    { key: "name", label: "Name" },
    { key: "lastUsedAt", label: "Last Used", render: (v) => (v ? new Date(String(v)).toLocaleString() : "Never") },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
    { key: "id", label: "", render: (_v, row) => (
      <div style={{ display: "flex", gap: 8 }}>
        <button onClick={async () => {
          if (!session) return;
          try {
            const result = await api.regenerateAPIKey(session, orgId, String(row.id));
            toast.success(`Key regenerated: ${String(result.token).slice(0, 12)}...`);
            await refresh();
          } catch (err) {
            toast.error((err as Error).message);
          }
        }} style={{ fontSize: 12 }}>Regenerate</button>
        <button onClick={() => setDeleteKey(row)} style={{ color: "var(--danger-text)", fontSize: 12 }}>Delete</button>
      </div>
    )},
  ];

  async function handleInvite() {
    if (!session || !inviteEmail) return;
    setInviting(true);
    try {
      await api.createOrganizationInvitation(session, orgId, { email: inviteEmail, role: inviteRole as "admin" | "member" });
      toast.success("Invitation sent");
      setShowInvite(false);
      setInviteEmail("");
      await refresh();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setInviting(false);
    }
  }

  async function handleCreateKey() {
    if (!session || !keyName) return;
    setCreatingKey(true);
    try {
      const result = await api.createAPIKey(session, orgId, { name: keyName });
      toast.success(`API key created: ${String(result.token ?? result.key ?? "").slice(0, 12)}...`);
      setShowCreateKey(false);
      setKeyName("");
      await refresh();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCreatingKey(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <h2 style={{ margin: 0 }}>Organization Admin</h2>

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <h3 style={{ margin: "0 0 12px" }}>Members</h3>
        <DataTable columns={memberColumns} rows={members} emptyMessage="No members." />
      </section>

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
          <h3 style={{ margin: 0 }}>Invitations</h3>
          <button onClick={() => setShowInvite(true)} style={{ padding: "4px 12px" }}>Invite Member</button>
        </div>
        <DataTable columns={invitationColumns} rows={invitations} emptyMessage="No pending invitations." />
      </section>

      <section style={{ border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16, background: "var(--bg-secondary)" }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
          <h3 style={{ margin: 0 }}>API Keys</h3>
          <button onClick={() => setShowCreateKey(true)} style={{ padding: "4px 12px" }}>Create API Key</button>
        </div>
        <DataTable columns={apiKeyColumns} rows={apiKeys} emptyMessage="No API keys." />
      </section>

      <FormDialog open={showInvite} title="Invite Member" onClose={() => setShowInvite(false)} onSubmit={handleInvite} loading={inviting}>
        <label>Email<input value={inviteEmail} onChange={(e) => setInviteEmail(e.target.value)} placeholder="user@example.com" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
        <label>Role<select value={inviteRole} onChange={(e) => setInviteRole(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}><option value="member">Member</option><option value="admin">Admin</option><option value="owner">Owner</option></select></label>
      </FormDialog>

      <FormDialog open={showCreateKey} title="Create API Key" onClose={() => setShowCreateKey(false)} onSubmit={handleCreateKey} loading={creatingKey}>
        <label>Name<input value={keyName} onChange={(e) => setKeyName(e.target.value)} placeholder="my-api-key" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      </FormDialog>

      <ConfirmDialog
        open={!!deleteInvite}
        title="Delete Invitation"
        message={`Delete invitation for "${String(deleteInvite?.email ?? "")}"?`}
        destructive
        onConfirm={async () => {
          if (!session || !deleteInvite) return;
          try {
            await api.deleteOrganizationInvitation(session, orgId, String(deleteInvite.id));
            toast.success("Invitation deleted");
            setDeleteInvite(null);
            await refresh();
          } catch (err) {
            toast.error((err as Error).message);
          }
        }}
        onCancel={() => setDeleteInvite(null)}
      />

      <ConfirmDialog
        open={!!deleteKey}
        title="Delete API Key"
        message={`Delete API key "${String(deleteKey?.name ?? "")}"?`}
        destructive
        onConfirm={async () => {
          if (!session || !deleteKey) return;
          try {
            await api.deleteAPIKey(session, orgId, String(deleteKey.id));
            toast.success("API key deleted");
            setDeleteKey(null);
            await refresh();
          } catch (err) {
            toast.error((err as Error).message);
          }
        }}
        onCancel={() => setDeleteKey(null)}
      />
    </div>
  );
}
