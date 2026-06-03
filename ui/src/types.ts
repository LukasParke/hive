import type { ItemMap, Session } from "./api/client";

// ── Typed interfaces ──

export interface Project {
  id: string;
  name: string;
  createdAt: string;
}

export interface Application {
  id: string;
  projectId: string;
  name: string;
  sourceType: "git" | "image" | "compose";
  image?: string;
  repositoryUrl?: string;
  gitRef?: string;
  containerPort?: number;
  watchPaths?: string[];
  createdAt: string;
}

export interface Stack {
  id: string;
  projectId: string;
  name: string;
  composeContent: string;
  createdAt: string;
}

export interface DatabaseService {
  id: string;
  projectId: string;
  engine: string;
  name: string;
  version?: string;
  serviceName?: string;
  databaseName?: string;
  port?: number;
  username?: string;
  passwordSecretName?: string;
  createdAt: string;
}

export interface Domain {
  id: string;
  applicationId: string;
  hostname: string;
  tlsEnabled?: boolean;
  createdAt: string;
}

export interface Registry {
  id: string;
  name: string;
  url: string;
  username?: string;
  secretName?: string;
  isDefault?: boolean;
  createdAt: string;
}

export interface Secret {
  id: string;
  name: string;
  createdAt: string;
}

export interface Config {
  id: string;
  name: string;
  createdAt: string;
}

export interface Network {
  id: string;
  name: string;
  driver?: string;
}

export interface Node {
  id: string;
  hostname: string;
  status: string;
  createdAt: string;
}

export interface Deployment {
  id: string;
  applicationId: string;
  applicationName?: string;
  imageTag?: string;
  status: string;
  trigger?: string;
  createdAt: string;
}

export interface Build {
  id: string;
  applicationId: string;
  status: string;
  trigger?: string;
  imageTag?: string;
  retries?: number;
  createdAt: string;
}

export interface Schedule {
  id: string;
  name: string;
  cronExpr: string;
  targetType: string;
  targetId: string;
  enabled: boolean;
  lastRunAt?: string;
  createdAt: string;
}

export interface Notification {
  id: string;
  channel: string;
  target: string;
  enabled: boolean;
  createdAt: string;
}

export interface BackupDestination {
  id: string;
  name: string;
  type: string;
  config: Record<string, unknown>;
  createdAt: string;
}

export interface Backup {
  id: string;
  targetType: string;
  targetId: string;
  status: string;
  artifactPath?: string;
  destinationId?: string;
  createdAt: string;
}

export interface GitProvider {
  id: string;
  type: string;
  name: string;
  baseUrl?: string;
  enabled?: boolean;
  createdAt: string;
}

export interface AppEnvVar {
  id: string;
  key: string;
  value: string | null;
  isSecret: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface Environment {
  id: string;
  projectId: string;
  name: string;
  slug: string;
  createdAt: string;
}

export interface Organization {
  id: string;
  name: string;
  slug: string;
  role?: string;
}

export interface Member {
  userId: string;
  email: string;
  role: string;
  createdAt: string;
}

export interface Settings {
  [key: string]: unknown;
}

// ── Dashboard state (backward compat with ItemMap) ──

export type DashboardState = {
  me?: ItemMap;
  organizations: ItemMap[];
  projects: ItemMap[];
  environments: ItemMap[];
  applications: ItemMap[];
  services: ItemMap[];
  nodes: ItemMap[];
  builds: ItemMap[];
  buildQueue: ItemMap[];
  domains: ItemMap[];
  registries: ItemMap[];
  stacks: ItemMap[];
  backups: ItemMap[];
  backupDestinations: ItemMap[];
  schedules: ItemMap[];
  gitProviders: ItemMap[];
  notifications: ItemMap[];
  databaseServices: ItemMap[];
  secrets: ItemMap[];
  configs: ItemMap[];
  networks: ItemMap[];
  securityRules: ItemMap[];
};

export const initialDashboard: DashboardState = {
  organizations: [],
  projects: [],
  environments: [],
  applications: [],
  services: [],
  nodes: [],
  builds: [],
  buildQueue: [],
  domains: [],
  registries: [],
  stacks: [],
  backups: [],
  backupDestinations: [],
  schedules: [],
  gitProviders: [],
  notifications: [],
  databaseServices: [],
  secrets: [],
  configs: [],
  networks: [],
  securityRules: [],
};

export type AppState = {
  session: Session | null;
  dashboard: DashboardState;
  error: string | null;
};
