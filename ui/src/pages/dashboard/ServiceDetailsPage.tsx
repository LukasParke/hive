import { useEffect, useState } from "react";
import { useParams, Link, useNavigate } from "react-router-dom";
import { api, type ItemMap, type Session } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useToast } from "../../contexts/ToastContext";
import { useAppData } from "../../contexts/AppContext";
import { DataTable, type Column } from "../../components/DataTable";
import { StatusBadge } from "../../components/StatusBadge";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { FormDialog } from "../../components/FormDialog";
import { LoadingState } from "../../components/LoadingState";
import { Terminal } from "../../components/Terminal";
import { LogViewer } from "../../components/LogViewer";

// ── Application Detail ──

export function ApplicationDetailPage() {
  const { id = "" } = useParams();
  const { session } = useAuth();
  const toast = useToast();
  const { refreshApplications } = useAppData();

  const [app, setApp] = useState<ItemMap | null>(null);
  const [deployments, setDeployments] = useState<ItemMap[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState("general");
  const [confirmAction, setConfirmAction] = useState<{ action: string; fn: () => Promise<void> } | null>(null);
  const [actionLoading, setActionLoading] = useState(false);

  // Edit state
  const [editName, setEditName] = useState("");
  const [editImage, setEditImage] = useState("");
  const [editPort, setEditPort] = useState("");
  const [editRepoUrl, setEditRepoUrl] = useState("");
  const [editGitRef, setEditGitRef] = useState("");
  const [saving, setSaving] = useState(false);

  // Domain state
  const [domains, setDomains] = useState<ItemMap[]>([]);
  const [showAddDomain, setShowAddDomain] = useState(false);
  const [domainHostname, setDomainHostname] = useState("");

  // Env var state
  const [envVars, setEnvVars] = useState<ItemMap[]>([]);
  const [showAddEnvVar, setShowAddEnvVar] = useState(false);
  const [envKey, setEnvKey] = useState("");
  const [envValue, setEnvValue] = useState("");
  const [envIsSecret, setEnvIsSecret] = useState(false);
  const [editingVarId, setEditingVarId] = useState<string | null>(null);
  const [editingVarValue, setEditingVarValue] = useState("");
  const [editingVarIsSecret, setEditingVarIsSecret] = useState(false);

  // Terminal/Log state
  const [showTerminal, setShowTerminal] = useState(false);
  const [showLogs, setShowLogs] = useState(false);

  useEffect(() => {
    if (!id || !session) return;
    let cancelled = false;
    (async () => {
      try {
        const [a, d] = await Promise.all([api.getApplication(session, id), api.listApplicationDeployments(session, id)]);
        if (!cancelled) {
          setApp(a);
          setDeployments(d.items);
          setEditName(String(a.name ?? ""));
          setEditImage(String(a.image ?? ""));
          setEditPort(String(a.containerPort ?? ""));
          setEditRepoUrl(String(a.repositoryUrl ?? ""));
          setEditGitRef(String(a.gitRef ?? ""));
        }
      } catch (err) {
        if (!cancelled) toast.error((err as Error).message);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [id, session]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!session || activeTab !== "domains") return;
    api.listDomains(session).then((res) => {
      setDomains(res.items.filter((d) => d.applicationId === id));
    }).catch(() => {});
  }, [session, activeTab, id]);

  useEffect(() => {
    if (!session || activeTab !== "environment") return;
    api.listAppEnvVars(session, id).then((res) => {
      setEnvVars(res.items);
    }).catch(() => {});
  }, [session, activeTab, id]);

  if (loading) return <LoadingState />;
  if (!app || !session) return <p>Application not found.</p>;

  const containerID = String(app.serviceName ?? app.name ?? id);

  async function runAction(action: string, fn: () => Promise<void>) {
    setActionLoading(true);
    try {
      await fn();
      toast.success(action === "Deploy" || action === "Rollback" ? `${action} queued` : `${action} successful`);
      const [a, d] = await Promise.all([api.getApplication(session!, id), api.listApplicationDeployments(session!, id)]);
      setApp(a);
      setDeployments(d.items);
      await refreshApplications();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setActionLoading(false);
      setConfirmAction(null);
    }
  }

  async function handleSave() {
    setSaving(true);
    try {
      const payload: ItemMap = { name: editName };
      if (editImage) payload.image = editImage;
      if (editPort) payload.containerPort = parseInt(editPort, 10);
      if (editRepoUrl) payload.repositoryUrl = editRepoUrl;
      if (editGitRef) payload.gitRef = editGitRef;
      const updated = await api.updateApplication(session!, id, payload);
      setApp(updated);
      toast.success("Application updated");
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSaving(false);
    }
  }

  async function handleAddDomain() {
    if (!domainHostname) return;
    try {
      await api.createDomain(session!, { applicationId: id, hostname: domainHostname, tlsEnabled: true });
      toast.success("Domain added");
      setShowAddDomain(false);
      setDomainHostname("");
      const res = await api.listDomains(session!);
      setDomains(res.items.filter((d) => d.applicationId === id));
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  async function handleDeleteDomain(domainId: string) {
    try {
      await api.deleteDomain(session!, domainId);
      setDomains((prev) => prev.filter((d) => d.id !== domainId));
      toast.success("Domain deleted");
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  async function handleAddEnvVar() {
    if (!envKey) return;
    try {
      await api.createAppEnvVar(session!, id, { key: envKey, value: envValue, isSecret: envIsSecret });
      toast.success("Variable added");
      setShowAddEnvVar(false);
      setEnvKey("");
      setEnvValue("");
      setEnvIsSecret(false);
      const res = await api.listAppEnvVars(session!, id);
      setEnvVars(res.items);
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  async function handleUpdateEnvVar() {
    if (!editingVarId || !editingVarValue) return;
    try {
      await api.updateAppEnvVar(session!, id, editingVarId, { value: editingVarValue });
      toast.success("Variable updated");
      setEditingVarId(null);
      setEditingVarValue("");
      setEditingVarIsSecret(false);
      const res = await api.listAppEnvVars(session!, id);
      setEnvVars(res.items);
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  async function handleDeleteEnvVar(varId: string) {
    try {
      await api.deleteAppEnvVar(session!, id, varId);
      setEnvVars((prev) => prev.filter((v) => v.id !== varId));
      toast.success("Variable deleted");
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const wsBase = `${protocol}//${window.location.host}`;
  const token = encodeURIComponent(session.accessToken);

  const tabs = ["general", "deployments", "domains", "environment", "logs", "terminal"];

  const deploymentColumns: Column[] = [
    { key: "imageTag", label: "Image Tag", render: (v) => String(v ?? "-") },
    { key: "status", label: "Status", render: (v) => <StatusBadge status={String(v ?? "")} /> },
    { key: "trigger", label: "Trigger", render: (v) => String(v ?? "-") },
    { key: "createdAt", label: "Date", render: (v) => (v ? new Date(String(v)).toLocaleString() : "") },
  ];

  const domainColumns: Column[] = [
    { key: "hostname", label: "Hostname" },
    { key: "tlsEnabled", label: "TLS", render: (v) => (v ? "Yes" : "No") },
    { key: "id", label: "", render: (_v, row) => <button onClick={(e) => { e.stopPropagation(); handleDeleteDomain(String(row.id)); }} style={{ color: "var(--danger-text)", fontSize: 12 }}>Delete</button> },
  ];

  return (
    <div style={{ display: "grid", gap: 16 }}>
      {/* Header */}
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <Link to="/dashboard/projects" style={{ color: "var(--text-secondary)" }}>Projects</Link>
        <span style={{ color: "var(--text-faint)" }}>/</span>
        <h2 style={{ margin: 0 }}>{String(app.name)}</h2>
        <StatusBadge status={String(app.status ?? "")} />
        <span style={{ fontSize: 12, color: "var(--text-faint)", background: "var(--bg-tertiary)", padding: "2px 8px", borderRadius: 4 }}>{String(app.sourceType)}</span>
      </div>

      {/* Actions */}
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        {[
          { label: "Deploy", action: "Deploy", fn: () => api.deployApplication(session!, id) },
          { label: "Start", action: "Start", fn: () => api.startApplication(session!, id) },
          { label: "Stop", action: "Stop", fn: () => api.stopApplication(session!, id) },
          { label: "Restart", action: "Restart", fn: () => api.restartApplication(session!, id) },
          { label: "Rollback", action: "Rollback", fn: () => api.rollbackApplication(session!, id) },
        ].map((btn) => (
          <button key={btn.label} onClick={() => setConfirmAction({ action: btn.action, fn: async () => { await btn.fn(); } })} style={{ padding: "6px 14px" }}>
            {btn.label}
          </button>
        ))}
      </div>

      {/* Tabs */}
      <div style={{ display: "flex", gap: 0, borderBottom: "2px solid var(--border-primary)" }}>
        {tabs.map((tab) => (
          <button
            key={tab}
            onClick={() => setActiveTab(tab)}
            style={{
              padding: "8px 16px",
              border: "none",
              background: "none",
              cursor: "pointer",
              fontWeight: activeTab === tab ? 600 : 400,
              borderBottom: activeTab === tab ? "2px solid var(--gold-500)" : "2px solid transparent",
              marginBottom: -2,
              textTransform: "capitalize",
            }}
          >
            {tab}
          </button>
        ))}
      </div>

      {/* Tab content */}
      {activeTab === "general" && (
        <div style={{ display: "grid", gap: 12, maxWidth: 500 }}>
          <label>Name<input value={editName} onChange={(e) => setEditName(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
          <label>Image<input value={editImage} onChange={(e) => setEditImage(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
          <label>Repository URL<input value={editRepoUrl} onChange={(e) => setEditRepoUrl(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
          <label>Git Ref<input value={editGitRef} onChange={(e) => setEditGitRef(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
          <label>Container Port<input value={editPort} onChange={(e) => setEditPort(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
          <button onClick={handleSave} disabled={saving} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, cursor: "pointer", justifySelf: "start", fontWeight: 600 }}>
            {saving ? "Saving..." : "Save Changes"}
          </button>
        </div>
      )}

      {activeTab === "deployments" && <DataTable columns={deploymentColumns} rows={deployments} emptyMessage="No deployments yet." />}

      {activeTab === "domains" && (
        <div>
          <button onClick={() => setShowAddDomain(true)} style={{ marginBottom: 12, padding: "6px 14px" }}>Add Domain</button>
          <DataTable columns={domainColumns} rows={domains} emptyMessage="No domains configured." />
        </div>
      )}

      {activeTab === "environment" && (
        <div>
          <button onClick={() => setShowAddEnvVar(true)} style={{ marginBottom: 12, padding: "6px 14px" }}>Add Variable</button>
          <DataTable
            columns={[
              { key: "key", label: "Key" },
              { key: "value", label: "Value", render: (v, row) => row.isSecret ? "••••••••" : String(v ?? "") },
              { key: "isSecret", label: "Type", render: (v) => <StatusBadge status={v ? "secret" : "plain"} /> },
              {
                key: "id", label: "", render: (_v, row) => (
                  <span style={{ display: "flex", gap: 8 }}>
                    <button onClick={(e) => { e.stopPropagation(); setEditingVarId(String(row.id)); setEditingVarValue(""); setEditingVarIsSecret(!!row.isSecret); }} style={{ fontSize: 12 }}>Edit</button>
                    <button onClick={(e) => { e.stopPropagation(); handleDeleteEnvVar(String(row.id)); }} style={{ color: "var(--danger-text)", fontSize: 12 }}>Delete</button>
                  </span>
                ),
              },
            ] as Column[]}
            rows={envVars}
            emptyMessage="No environment variables configured."
          />
        </div>
      )}

      {activeTab === "logs" && (
        <div>
          {showLogs ? (
            <LogViewer websocketUrl={`${wsBase}/api/v1/ws/logs/${containerID}?access_token=${token}&follow=true&tail=200`} />
          ) : (
            <button onClick={() => setShowLogs(true)} style={{ padding: "8px 20px" }}>Connect to Logs</button>
          )}
        </div>
      )}

      {activeTab === "terminal" && (
        <div>
          {showTerminal ? (
            <Terminal websocketUrl={`${wsBase}/api/v1/ws/terminal/${containerID}?access_token=${token}&shell=/bin/sh`} onClose={() => setShowTerminal(false)} />
          ) : (
            <button onClick={() => setShowTerminal(true)} style={{ padding: "8px 20px" }}>Open Terminal</button>
          )}
        </div>
      )}

      {/* Dialogs */}
      <ConfirmDialog
        open={!!confirmAction}
        title={confirmAction?.action ?? ""}
        message={`Are you sure you want to ${confirmAction?.action.toLowerCase()} this application?`}
        onConfirm={() => confirmAction && runAction(confirmAction.action, confirmAction.fn)}
        onCancel={() => setConfirmAction(null)}
        loading={actionLoading}
      />

      <FormDialog open={showAddDomain} title="Add Domain" onClose={() => setShowAddDomain(false)} onSubmit={handleAddDomain}>
        <label>
          Hostname
          <input value={domainHostname} onChange={(e) => setDomainHostname(e.target.value)} placeholder="app.example.com" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} />
        </label>
      </FormDialog>

      <FormDialog open={showAddEnvVar} title="Add Variable" onClose={() => { setShowAddEnvVar(false); setEnvKey(""); setEnvValue(""); setEnvIsSecret(false); }} onSubmit={handleAddEnvVar}>
        <label>
          Key
          <input value={envKey} onChange={(e) => setEnvKey(e.target.value)} placeholder="MY_VAR" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} />
        </label>
        <label style={{ marginTop: 8, display: "block" }}>
          Value
          <textarea value={envValue} onChange={(e) => setEnvValue(e.target.value)} rows={3} style={{ width: "100%", padding: "6px 10px", marginTop: 4, fontFamily: "monospace" }} />
        </label>
        <label style={{ marginTop: 8, display: "flex", alignItems: "center", gap: 8 }}>
          <input type="checkbox" checked={envIsSecret} onChange={(e) => setEnvIsSecret(e.target.checked)} />
          Secret (stored as Docker secret, write-only)
        </label>
      </FormDialog>

      <FormDialog open={!!editingVarId} title={editingVarIsSecret ? "Set New Secret Value" : "Edit Variable"} onClose={() => { setEditingVarId(null); setEditingVarValue(""); setEditingVarIsSecret(false); }} onSubmit={handleUpdateEnvVar}>
        <label>
          Value
          <textarea value={editingVarValue} onChange={(e) => setEditingVarValue(e.target.value)} rows={3} placeholder={editingVarIsSecret ? "Enter new secret value" : ""} style={{ width: "100%", padding: "6px 10px", marginTop: 4, fontFamily: "monospace" }} />
        </label>
      </FormDialog>
    </div>
  );
}

// ── Stack Detail ──

export function StackDetailPage() {
  const { id = "" } = useParams();
  const { session } = useAuth();
  const toast = useToast();

  const [stack, setStack] = useState<ItemMap | null>(null);
  const [loading, setLoading] = useState(true);
  const [compose, setCompose] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmAction, setConfirmAction] = useState<{ action: string; fn: () => Promise<void> } | null>(null);
  const [actionLoading, setActionLoading] = useState(false);

  useEffect(() => {
    if (!id || !session) return;
    let cancelled = false;
    api.getStack(session, id).then((res) => {
      if (!cancelled) {
        setStack(res);
        setCompose(String(res.composeContent ?? ""));
      }
    }).catch((err) => {
      if (!cancelled) toast.error((err as Error).message);
    }).finally(() => {
      if (!cancelled) setLoading(false);
    });
    return () => { cancelled = true; };
  }, [id, session]); // eslint-disable-line react-hooks/exhaustive-deps

  if (loading) return <LoadingState />;
  if (!stack || !session) return <p>Stack not found.</p>;

  async function runAction(action: string, fn: () => Promise<void>) {
    setActionLoading(true);
    try {
      await fn();
      toast.success(`${action} successful`);
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setActionLoading(false);
      setConfirmAction(null);
    }
  }

  async function handleSave() {
    setSaving(true);
    try {
      await api.updateStack(session!, id, { composeContent: compose });
      toast.success("Stack updated");
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <Link to="/dashboard/stacks" style={{ color: "var(--text-secondary)" }}>Stacks</Link>
        <span style={{ color: "var(--text-faint)" }}>/</span>
        <h2 style={{ margin: 0 }}>{String(stack.name)}</h2>
      </div>

      <div style={{ display: "flex", gap: 8 }}>
        {["Deploy", "Start", "Stop", "Restart"].map((action) => (
          <button
            key={action}
            onClick={() => setConfirmAction({
              action,
              fn: async () => {
                const fn = { Deploy: api.deployStack, Start: api.startStack, Stop: api.stopStack, Restart: api.restartStack }[action]!;
                await fn(session!, id);
              },
            })}
            style={{ padding: "6px 14px" }}
          >
            {action}
          </button>
        ))}
      </div>

      <div style={{ display: "grid", gap: 8 }}>
        <label style={{ fontWeight: 600 }}>Compose Content</label>
        <textarea value={compose} onChange={(e) => setCompose(e.target.value)} rows={16} style={{ fontFamily: "monospace", fontSize: 13, padding: 12, borderRadius: 6, border: "1px solid var(--border-input)" }} />
        <button onClick={handleSave} disabled={saving} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, cursor: "pointer", justifySelf: "start", fontWeight: 600 }}>
          {saving ? "Saving..." : "Save Changes"}
        </button>
      </div>

      <ConfirmDialog
        open={!!confirmAction}
        title={confirmAction?.action ?? ""}
        message={`Are you sure you want to ${confirmAction?.action.toLowerCase()} this stack?`}
        onConfirm={() => confirmAction && runAction(confirmAction.action, confirmAction.fn)}
        onCancel={() => setConfirmAction(null)}
        loading={actionLoading}
      />
    </div>
  );
}

// ── Database Service Detail ──

export function DatabaseServiceDetailPage() {
  const { id = "" } = useParams();
  const { session } = useAuth();
  const toast = useToast();
  const navigate = useNavigate();
  const { refreshDatabaseServices } = useAppData();

  const [db, setDB] = useState<ItemMap | null>(null);
  const [loading, setLoading] = useState(true);
  const [backups, setBackups] = useState<ItemMap[]>([]);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    if (!id || !session) return;
    let cancelled = false;
    Promise.all([api.getDatabaseService(session, id), api.listBackups(session)]).then(([d, b]) => {
      if (!cancelled) {
        setDB(d);
        setBackups(b.items.filter((bk) => bk.targetId === id));
      }
    }).catch((err) => {
      if (!cancelled) toast.error((err as Error).message);
    }).finally(() => {
      if (!cancelled) setLoading(false);
    });
    return () => { cancelled = true; };
  }, [id, session]); // eslint-disable-line react-hooks/exhaustive-deps

  if (loading) return <LoadingState />;
  if (!db || !session) return <p>Database service not found.</p>;

  async function handleDeleteDatabase() {
    setDeleting(true);
    try {
      await api.deleteDatabaseService(session!, id);
      toast.success("Database service deleted");
      await refreshDatabaseServices();
      navigate("/dashboard/runtime");
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleting(false);
      setConfirmDelete(false);
    }
  }

  async function handleCreateBackup() {
    try {
      await api.createBackup(session!, { targetType: "database", targetId: id });
      toast.success("Backup queued");
      const res = await api.listBackups(session!);
      setBackups(res.items.filter((bk) => bk.targetId === id));
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  const backupColumns: Column[] = [
    { key: "id", label: "ID", render: (v) => String(v ?? "").slice(0, 8) },
    { key: "status", label: "Status", render: (v) => <StatusBadge status={String(v ?? "")} /> },
    { key: "createdAt", label: "Date", render: (v) => (v ? new Date(String(v)).toLocaleString() : "") },
  ];

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 12 }}>
        <h2 style={{ margin: 0 }}>{String(db.name)}</h2>
        <button onClick={() => setConfirmDelete(true)} style={{ padding: "6px 14px", color: "var(--danger-text)" }}>Delete Database</button>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))", gap: 16 }}>
        <section style={{ background: "var(--bg-secondary)", border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16 }}>
          <h3 style={{ margin: "0 0 12px" }}>Info</h3>
          {[
            ["Engine", db.engine],
            ["Version", db.version],
            ["Port", db.port],
            ["Database", db.databaseName],
            ["Service Name", db.serviceName],
            ["Created", db.createdAt ? new Date(String(db.createdAt)).toLocaleString() : ""],
          ].map(([label, value]) => (
            <div key={String(label)} style={{ display: "flex", justifyContent: "space-between", padding: "4px 0", borderBottom: "1px solid var(--border-primary)", fontSize: 14 }}>
              <span style={{ color: "var(--text-secondary)" }}>{String(label)}</span>
              <span>{String(value ?? "-")}</span>
            </div>
          ))}
        </section>

        <section style={{ background: "var(--bg-secondary)", border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16 }}>
          <h3 style={{ margin: "0 0 12px" }}>Connection</h3>
          <pre style={{ background: "var(--code-bg)", color: "var(--text-primary)", border: "1px solid var(--border-primary)", padding: 12, borderRadius: 6, fontSize: 13, overflow: "auto" }}>
            {`Host: ${db.serviceName ?? db.name}\nPort: ${db.port ?? "5432"}\nDatabase: ${db.databaseName ?? ""}\nUsername: ${db.username ?? "postgres"}`}
          </pre>
        </section>
      </div>

      <section style={{ background: "var(--bg-secondary)", border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
          <h3 style={{ margin: 0 }}>Backups</h3>
          <button onClick={handleCreateBackup} style={{ padding: "6px 14px" }}>Create Backup</button>
        </div>
        <DataTable columns={backupColumns} rows={backups} emptyMessage="No backups yet." />
      </section>

      <ConfirmDialog
        open={confirmDelete}
        title="Delete Database Service"
        message={`Delete ${String(db.name)} and remove its Swarm service? Database volume cleanup depends on the configured Docker volume lifecycle.`}
        destructive
        onConfirm={handleDeleteDatabase}
        onCancel={() => setConfirmDelete(false)}
        loading={deleting}
      />
    </div>
  );
}
