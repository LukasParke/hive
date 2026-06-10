import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api, type ItemMap, type Session } from "../api/client";
import { useAuth } from "./AuthContext";
import { initialDashboard, type DashboardState } from "../types";

interface AppContextValue {
  dashboard: DashboardState;
  events: string[];
  refreshAll: () => Promise<void>;
  refreshProjects: () => Promise<void>;
  refreshApplications: () => Promise<void>;
  refreshStacks: () => Promise<void>;
  refreshBuilds: () => Promise<void>;
  refreshBuildQueue: () => Promise<void>;
  refreshDomains: () => Promise<void>;
  refreshRegistries: () => Promise<void>;
  refreshSchedules: () => Promise<void>;
  refreshNotifications: () => Promise<void>;
  refreshBackups: () => Promise<void>;
  refreshBackupDestinations: () => Promise<void>;
  refreshGitProviders: () => Promise<void>;
  refreshDatabaseServices: () => Promise<void>;
  refreshDeployments: () => Promise<void>;
  refreshSecrets: () => Promise<void>;
  refreshConfigs: () => Promise<void>;
  refreshNetworks: () => Promise<void>;
  refreshNodes: () => Promise<void>;
  refreshEnvironments: () => Promise<void>;
  refreshSecurityRules: () => Promise<void>;
}

const AppContext = createContext<AppContextValue | null>(null);

