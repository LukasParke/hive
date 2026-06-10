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
  const isLoading = dashboard.loading;

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
    { label: "Projects", count: totalProjects, to: "/dashboard/projects", icon: "◫" },
    { label: "Applications", count: totalApps, to: "/dashboard/projects", icon: "◈" },
    { label: "Stacks", count: totalStacks, to: "/dashboard/stacks", icon: "▣" },
    { label: "Nodes", count: totalNodes, to: "/dashboard/runtime", icon: "◐" },
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

      {/* Stats */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))", gap: 14 }}>
        {statCards.map((card) => (
          <Link
            key={card.label}
            to={card.to}
            style={{
              textDecoration: "none",
              color: "inherit",
            }}
          >
            <div className="card" style={{ padding: "18px 20px" }}>
              <div style={{ fontSize: 20, marginBottom: 8, opacity: 0.5 }}>{card.icon}</div>
              <div style={{ fontSize: 28, fontWeight: 700, color: "var(--text-heading)", letterSpacing: "-0.02em" }}>
                {isLoading ? "…" : card.count}
              </div>
              <div style={{ color: "var(--text-secondary)", fontSize: 13, marginTop: 2 }}>{card.label}</div>
            </div>
          </Link>
        ))}
      </div>

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
