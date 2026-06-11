import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useAuth } from "../contexts/AuthContext";
import { useAppData } from "../contexts/AppContext";
import { useToast } from "../contexts/ToastContext";
import { navGroups } from "../navigation";
import { CommandPalette } from "./CommandPalette";
import { SystemUpdateBanner } from "./SystemUpdateBanner";
import { Logo } from "./Logo";

export function DashboardShell({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const { session, logout } = useAuth();
  const { dashboard, refreshAll } = useAppData();
  const toast = useToast();
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const isActive = (to: string) =>
    location.pathname === to || location.pathname.startsWith(to + "/");

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setCommandOpen(true);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  async function handleRefresh() {
    setRefreshing(true);
    try {
      await refreshAll();
      toast.success("Dashboard refreshed");
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setRefreshing(false);
    }
  }

  const totalResources = dashboard.projects.length + dashboard.applications.length + dashboard.stacks.length + dashboard.databaseServices.length;

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
      <CommandPalette open={commandOpen} onClose={() => setCommandOpen(false)} />
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
        <div className="dashboard-topbar">
          <button type="button" className="command-trigger" onClick={() => setCommandOpen(true)} aria-label="Search Hive">
            <span aria-hidden="true">⌕</span>
            <span>Search Hive</span>
            <kbd>{navigator.platform.toLowerCase().includes("mac") ? "⌘" : "Ctrl"} K</kbd>
          </button>
          <div className="topbar-actions">
            <span className={`health-pill${dashboard.error ? " is-error" : ""}`}>
              <span className="dot" />
              {dashboard.error ? "Needs attention" : `${totalResources} resources`}
            </span>
            <button type="button" className="btn-ghost btn-sm" onClick={handleRefresh} disabled={refreshing || dashboard.loading}>
              {refreshing || dashboard.loading ? "Refreshing…" : "Refresh"}
            </button>
          </div>
        </div>
        <SystemUpdateBanner />
        {children}
      </main>
    </section>
  );
}
