import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import type { ItemMap } from "../api/client";
import { useAppData } from "../contexts/AppContext";
import { navGroups } from "../navigation";

interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
}

type CommandItem = {
  id: string;
  title: string;
  subtitle: string;
  section: string;
  icon: string;
  to: string;
  keywords: string;
};

function itemText(value: unknown): string {
  return String(value ?? "");
}

function resourceItem(kind: string, icon: string, item: ItemMap, to: string, subtitleParts: unknown[]): CommandItem {
  const id = itemText(item.id) || `${kind}-${itemText(item.name)}`;
  const title = itemText(item.name) || itemText(item.hostname) || id;
  const subtitle = subtitleParts.map(itemText).filter(Boolean).join(" · ");
  return {
    id: `${kind}-${id}`,
    title,
    subtitle: subtitle || kind,
    section: kind,
    icon,
    to,
    keywords: `${kind} ${title} ${subtitle} ${itemText(item.status)} ${itemText(item.image)} ${itemText(item.serviceName)}`.toLowerCase(),
  };
}

export function CommandPalette({ open, onClose }: CommandPaletteProps) {
  const navigate = useNavigate();
  const { dashboard } = useAppData();
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);

  const items = useMemo<CommandItem[]>(() => {
    const navigation = navGroups.flatMap((group) =>
      group.items.map((item) => ({
        id: `nav-${item.to}`,
        title: item.label,
        subtitle: group.label || "Navigate",
        section: "Navigation",
        icon: item.icon,
        to: item.to,
        keywords: `${item.label} ${group.label} ${(item.keywords ?? []).join(" ")}`.toLowerCase(),
      })),
    );

    const projects = dashboard.projects.map((project) =>
      resourceItem("Projects", "◫", project, "/dashboard/projects", [project.id]),
    );
    const apps = dashboard.applications.map((app) =>
      resourceItem("Applications", "◈", app, `/dashboard/services/application/${itemText(app.id)}`, [app.status, app.sourceType, app.image]),
    );
    const stacks = dashboard.stacks.map((stack) =>
      resourceItem("Stacks", "▣", stack, `/dashboard/services/stack/${itemText(stack.id)}`, [stack.id]),
    );
    const databases = dashboard.databaseServices.map((db) =>
      resourceItem("Databases", "◉", db, `/dashboard/services/database/${itemText(db.id)}`, [db.engine, db.serviceName]),
    );
    const domains = dashboard.domains.map((domain) =>
      resourceItem("Domains", "◊", domain, "/dashboard/domains", [domain.hostname, domain.tlsEnabled ? "TLS" : "HTTP"]),
    );

    return [...navigation, ...projects, ...apps, ...stacks, ...databases, ...domains];
  }, [dashboard.applications, dashboard.databaseServices, dashboard.domains, dashboard.projects, dashboard.stacks]);

  const results = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items.slice(0, 12);
    const terms = q.split(/\s+/).filter(Boolean);
    return items
      .map((item) => {
        const haystack = `${item.title} ${item.subtitle} ${item.keywords}`.toLowerCase();
        const score = terms.reduce((total, term) => {
          if (item.title.toLowerCase().includes(term)) return total + 4;
          if (haystack.includes(term)) return total + 1;
          return total - 100;
        }, 0);
        return { item, score };
      })
      .filter((entry) => entry.score >= 0)
      .sort((a, b) => b.score - a.score || a.item.title.localeCompare(b.item.title))
      .slice(0, 12)
      .map((entry) => entry.item);
  }, [items, query]);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setSelectedIndex(0);
    const timer = window.setTimeout(() => inputRef.current?.focus(), 20);
    return () => window.clearTimeout(timer);
  }, [open]);

  useEffect(() => {
    setSelectedIndex(0);
  }, [query]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!open) return;
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
      }
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setSelectedIndex((idx) => Math.min(idx + 1, Math.max(results.length - 1, 0)));
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        setSelectedIndex((idx) => Math.max(idx - 1, 0));
      }
      if (event.key === "Enter" && results[selectedIndex]) {
        event.preventDefault();
        navigate(results[selectedIndex].to);
        onClose();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [navigate, onClose, open, results, selectedIndex]);

  if (!open) return null;

  return (
    <div className="command-overlay" role="dialog" aria-modal="true" aria-label="Command palette" onMouseDown={onClose}>
      <div className="command-panel" onMouseDown={(event) => event.stopPropagation()}>
        <div className="command-search-row">
          <span aria-hidden="true">⌕</span>
          <input
            ref={inputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search pages, apps, stacks, domains…"
            aria-label="Search Hive"
          />
          <kbd>Esc</kbd>
        </div>
        <div className="command-results" role="listbox" aria-label="Command results">
          {results.length === 0 ? (
            <div className="command-empty">No results. Try “project”, “domain”, or an app name.</div>
          ) : (
            results.map((item, index) => (
              <button
                key={item.id}
                type="button"
                className={`command-item${index === selectedIndex ? " is-selected" : ""}`}
                onMouseEnter={() => setSelectedIndex(index)}
                onClick={() => {
                  navigate(item.to);
                  onClose();
                }}
                role="option"
                aria-selected={index === selectedIndex}
              >
                <span className="command-icon">{item.icon}</span>
                <span className="command-copy">
                  <span className="command-title">{item.title}</span>
                  <span className="command-subtitle">{item.section} · {item.subtitle}</span>
                </span>
                <span className="command-arrow">↵</span>
              </button>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
