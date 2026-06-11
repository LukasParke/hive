import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";
import { StatusBadge } from "../../components/StatusBadge";

export function OverviewPage() {
  const { session } = useAuth();
  const { dashboard, refreshAll } = useAppData();
  const toast = useToast();

  const [orgName, setOrgName] = useState("");
  const [orgSlug, setOrgSlug] = useState("");
  const [projectName, setProjectName] = useState("");

  const totalApps = dashboard.applications.length;
  const totalProjects = dashboard.projects.length;
  const totalNodes = dashboard.nodes.length;
  const totalStacks = dashboard.stacks.length;
  const runningApps = dashboard.applications.filter((app) => String(app.status ?? "").toLowerCase() === "running").length;
  const activeBuilds = dashboard.buildQueue.length;
  const hasDomain = dashboard.domains.length > 0;
  const isLoading = dashboard.loading;

  const readinessSteps = [
    { label: "Create a project", done: totalProjects > 0, to: "/dashboard/projects" },
    { label: "Deploy your first app", done: totalApps > 0, to: "/dashboard/projects" },
    { label: "Attach a domain", done: hasDomain, to: "/dashboard/domains" },
    { label: "Configure backups", done: dashboard.backupDestinations.length > 0 || dashboard.backups.length > 0, to: "/dashboard/settings/backups" },
  ];
  const completedSteps = readinessSteps.filter((step) => step.done).length;

  async function handleCreateOrg() {
    if (!session || !orgName) return;
    try {
      const slug = orgSlug || orgName.toLowerCase().replace(/[^a-z0-9]+/g, "-").slice(0, 48);
      await api.createOrganization(session, { name: orgName, slug });
      toast.success("Organization created");
      setOrgName("");
      setOrgSlug("");
      await refreshAll();
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  async function handleCreateProject() {
    if (!session || !projectName) return;
    try {
      await api.createProject(session, { name: projectName });
      toast.success("Project created");
      setProjectName("");
      await refreshAll();
    } catch (err) {
      toast.error((err as Error).message);
    }
  }

  const statCards = [
    { label: "Projects", count: totalProjects, to: "/dashboard/projects", icon: "◫", detail: "deployment workspaces" },
    { label: "Applications", count: totalApps, to: "/dashboard/projects", icon: "◈", detail: `${runningApps} running` },
    { label: "Stacks", count: totalStacks, to: "/dashboard/stacks", icon: "▣", detail: "compose bundles" },
    { label: "Nodes", count: totalNodes, to: "/dashboard/runtime", icon: "◐", detail: "swarm capacity" },
  ];

  return (
    <div style={{ display: "grid", gap: 20 }}>
      <div className="page-header">
        <h2>Overview</h2>
      </div>

      {dashboard.error && (
        <div className="card" style={{ borderColor: "var(--error-fg)", color: "var(--error-fg)" }}>
          {dashboard.error}
        </div>
      )}

      <section className="overview-hero">
        <div>
          <div className="eyebrow">Swarm-native deployment cockpit</div>
          <h1>Ship apps, stacks, databases, domains, and backups from one place.</h1>
          <p>
            Hive is tuned for direct self-hosted operation: fast deploy loops, honest runtime status,
            and clear next steps instead of blank dashboards.
          </p>
          <div className="hero-actions">
            <Link className="btn-primary" to="/dashboard/projects">Create or deploy</Link>
            <Link className="btn-ghost" to="/dashboard/runtime">Inspect runtime</Link>
          </div>
        </div>
        <div className="hero-status-card" aria-label="Platform readiness">
          <span className="hero-score">{completedSteps}/{readinessSteps.length}</span>
          <span className="hero-label">launch checklist complete</span>
          <span className="hero-meta">{activeBuilds} active build{activeBuilds === 1 ? "" : "s"}</span>
        </div>
      </section>

      {/* Stats */}
      <div className="metric-grid">
        {statCards.map((card) => (
          <Link
            key={card.label}
            to={card.to}
            style={{
              textDecoration: "none",
              color: "inherit",
            }}
          >
            <div className="card metric-card">
              <div className="metric-icon">{card.icon}</div>
              <div className="metric-value">
                {isLoading ? <span className="skeleton-line skeleton-short" /> : card.count}
              </div>
              <div className="metric-label">{card.label}</div>
              <div className="metric-detail">{card.detail}</div>
            </div>
          </Link>
        ))}
      </div>

      <section className="card launch-checklist">
        <div className="card-header">
          <div>
            <div className="card-title">Launch checklist</div>
            <div className="card-subtitle">A guided path from empty cluster to production-ready app.</div>
          </div>
          <span className="badge badge-info">{completedSteps}/{readinessSteps.length}</span>
        </div>
        <div className="checklist-grid">
          {readinessSteps.map((step, index) => (
            <Link key={step.label} to={step.to} className={`checklist-item${step.done ? " is-done" : ""}`}>
              <span className="checklist-index">{step.done ? "✓" : index + 1}</span>
              <span>{step.label}</span>
            </Link>
          ))}
        </div>
      </section>

      <div className="responsive-grid-2">
        {/* Recent Builds */}
        <div className="card">
          <div className="card-header">
            <div>
              <div className="card-title">Recent Builds</div>
            </div>
          </div>
          {dashboard.loading ? (
            <div className="empty-state" style={{ padding: 24 }}>Loading builds…</div>
          ) : dashboard.builds.length === 0 ? (
            <div className="empty-state" style={{ padding: 24 }}>No builds yet.</div>
          ) : (
            <div style={{ display: "grid", gap: 8 }}>
              {dashboard.builds.slice(0, 6).map((b, i) => (
                <div
                  key={String(b.id ?? i)}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 12,
                    padding: "8px 10px",
                    borderRadius: "var(--radius-sm)",
                    background: "var(--bg-primary)",
                  }}
                >
                  <StatusBadge status={String(b.status ?? "")} />
                  <span style={{ fontSize: 13, fontFamily: "var(--font-mono)" }}>
                    {String(b.imageTag ?? b.id ?? "").slice(0, 32)}
                  </span>
                  <span style={{ fontSize: 11, color: "var(--text-faint)", marginLeft: "auto" }}>
                    {String(b.trigger ?? "")}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Quick Actions */}
        <div className="card">
          <div className="card-header">
            <div className="card-title">Quick Actions</div>
          </div>
          <div className="form-stack">
            <div className="form-group">
              <label>New Organization</label>
              <div className="inline-action-row">
                <input
                  value={orgName}
                  onChange={(e) => setOrgName(e.target.value)}
                  placeholder="Organization name"
                  style={{ flex: 1 }}
                />
                <button onClick={handleCreateOrg} className="btn-primary btn-sm">
                  Create
                </button>
              </div>
            </div>
            <div className="form-group">
              <label>New Project</label>
              <div className="inline-action-row">
                <input
                  value={projectName}
                  onChange={(e) => setProjectName(e.target.value)}
                  placeholder="Project name"
                  style={{ flex: 1 }}
                />
                <button onClick={handleCreateProject} className="btn-primary btn-sm">
                  Create
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Organizations */}
      {dashboard.organizations.length > 0 && (
        <div className="card">
          <div className="card-header">
            <div className="card-title">Organizations</div>
          </div>
          <div style={{ display: "grid", gap: 6 }}>
            {dashboard.organizations.map((org, i) => (
              <div
                key={String(org.id ?? i)}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 10,
                  padding: "8px 10px",
                  borderRadius: "var(--radius-sm)",
                  background: "var(--bg-primary)",
                }}
              >
                <span style={{ fontSize: 14, fontWeight: 500 }}>{String(org.name ?? "")}</span>
                <span style={{ color: "var(--text-faint)", fontSize: 12 }}>
                  {String(org.slug ?? "")}
                </span>
                {typeof org.role === "string" && (
                  <span className="badge badge-neutral" style={{ marginLeft: "auto" }}>
                    {org.role}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