export function AppProvider({ children }: { children: ReactNode }) {
  const { session, setSession } = useAuth();
  const [dashboard, setDashboard] = useState<DashboardState>(initialDashboard);
  const [events, setEvents] = useState<string[]>([]);
  const orgSessionPromiseRef = useRef<Promise<{ session: Session; organizations: ItemMap[] } | null> | null>(null);

  const patch = useCallback((key: keyof DashboardState, items: ItemMap[] | null | undefined) => {
    setDashboard((prev) => ({ ...prev, [key]: Array.isArray(items) ? items : [] }));
  }, []);

  const ensureOrgSession = useCallback(async () => {
    if (!session) return null;
    if (session.orgId) {
      return { session, organizations: dashboard.organizations };
    }
    if (orgSessionPromiseRef.current) {
      return orgSessionPromiseRef.current;
    }

    orgSessionPromiseRef.current = (async () => {
      try {
        const orgs = await api.listOrganizations(session);
        let organizations = (orgs.items ?? []) as ItemMap[];
        let orgId = String(organizations[0]?.id ?? "");

        if (!orgId) {
          const created = await api.createOrganization(session, { name: "Hive", slug: "hive" });
          orgId = String(created.id);
          organizations = [{ ...created, role: "owner" }];
        }

        if (!orgId) return { session, organizations };
        const scopedSession = { ...session, orgId };
        setSession(scopedSession);
        setDashboard((prev) => ({ ...prev, organizations }));
        return { session: scopedSession, organizations };
      } catch (err) {
        console.warn("Failed to resolve organization", err);
        return { session, organizations: [] };
      } finally {
        orgSessionPromiseRef.current = null;
      }
    })();

    return orgSessionPromiseRef.current;
  }, [session, setSession, dashboard.organizations]);

  const refreshList = useCallback(
    async (key: keyof DashboardState, fetcher: (s: Session) => Promise<{ items: ItemMap[] }>) => {
      const resolved = await ensureOrgSession();
      if (!resolved) return;
      try {
        const res = await fetcher(resolved.session);
        patch(key, res.items ?? []);
      } catch {
        // silently ignore refresh errors
      }
    },
    [ensureOrgSession, patch],
  );

  const refreshProjects = useCallback(() => refreshList("projects", (s) => api.listProjects(s)), [refreshList]);
  const refreshApplications = useCallback(() => refreshList("applications", (s) => api.listApplications(s)), [refreshList]);
  const refreshStacks = useCallback(() => refreshList("stacks", (s) => api.listStacks(s)), [refreshList]);
  const refreshBuilds = useCallback(() => refreshList("builds", (s) => api.listBuilds(s)), [refreshList]);
  const refreshBuildQueue = useCallback(() => refreshList("buildQueue", (s) => api.listBuildQueue(s)), [refreshList]);
  const refreshDomains = useCallback(() => refreshList("domains", (s) => api.listDomains(s)), [refreshList]);
  const refreshRegistries = useCallback(() => refreshList("registries", (s) => api.listRegistries(s)), [refreshList]);
  const refreshSchedules = useCallback(() => refreshList("schedules", (s) => api.listSchedules(s)), [refreshList]);
  const refreshNotifications = useCallback(() => refreshList("notifications", (s) => api.listNotifications(s)), [refreshList]);
  const refreshBackups = useCallback(() => refreshList("backups", (s) => api.listBackups(s)), [refreshList]);
  const refreshBackupDestinations = useCallback(() => refreshList("backupDestinations", (s) => api.listBackupDestinations(s)), [refreshList]);
  const refreshGitProviders = useCallback(() => refreshList("gitProviders", (s) => api.listGitProviders(s)), [refreshList]);
  const refreshDatabaseServices = useCallback(() => refreshList("databaseServices", (s) => api.listDatabaseServices(s)), [refreshList]);
  const refreshDeployments = useCallback(() => refreshList("builds", (s) => api.listDeployments(s)), [refreshList]);
  const refreshSecrets = useCallback(() => refreshList("secrets", (s) => api.listSecrets(s)), [refreshList]);
  const refreshConfigs = useCallback(() => refreshList("configs", (s) => api.listConfigs(s)), [refreshList]);
  const refreshNetworks = useCallback(() => refreshList("networks", (s) => api.listNetworks(s)), [refreshList]);
  const refreshNodes = useCallback(() => refreshList("nodes", (s) => api.listNodes(s)), [refreshList]);
  const refreshEnvironments = useCallback(() => refreshList("environments", (s) => api.listEnvironments(s)), [refreshList]);
  const refreshSecurityRules = useCallback(() => refreshList("securityRules", (s) => api.listSecurityRules(s)), [refreshList]);

  const refreshAll = useCallback(async () => {
    if (!session) return;

    const fetchItems = async (fetcher: () => Promise<{ items: ItemMap[] }>) => {
      try {
        const res = await fetcher();
        return res.items ?? [];
      } catch (err) {
        console.warn("Dashboard refresh request failed", err);
        return [];
      }
    };

    try {
      const [me, resolved] = await Promise.all([api.me(session), ensureOrgSession()]);
      if (!resolved) return;
      const { session: scopedSession, organizations } = resolved;

      const [projects, environments, applications, services, nodes, builds, buildQueue, domains, registries, stacks, backups, backupDestinations, schedules, gitProviders, notifications, databaseServices, secrets, configs, networks, securityRules] =
        await Promise.all([
          fetchItems(() => api.listProjects(scopedSession)),
          fetchItems(() => api.listEnvironments(scopedSession)),
          fetchItems(() => api.listApplications(scopedSession)),
          fetchItems(() => api.listServices(scopedSession)),
          fetchItems(() => api.listNodes(scopedSession)),
          fetchItems(() => api.listBuilds(scopedSession)),
          fetchItems(() => api.listBuildQueue(scopedSession)),
          fetchItems(() => api.listDomains(scopedSession)),
          fetchItems(() => api.listRegistries(scopedSession)),
          fetchItems(() => api.listStacks(scopedSession)),
          fetchItems(() => api.listBackups(scopedSession)),
          fetchItems(() => api.listBackupDestinations(scopedSession)),
          fetchItems(() => api.listSchedules(scopedSession)),
          fetchItems(() => api.listGitProviders(scopedSession)),
          fetchItems(() => api.listNotifications(scopedSession)),
          fetchItems(() => api.listDatabaseServices(scopedSession)),
          fetchItems(() => api.listSecrets(scopedSession)),
          fetchItems(() => api.listConfigs(scopedSession)),
          fetchItems(() => api.listNetworks(scopedSession)),
          fetchItems(() => api.listSecurityRules(scopedSession)),
        ]);

      setDashboard({
        me: me as ItemMap,
        organizations,
        projects,
        environments,
        applications,
        services,
        nodes,
        builds,
        buildQueue,
        domains,
        registries,
        stacks,
        backups,
        backupDestinations,
        schedules,
        gitProviders,
        notifications,
        databaseServices,
        secrets,
        configs,
        networks,
        securityRules,
      });
    } catch (err) {
      console.warn("Dashboard refresh failed", err);
    }
  }, [session, ensureOrgSession]);

  // Initial load
  useEffect(() => {
    if (session?.accessToken) {
      refreshAll();
    } else {
      setDashboard(initialDashboard);
    }
  }, [session?.accessToken]); // eslint-disable-line react-hooks/exhaustive-deps

  // WebSocket events
  useEffect(() => {
    if (!session?.accessToken) return;
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    let cancelled = false;
    let ws: WebSocket | null = null;
    let retryCount = 0;
    const connect = () => {
      if (cancelled) return;
      ws = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws/events?access_token=${encodeURIComponent(session.accessToken)}`);
      ws.onopen = () => {
        retryCount = 0;
      };
      ws.onmessage = (event) => setEvents((prev) => [event.data, ...prev].slice(0, 100));
      ws.onclose = () => {
        if (cancelled) return;
        if (retryCount >= 10) {
          console.warn("Realtime event stream unavailable; stopping reconnect attempts");
          return;
        }
        const delay = Math.min(5000, 500 * (retryCount + 1));
        retryCount += 1;
        window.setTimeout(connect, delay);
      };
    };
    connect();
    return () => {
      cancelled = true;
      ws?.close();
    };
  }, [session?.accessToken]);

  const value = useMemo(
    () => ({
      dashboard,
      events,
      refreshAll,
      refreshProjects,
      refreshApplications,
      refreshStacks,
      refreshBuilds,
      refreshBuildQueue,
      refreshDomains,
      refreshRegistries,
      refreshSchedules,
      refreshNotifications,
      refreshBackups,
      refreshBackupDestinations,
      refreshGitProviders,
      refreshDatabaseServices,
      refreshDeployments,
      refreshSecrets,
      refreshConfigs,
      refreshNetworks,
      refreshNodes,
      refreshEnvironments,
      refreshSecurityRules,
    }),
    [dashboard, events, refreshAll, refreshProjects, refreshApplications, refreshStacks, refreshBuilds, refreshBuildQueue, refreshDomains, refreshRegistries, refreshSchedules, refreshNotifications, refreshBackups, refreshBackupDestinations, refreshGitProviders, refreshDatabaseServices, refreshDeployments, refreshSecrets, refreshConfigs, refreshNetworks, refreshNodes, refreshEnvironments],
  );

  return <AppContext.Provider value={value}>{children}</AppContext.Provider>;
}

export function useAppData(): AppContextValue {
  const ctx = useContext(AppContext);
  if (!ctx) throw new Error("useAppData must be used within AppProvider");
  return ctx;
}
