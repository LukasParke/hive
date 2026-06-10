import type { components } from "./generated";
export type { components };

export type Session = {
  accessToken: string;
  refreshToken: string;
  orgId?: string;
};

// Re-export generated schema types for convenience
export type User = components["schemas"]["User"];
export type Organization = components["schemas"]["Organization"];
export type Member = components["schemas"]["Member"];
export type Invitation = components["schemas"]["Invitation"];
export type ApiKey = components["schemas"]["ApiKey"];
export type Project = components["schemas"]["Project"];
export type Environment = components["schemas"]["Environment"];
export type Application = components["schemas"]["Application"];
export type Domain = components["schemas"]["Domain"];
export type Registry = components["schemas"]["Registry"];
export type Stack = components["schemas"]["Stack"];
export type SwarmSecret = components["schemas"]["SwarmSecret"];
export type SwarmConfig = components["schemas"]["SwarmConfig"];
export type Network = components["schemas"]["Network"];
export type BuildJob = components["schemas"]["BuildJob"];
export type Deployment = components["schemas"]["Deployment"];
export type Schedule = components["schemas"]["Schedule"];
export type BackupDestination = components["schemas"]["BackupDestination"];
export type DatabaseService = components["schemas"]["DatabaseService"];
export type GitProvider = components["schemas"]["GitProvider"];
export type Notification = components["schemas"]["Notification"];
export type Node = components["schemas"]["Node"];
export type Service = components["schemas"]["Service"];
export type EnvVar = components["schemas"]["EnvVar"];
export type Settings = components["schemas"]["Settings"];
export type ClusterResources = components["schemas"]["ClusterResources"];
export type NodeResources = components["schemas"]["NodeResources"];
export type PreviewDeployment = components["schemas"]["PreviewDeployment"];
export type Profile = components["schemas"]["Profile"];
export type SecurityRule = components["schemas"]["SecurityRule"];
export type UpdateStatus = components["schemas"]["UpdateStatus"];
export type StatusResponse = components["schemas"]["StatusResponse"];

// Backward-compatible generic map type for gradual migration
export type ItemMap = Record<string, unknown>;

export class ApiError extends Error {
  status: number;

  constructor(path: string, status: number, message: string) {
    super(`${path} failed with status ${status}${message ? `: ${message}` : ""}`);
    this.name = "ApiError";
    this.status = status;
  }
}

export function isAuthError(err: unknown): boolean {
  return err instanceof ApiError && err.status === 401;
}

async function readErrorMessage(res: Response): Promise<string> {
  const contentType = res.headers.get("content-type") ?? "";
  if (contentType.includes("application/json")) {
    try {
      const payload = (await res.json()) as Record<string, unknown>;
      const message = payload.message;
      if (typeof message === "string" && message.trim() !== "") return message;
      return JSON.stringify(payload);
    } catch {
      return `request failed (${res.status})`;
    }
  }
  const text = await res.text();
  return text || `request failed (${res.status})`;
}

async function request<T>(path: string, init?: RequestInit, session?: Session): Promise<T> {
  const headers = new Headers(init?.headers ?? {});
  headers.set("Content-Type", "application/json");
  if (session?.accessToken) headers.set("Authorization", `Bearer ${session.accessToken}`);
  if (session?.orgId) headers.set("X-Organization-Id", session.orgId);
  const res = await fetch(path, { ...init, headers });
  if (!res.ok) {
    const message = await readErrorMessage(res);
    throw new ApiError(path, res.status, message);
  }
  return (await res.json()) as T;
}

