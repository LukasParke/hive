import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";

export function DatabaseCreatePage() {
  const { session } = useAuth();
  const { dashboard, refreshDatabaseServices } = useAppData();
  const toast = useToast();
  const navigate = useNavigate();

  const [projectId, setProjectId] = useState(String(dashboard.projects[0]?.id ?? ""));
  const [engine, setEngine] = useState("postgres");
  const [name, setName] = useState("");
  const [version, setVersion] = useState("");
  const [username, setUsername] = useState("postgres");
  const [passwordSecret, setPasswordSecret] = useState("");
  const [dbName, setDbName] = useState("");
  const [creating, setCreating] = useState(false);

  async function handleCreate() {
    if (!session || !projectId || !name) return;
    setCreating(true);
    try {
      const res = await api.createDatabaseService(session, {
        projectId,
        engine,
        name,
        ...(version && { version }),
        ...(username && { username }),
        ...(passwordSecret && { passwordSecretName: passwordSecret }),
        ...(dbName && { databaseName: dbName }),
      });
      toast.success("Database service created");
      await refreshDatabaseServices();
      navigate(`/dashboard/services/database/${res.id}`);
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCreating(false);
    }
  }

  return (
    <div style={{ maxWidth: 500, display: "grid", gap: 16 }}>
      <h2 style={{ margin: 0 }}>Create Database Service</h2>

      <label>
        Project
        <select value={projectId} onChange={(e) => setProjectId(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}>
          {dashboard.projects.map((p) => <option key={String(p.id)} value={String(p.id)}>{String(p.name)}</option>)}
        </select>
      </label>

      <label>
        Engine
        <select value={engine} onChange={(e) => setEngine(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}>
          <option value="postgres">PostgreSQL</option>
          <option value="mysql">MySQL</option>
          <option value="redis">Redis</option>
          <option value="mongo">MongoDB</option>
        </select>
      </label>

      <label>Name<input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-database" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      <label>Version (optional)<input value={version} onChange={(e) => setVersion(e.target.value)} placeholder="16" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      <label>Username<input value={username} onChange={(e) => setUsername(e.target.value)} placeholder="postgres" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      <label>Password Secret Name<input value={passwordSecret} onChange={(e) => setPasswordSecret(e.target.value)} placeholder="db-password" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>
      <label>Database Name<input value={dbName} onChange={(e) => setDbName(e.target.value)} placeholder="mydb" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} /></label>

      <button onClick={handleCreate} disabled={creating} style={{ padding: "8px 20px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, fontWeight: 600, cursor: creating ? "wait" : "pointer" }}>
        {creating ? "Creating..." : "Create Database Service"}
      </button>
    </div>
  );
}
