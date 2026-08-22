import { Link } from "react-router-dom";

const settingsLinks = [
  { to: "/dashboard/settings/notifications", label: "Notifications", desc: "Configure notification channels" },
  { to: "/dashboard/settings/registries", label: "Registries", desc: "Manage Docker registries" },
  { to: "/dashboard/settings/backups", label: "Backup Destinations", desc: "Configure backup storage" },
  { to: "/dashboard/settings/schedules", label: "Schedules", desc: "Manage scheduled tasks" },
  { to: "/dashboard/settings/git-providers", label: "Git Providers", desc: "Connect git providers" },
  { to: "/dashboard/settings/global", label: "Global Settings", desc: "System configuration" },
  { to: "/dashboard/settings/users", label: "Users & Members", desc: "Organization management" },
  { to: "/dashboard/settings/infra", label: "Infrastructure", desc: "Servers, SSH keys, certificates" },
];

export function SettingsPage() {
  return (
    <div style={{ display: "grid", gap: 16 }}>
      <h2 style={{ margin: 0 }}>Settings</h2>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: 12 }}>
        {settingsLinks.map((link) => (
          <Link key={link.to} to={link.to} style={{ textDecoration: "none", color: "var(--text-primary)", background: "var(--bg-secondary)", border: "1px solid var(--border-primary)", borderRadius: 8, padding: 16 }}>
            <div style={{ fontWeight: 600, marginBottom: 4 }}>{link.label}</div>
            <div style={{ fontSize: 13, color: "var(--text-secondary)" }}>{link.desc}</div>
          </Link>
        ))}
      </div>
    </div>
  );
}