export const api = {
  // ── Health ──
  getHealth: () => request<{ status: "ok" }>("/api/v1/health"),

  // ── Auth ──
  register: (body: components["schemas"]["RegisterRequest"]) =>
    request<{ id: string }>("/api/v1/auth/register", { method: "POST", body: JSON.stringify(body) }),
  login: (body: components["schemas"]["LoginRequest"]) =>
    request<Session>("/api/v1/auth/login", { method: "POST", body: JSON.stringify(body) }),
  sendResetPassword: (body: components["schemas"]["SendResetPasswordRequest"]) =>
    request<ItemMap>("/api/v1/auth/send-reset-password", { method: "POST", body: JSON.stringify(body) }),
  resetPassword: (body: components["schemas"]["ResetPasswordRequest"]) =>
    request<ItemMap>("/api/v1/auth/reset-password", { method: "POST", body: JSON.stringify(body) }),
  me: (session: Session) => request<User>("/api/v1/auth/me", undefined, session),

  // ── Profile ──
  getProfile: (session: Session) => request<Profile>("/api/v1/profile", undefined, session),
  updateProfile: (session: Session, body: components["schemas"]["ProfileUpdateRequest"]) =>
    request<Profile>("/api/v1/profile", { method: "PUT", body: JSON.stringify(body) }, session),
  changePassword: (session: Session, body: components["schemas"]["ChangePasswordRequest"]) =>
    request<ItemMap>("/api/v1/profile/change-password", { method: "POST", body: JSON.stringify(body) }, session),

  // ── Invitations ──
  getInvitationByToken: (token: string) => request<Invitation>(`/api/v1/invitations/${token}`),
  acceptInvitationByToken: (session: Session, token: string) =>
    request<ItemMap>(`/api/v1/invitations/${token}/accept`, { method: "POST", body: "{}" }, session),

  // ── Organizations ──
  listOrganizations: (session: Session) => request<{ items: Organization[] }>("/api/v1/organizations", undefined, session),
  createOrganization: (session: Session, body: components["schemas"]["CreateOrganizationRequest"]) =>
    request<{ id: string }>("/api/v1/organizations", { method: "POST", body: JSON.stringify(body) }, session),
  listOrganizationMembers: (session: Session, orgId: string) =>
    request<{ items: Member[] }>(`/api/v1/organizations/${orgId}/members`, undefined, session),
  updateOrganizationMemberRole: (session: Session, orgId: string, userId: string, body: components["schemas"]["UpdateMemberRoleRequest"]) =>
    request<ItemMap>(`/api/v1/organizations/${orgId}/members/${userId}`, { method: "PUT", body: JSON.stringify(body) }, session),
  listOrganizationInvitations: (session: Session, orgId: string) =>
    request<{ items: Invitation[] }>(`/api/v1/organizations/${orgId}/invitations`, undefined, session),
  createOrganizationInvitation: (session: Session, orgId: string, body: components["schemas"]["CreateInvitationRequest"]) =>
    request<ItemMap>(`/api/v1/organizations/${orgId}/invitations`, { method: "POST", body: JSON.stringify(body) }, session),
  deleteOrganizationInvitation: (session: Session, orgId: string, inviteId: string) =>
    request<ItemMap>(`/api/v1/organizations/${orgId}/invitations/${inviteId}`, { method: "DELETE" }, session),
  resendOrganizationInvitation: (session: Session, orgId: string, inviteId: string) =>
    request<{ id: string; token: string; email: string; status: string }>(`/api/v1/organizations/${orgId}/invitations/${inviteId}/resend`, { method: "POST", body: "{}" }, session),
  revokeOrganizationInvitation: (session: Session, orgId: string, inviteId: string) =>
    request<ItemMap>(`/api/v1/organizations/${orgId}/invitations/${inviteId}/revoke`, { method: "POST", body: "{}" }, session),
  listAPIKeys: (session: Session, orgId: string) =>
    request<{ items: ApiKey[] }>(`/api/v1/organizations/${orgId}/api-keys`, undefined, session),
  createAPIKey: (session: Session, orgId: string, body: components["schemas"]["CreateApiKeyRequest"]) =>
    request<ItemMap>(`/api/v1/organizations/${orgId}/api-keys`, { method: "POST", body: JSON.stringify(body) }, session),
  deleteAPIKey: (session: Session, orgId: string, keyId: string) =>
    request<ItemMap>(`/api/v1/organizations/${orgId}/api-keys/${keyId}`, { method: "DELETE" }, session),
  regenerateAPIKey: (session: Session, orgId: string, keyId: string) =>
    request<{ token: string }>(`/api/v1/organizations/${orgId}/api-keys/${keyId}/regenerate`, { method: "POST", body: "{}" }, session),

  // ── Projects ──
  listProjects: (session: Session) => request<{ items: Project[] }>("/api/v1/projects", undefined, session),
  createProject: (session: Session, body: components["schemas"]["CreateProjectRequest"]) =>
    request<Project>("/api/v1/projects", { method: "POST", body: JSON.stringify(body) }, session),
  getProject: (session: Session, projectId: string) => request<Project>(`/api/v1/projects/${projectId}`, undefined, session),
  updateProject: (session: Session, projectId: string, body: components["schemas"]["UpdateProjectRequest"]) =>
    request<Project>(`/api/v1/projects/${projectId}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteProject: (session: Session, projectId: string) =>
    request<ItemMap>(`/api/v1/projects/${projectId}`, { method: "DELETE" }, session),

  // ── Environments ──
  listEnvironments: (session: Session) => request<{ items: Environment[] }>("/api/v1/environments", undefined, session),
  createEnvironment: (session: Session, body: components["schemas"]["CreateEnvironmentRequest"]) =>
    request<Environment>("/api/v1/environments", { method: "POST", body: JSON.stringify(body) }, session),
  getEnvironment: (session: Session, id: string) => request<Environment>(`/api/v1/environments/${id}`, undefined, session),
  updateEnvironment: (session: Session, id: string, body: components["schemas"]["UpdateEnvironmentRequest"]) =>
    request<Environment>(`/api/v1/environments/${id}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteEnvironment: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/environments/${id}`, { method: "DELETE" }, session),

  // ── Applications ──
  listApplications: (session: Session) => request<{ items: Application[] }>("/api/v1/applications", undefined, session),
  createApplication: (session: Session, body: components["schemas"]["CreateApplicationRequest"]) =>
    request<Application>("/api/v1/applications", { method: "POST", body: JSON.stringify(body) }, session),
  getApplication: (session: Session, appId: string) => request<Application>(`/api/v1/applications/${appId}`, undefined, session),
  updateApplication: (session: Session, appId: string, body: components["schemas"]["UpdateApplicationRequest"]) =>
    request<Application>(`/api/v1/applications/${appId}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteApplication: (session: Session, appId: string) =>
    request<ItemMap>(`/api/v1/applications/${appId}`, { method: "DELETE" }, session),
  deployApplication: (session: Session, appId: string) =>
    request<ItemMap>(`/api/v1/applications/${appId}/deploy`, { method: "POST", body: "{}" }, session),
  startApplication: (session: Session, appId: string) =>
    request<ItemMap>(`/api/v1/applications/${appId}/start`, { method: "POST", body: "{}" }, session),
  stopApplication: (session: Session, appId: string) =>
    request<ItemMap>(`/api/v1/applications/${appId}/stop`, { method: "POST", body: "{}" }, session),
  restartApplication: (session: Session, appId: string) =>
    request<ItemMap>(`/api/v1/applications/${appId}/restart`, { method: "POST", body: "{}" }, session),
  getApplicationLogs: (session: Session, appId: string) => request<ItemMap>(`/api/v1/applications/${appId}/logs`, undefined, session),
  rollbackApplication: (session: Session, appId: string) =>
    request<ItemMap>(`/api/v1/applications/${appId}/rollback`, { method: "POST", body: "{}" }, session),
  listApplicationDeployments: (session: Session, appId: string) =>
    request<{ items: Deployment[] }>(`/api/v1/applications/${appId}/deployments`, undefined, session),

  // ── Preview Deployments ──
  listPreviewDeployments: (session: Session, appId: string) =>
    request<{ items: PreviewDeployment[] }>(`/api/v1/applications/${appId}/previews`, undefined, session),
  createPreviewDeployment: (session: Session, appId: string, body: components["schemas"]["PreviewDeploymentCreateRequest"]) =>
    request<{ id: string }>(`/api/v1/applications/${appId}/previews`, { method: "POST", body: JSON.stringify(body) }, session),
  deletePreviewDeployment: (session: Session, appId: string, previewId: string) =>
    request<ItemMap>(`/api/v1/applications/${appId}/previews/${previewId}`, { method: "DELETE" }, session),

  // ── App Env Vars ──
  listAppEnvVars: (session: Session, appId: string) =>
    request<{ items: EnvVar[] }>(`/api/v1/applications/${appId}/env`, undefined, session),
  createAppEnvVar: (session: Session, appId: string, body: components["schemas"]["CreateEnvVarRequest"]) =>
    request<{ id: string }>(`/api/v1/applications/${appId}/env`, { method: "POST", body: JSON.stringify(body) }, session),
  updateAppEnvVar: (session: Session, appId: string, varId: string, body: components["schemas"]["UpdateEnvVarRequest"]) =>
    request<ItemMap>(`/api/v1/applications/${appId}/env/${varId}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteAppEnvVar: (session: Session, appId: string, varId: string) =>
    request<ItemMap>(`/api/v1/applications/${appId}/env/${varId}`, { method: "DELETE" }, session),

  // ── Nodes ──
  listNodes: (session: Session) => request<{ items: Node[] }>("/api/v1/nodes", undefined, session),
  getNodeMetrics: (session: Session, id: string) => request<NodeResources>(`/api/v1/nodes/${id}/metrics`, undefined, session),
  getNodePackages: (session: Session, id: string) => request<ItemMap>(`/api/v1/nodes/${id}/packages`, undefined, session),
  triggerPackageCheck: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/nodes/${id}/packages/check`, { method: "POST", body: "{}" }, session),
  triggerNodeMaintenance: (session: Session, id: string, body: components["schemas"]["NodeMaintenanceRequest"]) =>
    request<ItemMap>(`/api/v1/nodes/${id}/maintain`, { method: "POST", body: JSON.stringify(body) }, session),

  // ── Services ──
  listServices: (session: Session) => request<{ items: Service[] }>("/api/v1/services", undefined, session),

  // ── Metrics ──
  getMetrics: (session: Session) => request<ItemMap>("/api/v1/metrics", undefined, session),

  // ── Builds ──
  listBuilds: (session: Session) => request<{ items: BuildJob[] }>("/api/v1/builds", undefined, session),
  listBuildQueue: (session: Session) => request<{ items: BuildJob[] }>("/api/v1/builds/queue", undefined, session),
  cancelBuild: (session: Session, id: string) => request<ItemMap>(`/api/v1/builds/${id}/cancel`, { method: "POST", body: "{}" }, session),
  retryBuild: (session: Session, id: string) => request<ItemMap>(`/api/v1/builds/${id}/retry`, { method: "POST", body: "{}" }, session),

  // ── Deployments ──
  listDeployments: (session: Session) => request<{ items: Deployment[] }>("/api/v1/deployments", undefined, session),
  deleteDeployment: (session: Session, id: string) => request<ItemMap>(`/api/v1/deployments/${id}`, { method: "DELETE" }, session),

  // ── Domains ──
  listDomains: (session: Session) => request<{ items: Domain[] }>("/api/v1/domains", undefined, session),
  createDomain: (session: Session, body: components["schemas"]["CreateDomainRequest"]) =>
    request<Domain>("/api/v1/domains", { method: "POST", body: JSON.stringify(body) }, session),
  getDomain: (session: Session, id: string) => request<Domain>(`/api/v1/domains/${id}`, undefined, session),
  updateDomain: (session: Session, id: string, body: components["schemas"]["UpdateDomainRequest"]) =>
    request<Domain>(`/api/v1/domains/${id}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteDomain: (session: Session, id: string) => request<ItemMap>(`/api/v1/domains/${id}`, { method: "DELETE" }, session),

  // ── Registries ──
  listRegistries: (session: Session) => request<{ items: Registry[] }>("/api/v1/registries", undefined, session),
  createRegistry: (session: Session, body: components["schemas"]["CreateRegistryRequest"]) =>
    request<Registry>("/api/v1/registries", { method: "POST", body: JSON.stringify(body) }, session),
  getRegistry: (session: Session, id: string) => request<Registry>(`/api/v1/registries/${id}`, undefined, session),
  updateRegistry: (session: Session, id: string, body: components["schemas"]["UpdateRegistryRequest"]) =>
    request<Registry>(`/api/v1/registries/${id}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteRegistry: (session: Session, id: string) => request<ItemMap>(`/api/v1/registries/${id}`, { method: "DELETE" }, session),
  testRegistry: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/registries/${id}/test`, { method: "POST", body: "{}" }, session),

  // ── Stacks ──
  listStacks: (session: Session) => request<{ items: Stack[] }>("/api/v1/stacks", undefined, session),
  createStack: (session: Session, body: components["schemas"]["CreateStackRequest"]) =>
    request<Stack>("/api/v1/stacks", { method: "POST", body: JSON.stringify(body) }, session),
  getStack: (session: Session, id: string) => request<Stack>(`/api/v1/stacks/${id}`, undefined, session),
  updateStack: (session: Session, id: string, body: components["schemas"]["UpdateStackRequest"]) =>
    request<Stack>(`/api/v1/stacks/${id}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteStack: (session: Session, id: string) => request<ItemMap>(`/api/v1/stacks/${id}`, { method: "DELETE" }, session),
  deployStack: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/stacks/${id}/deploy`, { method: "POST", body: "{}" }, session),
  startStack: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/stacks/${id}/start`, { method: "POST", body: "{}" }, session),
  stopStack: (session: Session, id: string) => request<ItemMap>(`/api/v1/stacks/${id}/stop`, { method: "POST", body: "{}" }, session),
  restartStack: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/stacks/${id}/restart`, { method: "POST", body: "{}" }, session),

  // ── Secrets ──
  listSecrets: (session: Session) => request<{ items: SwarmSecret[] }>("/api/v1/secrets", undefined, session),
  createSecret: (session: Session, body: ItemMap) =>
    request<ItemMap>("/api/v1/secrets", { method: "POST", body: JSON.stringify(body) }, session),

  // ── Configs ──
  listConfigs: (session: Session) => request<{ items: SwarmConfig[] }>("/api/v1/configs", undefined, session),
  createConfig: (session: Session, body: ItemMap) =>
    request<ItemMap>("/api/v1/configs", { method: "POST", body: JSON.stringify(body) }, session),

  // ── Networks ──
  listNetworks: (session: Session) => request<{ items: Network[] }>("/api/v1/networks", undefined, session),
  createNetwork: (session: Session, body: ItemMap) =>
    request<ItemMap>("/api/v1/networks", { method: "POST", body: JSON.stringify(body) }, session),

  // ── Backups ──
  listBackups: (session: Session) => request<{ items: ItemMap[] }>("/api/v1/backups", undefined, session),
  createBackup: (session: Session, body: ItemMap) =>
    request<ItemMap>("/api/v1/backups", { method: "POST", body: JSON.stringify(body) }, session),
  restoreBackup: (session: Session, backupId: string, restoreTarget: string) =>
    request<ItemMap>(`/api/v1/backups/${backupId}/restore`, { method: "POST", body: JSON.stringify({ restoreTarget }) }, session),

  // ── Backup Destinations ──
  listBackupDestinations: (session: Session) => request<{ items: BackupDestination[] }>("/api/v1/backup/destinations", undefined, session),
  createBackupDestination: (session: Session, body: components["schemas"]["CreateBackupDestinationRequest"]) =>
    request<BackupDestination>("/api/v1/backup/destinations", { method: "POST", body: JSON.stringify(body) }, session),
  getBackupDestination: (session: Session, id: string) => request<BackupDestination>(`/api/v1/backup/destinations/${id}`, undefined, session),
  updateBackupDestination: (session: Session, id: string, body: components["schemas"]["UpdateBackupDestinationRequest"]) =>
    request<BackupDestination>(`/api/v1/backup/destinations/${id}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteBackupDestination: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/backup/destinations/${id}`, { method: "DELETE" }, session),
  testBackupDestination: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/backup/destinations/${id}/test`, { method: "POST", body: "{}" }, session),

  // ── Schedules ──
  listSchedules: (session: Session) => request<{ items: Schedule[] }>("/api/v1/schedules", undefined, session),
  createSchedule: (session: Session, body: components["schemas"]["CreateScheduleRequest"]) =>
    request<Schedule>("/api/v1/schedules", { method: "POST", body: JSON.stringify(body) }, session),
  updateSchedule: (session: Session, id: string, body: components["schemas"]["UpdateScheduleRequest"]) =>
    request<Schedule>(`/api/v1/schedules/${id}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteSchedule: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/schedules/${id}`, { method: "DELETE" }, session),
  runScheduleNow: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/schedules/${id}/run`, { method: "POST", body: "{}" }, session),

  // ── Git Providers ──
  listGitProviders: (session: Session) => request<{ items: GitProvider[] }>("/api/v1/git/providers", undefined, session),
  createGitProvider: (session: Session, body: components["schemas"]["CreateGitProviderRequest"]) =>
    request<GitProvider>("/api/v1/git/providers", { method: "POST", body: JSON.stringify(body) }, session),

  // ── Notifications ──
  listNotifications: (session: Session) => request<{ items: Notification[] }>("/api/v1/notifications", undefined, session),
  createNotification: (session: Session, body: components["schemas"]["CreateNotificationRequest"]) =>
    request<Notification>("/api/v1/notifications", { method: "POST", body: JSON.stringify(body) }, session),
  getNotification: (session: Session, id: string) => request<Notification>(`/api/v1/notifications/${id}`, undefined, session),
  updateNotification: (session: Session, id: string, body: components["schemas"]["UpdateNotificationRequest"]) =>
    request<Notification>(`/api/v1/notifications/${id}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteNotification: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/notifications/${id}`, { method: "DELETE" }, session),
  testNotification: (session: Session, id: string) =>
    request<ItemMap>(`/api/v1/notifications/${id}/test`, { method: "POST", body: "{}" }, session),

  // ── Database Services ──
  listDatabaseServices: (session: Session) => request<{ items: DatabaseService[] }>("/api/v1/database-services", undefined, session),
  createDatabaseService: (session: Session, body: components["schemas"]["CreateDatabaseServiceRequest"]) =>
    request<DatabaseService>("/api/v1/database-services", { method: "POST", body: JSON.stringify(body) }, session),
  getDatabaseService: (session: Session, id: string) => request<DatabaseService>(`/api/v1/database-services/${id}`, undefined, session),

  // ── Security Rules ──
  listSecurityRules: (session: Session) => request<{ items: SecurityRule[] }>("/api/v1/security-rules", undefined, session),
  createSecurityRule: (session: Session, body: components["schemas"]["SecurityRuleCreateRequest"]) =>
    request<SecurityRule>("/api/v1/security-rules", { method: "POST", body: JSON.stringify(body) }, session),
  getSecurityRule: (session: Session, id: string) => request<SecurityRule>(`/api/v1/security-rules/${id}`, undefined, session),
  updateSecurityRule: (session: Session, id: string, body: components["schemas"]["SecurityRuleCreateRequest"]) =>
    request<SecurityRule>(`/api/v1/security-rules/${id}`, { method: "PUT", body: JSON.stringify(body) }, session),
  deleteSecurityRule: (session: Session, id: string) => request<ItemMap>(`/api/v1/security-rules/${id}`, { method: "DELETE" }, session),

  // ── System Update ──
  getUpdateStatus: (session: Session) => request<components["schemas"]["UpdateStatus"]>("/api/v1/system/update", undefined, session),
  triggerUpdate: (session: Session) => request<StatusResponse>("/api/v1/system/update", { method: "POST", body: "{}" }, session),

  // ── Cluster Resources ──
  getClusterResources: (session: Session) => request<ClusterResources>("/api/v1/cluster/resources", undefined, session),

  // ── Settings ──
  getSettings: (session: Session) => request<Settings>("/api/v1/settings", undefined, session),
  putSettings: (session: Session, body: ItemMap) =>
    request<ItemMap>("/api/v1/settings", { method: "PUT", body: JSON.stringify(body) }, session),
  listSettingsServers: (session: Session) => request<{ items: ItemMap[] }>("/api/v1/settings/servers", undefined, session),
  createSettingsServer: (session: Session, body: ItemMap) =>
    request<ItemMap>("/api/v1/settings/servers", { method: "POST", body: JSON.stringify(body) }, session),
  getClusterInfo: (session: Session) => request<ItemMap>("/api/v1/settings/cluster", undefined, session),
  listSSHKeys: (session: Session) => request<{ items: ItemMap[] }>("/api/v1/settings/ssh-keys", undefined, session),
  createSSHKey: (session: Session, body: ItemMap) =>
    request<ItemMap>("/api/v1/settings/ssh-keys", { method: "POST", body: JSON.stringify(body) }, session),
  listCertificates: (session: Session) => request<{ items: ItemMap[] }>("/api/v1/settings/certificates", undefined, session),
  createCertificate: (session: Session, body: ItemMap) =>
    request<ItemMap>("/api/v1/settings/certificates", { method: "POST", body: JSON.stringify(body) }, session),
  listRequestEvents: (session: Session) => request<{ items: ItemMap[] }>("/api/v1/settings/requests", undefined, session),
};
