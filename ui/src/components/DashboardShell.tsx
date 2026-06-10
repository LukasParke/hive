import { useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useAuth } from "../contexts/AuthContext";
import { SystemUpdateBanner } from "./SystemUpdateBanner";
import { Logo } from "./Logo";

type NavItem = { to: string; label: string; icon: string };
type NavGroup = { label: string; items: NavItem[] };

const navGroups: NavGroup[] = [
  {
    label: "",
    items: [
      { to: "/dashboard/overview", label: "Overview", icon: "◈" },
    ],
  },
  {
    label: "Applications",
    items: [
      { to: "/dashboard/projects", label: "Projects", icon: "◫" },
      { to: "/dashboard/deployments", label: "Deployments", icon: "▶" },
      { to: "/dashboard/stacks", label: "Stacks", icon: "▣" },
      { to: "/dashboard/database/create", label: "Databases", icon: "◉" },
    ],
  },
  {
    label: "Infrastructure",
    items: [
      { to: "/dashboard/runtime", label: "Runtime / Nodes", icon: "◐" },
      { to: "/dashboard/monitoring", label: "Monitoring", icon: "◎" },
      { to: "/dashboard/secrets", label: "Secrets", icon: "◈" },
      { to: "/dashboard/configs", label: "Configs", icon: "◫" },
      { to: "/dashboard/networks", label: "Networks", icon: "◯" },
      { to: "/dashboard/domains", label: "Domains", icon: "◊" },
      { to: "/dashboard/security", label: "Security", icon: "◈" },
    ],
  },
  {
    label: "System",
    items: [
      { to: "/dashboard/environments", label: "Environments", icon: "◐" },
      { to: "/dashboard/settings", label: "Settings", icon: "◫" },
      { to: "/dashboard/events", label: "Events", icon: "◎" },
    ],
  },
];

export function DashboardShell({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const { session, logout } = useAuth();
  const [sidebarOpen, setSidebarOpen] = useState(false);

  const isActive = (to: string) =>
    location.pathname === to || location.pathname.startsWith(to + "/");

  return (
    <section className="dashboard-shell">
      <button
        type="button"
        className="dashboard-menu-button"
        onClick={() => setSidebarOpen(true)}
        aria-label="Open navigation"
      >
        ☰
      </button>
      {sidebarOpen && <button type="button" className="dashboard-scrim" aria-label="Close navigation" onClick={() => setSidebarOpen(false)} />}
      <aside className={`dashboard-sidebar${sidebarOpen ? " is-open" : ""}`}>

        {/* Logo */}
        <div style={{ padding: "0 20px 20px", display: "flex", alignItems: "center", gap: 10 }}>
          <Logo size={28} />
          <span style={{ fontSize: 18, fontWeight: 700, color: "var(--text-heading)", letterSpacing: "-0.02em" }}>
            Hive
          </span>
        </div>

        {/* Nav */}
        <nav style={{ flex: 1, display: "grid", gap: 18, padding: "0 12px" }}>
          {navGroups.map((group, gi) => (
            <div key={gi}>
              {group.label && (
                <div
                  style={{
                    fontSize: 10,
                    fontWeight: 600,
                    color: "var(--text-faint)",
                    textTransform: "uppercase",
                    letterSpacing: 1,
                    padding: "0 8px 6px",
                  }}
                >
                  {group.label}
                </div>
              )}
              <div style={{ display: "grid", gap: 1 }}>
                {group.items.map((item) => {
                  const active = isActive(item.to);
                  return (
                    <Link
                      key={item.to}
                      to={item.to}
                      aria-current={active ? "page" : undefined}
                      onClick={() => setSidebarOpen(false)}
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 10,
                        padding: "7px 10px",
                        borderRadius: "var(--radius-sm)",
                        fontWeight: active ? 600 : 450,
                        background: active ? "var(--gold-alpha-10)" : "transparent",
                        color: active ? "var(--gold-500)" : "var(--text-secondary)",
                        textDecoration: "none",
                        fontSize: 13,
                        borderLeft: active ? "2px solid var(--gold-500)" : "2px solid transparent",
                        transition: "background var(--transition-fast), color var(--transition-fast)",
                        lineHeight: 1.3,
                      }}
                    >
                      <span style={{ fontSize: 12, opacity: active ? 1 : 0.6 }}>{item.icon}</span>
                      {item.label}
                    </Link>
                  );
                })}
              </div>
            </div>
          ))}
        </nav>

        {/* Footer */}
        <div
          style={{
            marginTop: "auto",
            padding: "14px 16px 0",
            borderTop: "1px solid var(--border-primary)",
            display: "grid",
            gap: 6,
          }}
        >
          <div style={{ fontSize: 11, color: "var(--text-faint)" }}>
            Org: {session?.orgId?.slice(0, 8) ?? "none"}
          </div>
          <Link
            to="/dashboard/profile"
            style={{
              fontSize: 12,
              color: "var(--text-secondary)",
              textDecoration: "none",
              padding: "4px 0",
            }}
          >
            Profile
          </Link>
          <button
            onClick={logout}
            className="btn-ghost btn-sm"
            style={{ justifyContent: "flex-start", width: "fit-content" }}
          >
            Logout
          </button>
        </div>
      </aside>

      <main className="dashboard-main">
        <SystemUpdateBanner />
        {children}
      </main>
    </section>
  );
}
