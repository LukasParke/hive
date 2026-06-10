import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, type ItemMap, type components } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { FormDialog } from "../../components/FormDialog";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { StatusBadge } from "../../components/StatusBadge";

export function ProjectsPage() {
  const { session } = useAuth();
  const { dashboard, refreshProjects, refreshApplications } = useAppData();
  const toast = useToast();
  const navigate = useNavigate();

  const [showCreateProject, setShowCreateProject] = useState(false);
  const [projectName, setProjectName] = useState("");
  const [creating, setCreating] = useState(false);

  const [showCreateApp, setShowCreateApp] = useState(false);
  const [appProjectId, setAppProjectId] = useState("");
  const [appName, setAppName] = useState("");
  const [appSourceType, setAppSourceType] = useState("image");
  const [appImage, setAppImage] = useState("");
  const [appRepoUrl, setAppRepoUrl] = useState("");
  const [appPort, setAppPort] = useState("80");
  const [creatingApp, setCreatingApp] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<ItemMap | null>(null);
  const [deleting, setDeleting] = useState(false);

  const projectColumns: Column[] = [
    { key: "name", label: "Name" },
    {
      key: "id",
      label: "Applications",
      render: (_v, row) => {
        const count = dashboard.applications.filter((a) => a.projectId === row.id).length;
        return `${count} app${count !== 1 ? "s" : ""}`;
      },
    },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
  ];

  const appColumns: Column[] = [
    { key: "name", label: "Name" },
    { key: "sourceType", label: "Source" },
    { key: "image", label: "Image", render: (v) => String(v ?? "-") },
    { key: "status", label: "Status", render: (v) => <StatusBadge status={String(v ?? "")} /> },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
  ];

  async function handleCreateProject() {
    if (!session || !projectName) return;
    setCreating(true);
    try {
      await api.createProject(session, { name: projectName });
      toast.success("Project created");
      setShowCreateProject(false);
      setProjectName("");
      await refreshProjects();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCreating(false);
    }
  }

  async function handleCreateApp() {
    if (!session || !appName || !appProjectId) return;
    setCreatingApp(true);
    try {
      const payload: components["schemas"]["CreateApplicationRequest"] = {
        projectId: appProjectId,
        name: appName,
        sourceType: appSourceType as "git" | "image",
        image: appSourceType === "image" ? appImage : undefined,
        repositoryUrl: appSourceType === "git" ? appRepoUrl : undefined,
        gitRef: appSourceType === "git" ? "main" : undefined,
        containerPort: appPort ? parseInt(appPort, 10) : 8080,
        watchPaths: [],
      };
      await api.createApplication(session, payload);
      toast.success("Application created");
      setShowCreateApp(false);
      setAppName("");
      setAppImage("");
      await refreshApplications();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setCreatingApp(false);
    }
  }

  async function handleDeleteProject() {
    if (!session || !deleteTarget) return;
    setDeleting(true);
    try {
      await api.deleteProject(session, String(deleteTarget.id));
      toast.success("Project deleted");
      setDeleteTarget(null);
      await refreshProjects();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setDeleting(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Projects</h2>
        <div style={{ display: "flex", gap: 8 }}>
          <button onClick={() => setShowCreateProject(true)} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, cursor: "pointer", fontWeight: 600 }}>
            Create Project
          </button>
          <button onClick={() => { setShowCreateApp(true); setAppProjectId(String(dashboard.projects[0]?.id ?? "")); }} style={{ padding: "6px 16px" }}>
            Create Application
          </button>
        </div>
      </div>

      {dashboard.projects.map((project) => {
        const apps = dashboard.applications.filter((a) => a.projectId === project.id);
        return (
          <section key={String(project.id)} style={{ background: "var(--bg-secondary)", border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16 }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
              <h3 style={{ margin: 0 }}>{String(project.name)}</h3>
              <div style={{ display: "flex", gap: 8 }}>
                <button onClick={() => { setShowCreateApp(true); setAppProjectId(String(project.id)); }} style={{ padding: "4px 12px", fontSize: 13 }}>
                  + App
                </button>
                <button onClick={() => setDeleteTarget(project)} style={{ padding: "4px 12px", fontSize: 13, color: "var(--danger-text)" }}>
                  Delete
                </button>
              </div>
            </div>
            <DataTable
              columns={appColumns}
              rows={apps}
              loading={dashboard.loading}
              onRowClick={(row) => navigate(`/dashboard/services/application/${row.id}`)}
              emptyMessage="No applications in this project."
            />
          </section>
        );
      })}

      {dashboard.projects.length === 0 && (
        <DataTable columns={projectColumns} rows={[]} loading={dashboard.loading} emptyMessage="No projects yet. Create one to get started." />
      )}

      <FormDialog open={showCreateProject} title="Create Project" onClose={() => setShowCreateProject(false)} onSubmit={handleCreateProject} loading={creating}>
        <label>
          Name
          <input value={projectName} onChange={(e) => setProjectName(e.target.value)} placeholder="my-project" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} />
        </label>
      </FormDialog>

      <FormDialog open={showCreateApp} title="Create Application" onClose={() => setShowCreateApp(false)} onSubmit={handleCreateApp} loading={creatingApp}>
        <label>
          Project
          <select value={appProjectId} onChange={(e) => setAppProjectId(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}>
            {dashboard.projects.map((p) => (
              <option key={String(p.id)} value={String(p.id)}>{String(p.name)}</option>
            ))}
          </select>
        </label>
        <label>
          Name
          <input value={appName} onChange={(e) => setAppName(e.target.value)} placeholder="my-app" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} />
        </label>
        <label>
          Source Type
          <select value={appSourceType} onChange={(e) => setAppSourceType(e.target.value)} style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}>
            <option value="image">Image</option>
            <option value="git">Git</option>
            <option value="compose">Compose</option>
          </select>
        </label>
        {appSourceType === "image" && (
          <label>
            Image
            <input value={appImage} onChange={(e) => setAppImage(e.target.value)} placeholder="nginx:alpine" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} />
          </label>
        )}
        {appSourceType === "git" && (
          <label>
            Repository URL
            <input value={appRepoUrl} onChange={(e) => setAppRepoUrl(e.target.value)} placeholder="https://github.com/..." style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} />
          </label>
        )}
        <label>
          Container Port
          <input value={appPort} onChange={(e) => setAppPort(e.target.value)} placeholder="80" style={{ width: "100%", padding: "6px 10px", marginTop: 4 }} />
        </label>
      </FormDialog>

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete Project"
        message={`Are you sure you want to delete "${String(deleteTarget?.name ?? "")}"? This will also delete all applications in this project.`}
        destructive
        onConfirm={handleDeleteProject}
        onCancel={() => setDeleteTarget(null)}
        loading={deleting}
      />
    </div>
  );
}
