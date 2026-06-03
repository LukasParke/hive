import { Link, useLocation } from "react-router-dom";
import { useAuth } from "../contexts/AuthContext";
import { SystemUpdateBanner } from "./SystemUpdateBanner";

type NavGroup = { label: string; items: { to: string; label: string }[] };

const navGroups: NavGroup[] = [
  {
    label: "",
    items: [{ to: "/dashboard/overview", label: "Overview" }],
  },
  {
    label: "Applications",
    items: [
      { to: "/dashboard/projects", label: "Projects" },
      { to: "/dashboard/deployments", label: "Deployments" },
      { to: "/dashboard/stacks", label: "Stacks" },
      { to: "/dashboard/database/create", label: "Databases" },
    ],
  },
  {
    label: "Infrastructure",
    items: [
      { to: "/dashboard/runtime", label: "Runtime / Nodes" },
      { to: "/dashboard/monitoring", label: "Monitoring" },
      { to: "/dashboard/secrets", label: "Secrets" },
      { to: "/dashboard/configs", label: "Configs" },
      { to: "/dashboard/networks", label: "Networks" },
      { to: "/dashboard/domains", label: "Domains" },
      { to: "/dashboard/security", label: "Security" },
    ],
  },
  {
    label: "System",
    items: [
      { to: "/dashboard/environments", label: "Environments" },
      { to: "/dashboard/settings", label: "Settings" },
      { to: "/dashboard/events", label: "Events" },
    ],
  },
];

export function DashboardShell({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const { session, logout } = useAuth();

  const isActive = (to: string) => location.pathname === to || location.pathname.startsWith(to + "/");

  return (
    <section style={{ display: "grid", gridTemplateColumns: "220px 1fr", gap: 16, minHeight: "calc(100vh - 80px)" }}>
      <aside style={{ background: "var(--bg-secondary)", borderRight: "1px solid var(--border-primary)", paddingRight: 10, borderRadius: "var(--radius-md) 0 0 var(--radius-md)" }}>
        <div style={{ display: "grid", gap: 2 }}>
          {navGroups.map((group, gi) => (
            <div key={gi}>
              {group.label && (
                <div style={{ fontSize: 11, fontWeight: 600, color: "var(--text-faint)", textTransform: "uppercase", letterSpacing: 1, padding: "12px 8px 4px" }}>
                  {group.label}
                </div>
              )}
              {group.items.map((item) => (
                <Link
                  key={item.to}
                  to={item.to}
                  style={{
                    display: "block",
                    padding: "6px 8px",
                    borderRadius: 4,
                    fontWeight: isActive(item.to) ? 600 : 400,
                    background: isActive(item.to) ? "var(--gold-alpha-10)" : "transparent",
                    color: isActive(item.to) ? "var(--gold-500)" : "var(--text-secondary)",
                    textDecoration: "none",
                    fontSize: 14,
                    borderLeft: isActive(item.to) ? "2px solid var(--gold-500)" : "2px solid transparent",
                    transition: "background var(--transition-fast), color var(--transition-fast)",
                  }}
                >
                  {item.label}
                </Link>
              ))}
            </div>
          ))}
        </div>

        <div style={{ marginTop: 24, padding: "12px 8px", borderTop: "1px solid var(--border-primary)" }}>
          <div style={{ fontSize: 12, color: "var(--text-faint)", marginBottom: 4 }}>Org: {session?.orgId?.slice(0, 8) ?? "none"}</div>
          <Link to="/dashboard/profile" style={{ display: "block", fontSize: 13, color: "var(--text-secondary)", textDecoration: "none", marginBottom: 8 }}>Profile</Link>
          <button onClick={logout} style={{ fontSize: 13, padding: "4px 12px" }}>Logout</button>
        </div>
      </aside>
      <section style={{ minWidth: 0 }}>
        <SystemUpdateBanner />
        {children}
      </section>
    </section>
  );
}
