export type NavItem = { to: string; label: string; icon: string; keywords?: string[] };
export type NavGroup = { label: string; items: NavItem[] };

export const navGroups: NavGroup[] = [
  {
    label: "",
    items: [
      { to: "/dashboard/overview", label: "Overview", icon: "◈", keywords: ["home", "summary", "dashboard"] },
    ],
  },
  {
    label: "Applications",
    items: [
      { to: "/dashboard/projects", label: "Projects", icon: "◫", keywords: ["apps", "services", "new app"] },
      { to: "/dashboard/deployments", label: "Deployments", icon: "▶", keywords: ["releases", "rollbacks", "builds"] },
      { to: "/dashboard/stacks", label: "Stacks", icon: "▣", keywords: ["compose", "docker compose"] },
      { to: "/dashboard/database/create", label: "Databases", icon: "◉", keywords: ["postgres", "mysql", "redis", "mongo"] },
    ],
  },
  {
    label: "Infrastructure",
    items: [
      { to: "/dashboard/runtime", label: "Runtime / Nodes", icon: "◐", keywords: ["swarm", "nodes", "services"] },
      { to: "/dashboard/monitoring", label: "Monitoring", icon: "◎", keywords: ["metrics", "health", "usage"] },
      { to: "/dashboard/secrets", label: "Secrets", icon: "◈", keywords: ["environment", "passwords"] },
      { to: "/dashboard/configs", label: "Configs", icon: "◫", keywords: ["configuration"] },
      { to: "/dashboard/networks", label: "Networks", icon: "◯", keywords: ["overlay", "docker network"] },
      { to: "/dashboard/domains", label: "Domains", icon: "◊", keywords: ["routing", "tls", "traefik"] },
      { to: "/dashboard/tunnels", label: "Tunnels", icon: "⇄", keywords: ["cloudflare", "tunnel", "ingress", "cname"] },
      { to: "/dashboard/security", label: "Security", icon: "◈", keywords: ["rules", "headers", "access"] },
    ],
  },
  {
    label: "System",
    items: [
      { to: "/dashboard/environments", label: "Environments", icon: "◐", keywords: ["staging", "production"] },
      { to: "/dashboard/settings", label: "Settings", icon: "◫", keywords: ["configuration", "admin"] },
      { to: "/dashboard/events", label: "Events", icon: "◎", keywords: ["realtime", "activity"] },
    ],
  },
];
