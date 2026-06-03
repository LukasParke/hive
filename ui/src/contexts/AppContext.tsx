import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { api, type ItemMap } from "../api/client";
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

  const patch = useCallback((key: keyof DashboardState, items: ItemMap[]) => {
    setDashboard((prev) => ({ ...prev, [key]: items }));
  }, []);

  const refreshList = useCallback(
    async (key: keyof DashboardState, fetcher: () => Promise<{ items: ItemMap[] }>) => {
      if (!session) return;
      try {
        const res = await fetcher();
        patch(key, res.items);
      } catch {
        // silently ignore refresh errors
      }
    },
    [session, patch],
  );

  const refreshProjects = useCallback(() => refreshList("projects", () => api.listProjects(session!)), [refreshList, session]);
  const refreshApplications = useCallback(() => refreshList("applications", () => api.listApplications(session!)), [refreshList, session]);
  const refreshStacks = useCallback(() => refreshList("stacks", () => api.listStacks(session!)), [refreshList, session]);
  const refreshBuilds = useCallback(() => refreshList("builds", () => api.listBuilds(session!)), [refreshList, session]);
  const refreshBuildQueue = useCallback(() => refreshList("buildQueue", () => api.listBuildQueue(session!)), [refreshList, session]);
  const refreshDomains = useCallback(() => refreshList("domains", () => api.listDomains(session!)), [refreshList, session]);
  const refreshRegistries = useCallback(() => refreshList("registries", () => api.listRegistries(session!)), [refreshList, session]);
  const refreshSchedules = useCallback(() => refreshList("schedules", () => api.listSchedules(session!)), [refreshList, session]);
  const refreshNotifications = useCallback(() => refreshList("notifications", () => api.listNotifications(session!)), [refreshList, session]);
  const refreshBackups = useCallback(() => refreshList("backups", () => api.listBackups(session!)), [refreshList, session]);
  const refreshBackupDestinations = useCallback(() => refreshList("backupDestinations", () => api.listBackupDestinations(session!)), [refreshList, session]);
  const refreshGitProviders = useCallback(() => refreshList("gitProviders", () => api.listGitProviders(session!)), [refreshList, session]);
  const refreshDatabaseServices = useCallback(() => refreshList("databaseServices", () => api.listDatabaseServices(session!)), [refreshList, session]);
  const refreshDeployments = useCallback(() => refreshList("builds", () => api.listDeployments(session!)), [refreshList, session]);
  const refreshSecrets = useCallback(() => refreshList("secrets", () => api.listSecrets(session!)), [refreshList, session]);
  const refreshConfigs = useCallback(() => refreshList("configs", () => api.listConfigs(session!)), [refreshList, session]);
  const refreshNetworks = useCallback(() => refreshList("networks", () => api.listNetworks(session!)), [refreshList, session]);
  const refreshNodes = useCallback(() => refreshList("nodes", () => api.listNodes(session!)), [refreshList, session]);
  const refreshEnvironments = useCallback(() => refreshList("environments", () => api.listEnvironments(session!)), [refreshList, session]);
  const refreshSecurityRules = useCallback(() => refreshList("securityRules", () => api.listSecurityRules(session!)), [refreshList, session]);

  const refreshAll = useCallback(async () => {
    if (!session) return;
    try {
      const [me, orgs, projects, environments, applications, services, nodes, builds, buildQueue, domains, registries, stacks, backups, backupDestinations, schedules, gitProviders, notifications, databaseServices, secrets, configs, networks, securityRules] =
        await Promise.all([
          api.me(session),
          api.listOrganizations(session),
          api.listProjects(session),
          api.listEnvironments(session),
          api.listApplications(session),
          api.listServices(session),
          api.listNodes(session),
          api.listBuilds(session),
          api.listBuildQueue(session),
          api.listDomains(session),
          api.listRegistries(session),
          api.listStacks(session),
          api.listBackups(session),
          api.listBackupDestinations(session),
          api.listSchedules(session),
          api.listGitProviders(session),
          api.listNotifications(session),
          api.listDatabaseServices(session),
          api.listSecrets(session),
          api.listConfigs(session),
          api.listNetworks(session),
          api.listSecurityRules(session),
        ]);
      const orgId = session.orgId ?? String(orgs.items[0]?.id ?? "");
      if (orgId && orgId !== session.orgId) {
        setSession({ ...session, orgId });
      }
      setDashboard({
        me,
        organizations: orgs.items,
        projects: projects.items,
        environments: environments.items,
        applications: applications.items,
        services: services.items,
        nodes: nodes.items,
        builds: builds.items,
        buildQueue: buildQueue.items,
        domains: domains.items,
        registries: registries.items,
        stacks: stacks.items,
        backups: backups.items,
        backupDestinations: backupDestinations.items,
        schedules: schedules.items,
        gitProviders: gitProviders.items,
        notifications: notifications.items,
        databaseServices: databaseServices.items,
        secrets: secrets.items,
        configs: configs.items,
        networks: networks.items,
        securityRules: securityRules.items,
      });
    } catch {
      // ignore bulk refresh errors
    }
  }, [session, setSession]);

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
      ws.onmessage = (event) => setEvents((prev) => [event.data, ...prev].slice(0, 100));
      ws.onclose = () => {
        if (cancelled) return;
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
