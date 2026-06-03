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

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <h2 style={{ margin: 0 }}>Overview</h2>

      {/* Dashboard cards */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 12 }}>
        {[
          { label: "Projects", count: totalProjects, to: "/dashboard/projects" },
          { label: "Applications", count: totalApps, to: "/dashboard/projects" },
          { label: "Stacks", count: totalStacks, to: "/dashboard/stacks" },
          { label: "Nodes", count: totalNodes, to: "/dashboard/runtime" },
        ].map((card) => (
          <Link key={card.label} to={card.to} style={{ textDecoration: "none", color: "var(--text-primary)", background: "var(--bg-secondary)", border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16 }}>
            <div style={{ fontSize: 28, fontWeight: 700 }}>{card.count}</div>
            <div style={{ color: "var(--text-secondary)", fontSize: 14 }}>{card.label}</div>
          </Link>
        ))}
      </div>

      {/* Recent builds */}
      {dashboard.builds.length > 0 && (
        <section style={{ background: "var(--bg-secondary)", border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16 }}>
          <h3 style={{ margin: "0 0 12px" }}>Recent Builds</h3>
          {dashboard.builds.slice(0, 5).map((b, i) => (
            <div key={String(b.id ?? i)} style={{ display: "flex", gap: 12, alignItems: "center", padding: "6px 0", borderBottom: "1px solid var(--border-primary)" }}>
              <StatusBadge status={String(b.status ?? "")} />
              <span style={{ fontSize: 13 }}>{String(b.imageTag ?? b.id ?? "")}</span>
              <span style={{ fontSize: 12, color: "var(--text-faint)", marginLeft: "auto" }}>{String(b.trigger ?? "")}</span>
            </div>
          ))}
        </section>
      )}

      {/* Quick actions */}
      <section style={{ background: "var(--bg-secondary)", border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16 }}>
        <h3 style={{ margin: "0 0 12px" }}>Quick Actions</h3>
        <div style={{ display: "grid", gap: 8, maxWidth: 400 }}>
          <div style={{ display: "flex", gap: 8 }}>
            <input value={orgName} onChange={(e) => setOrgName(e.target.value)} placeholder="Organization name" style={{ flex: 1, padding: "6px 10px" }} />
            <input value={orgSlug} onChange={(e) => setOrgSlug(e.target.value)} placeholder="Slug (optional)" style={{ flex: 1, padding: "6px 10px" }} />
            <button onClick={handleCreateOrg} style={{ whiteSpace: "nowrap", padding: "6px 12px" }}>Create Org</button>
          </div>
          <div style={{ display: "flex", gap: 8 }}>
            <input value={projectName} onChange={(e) => setProjectName(e.target.value)} placeholder="Project name" style={{ flex: 1, padding: "6px 10px" }} />
            <button onClick={handleCreateProject} style={{ padding: "6px 12px" }}>Create Project</button>
          </div>
        </div>
      </section>

      {/* Organizations */}
      {dashboard.organizations.length > 0 && (
        <section style={{ background: "var(--bg-secondary)", border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16 }}>
          <h3 style={{ margin: "0 0 8px" }}>Organizations</h3>
          {dashboard.organizations.map((org, i) => (
            <div key={String(org.id ?? i)} style={{ padding: "4px 0", fontSize: 14 }}>
              {String(org.name ?? "")} <span style={{ color: "var(--text-faint)" }}>({String(org.slug ?? "")})</span>
              {typeof org.role === "string" && <span style={{ marginLeft: 8, fontSize: 12, color: "var(--text-secondary)" }}>{org.role}</span>}
            </div>
          ))}
        </section>
      )}
    </div>
  );
}
