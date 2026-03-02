const API_BASE = '/api/v1';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
	const res = await fetch(`${API_BASE}${path}`, {
		credentials: 'include',
		headers: {
			'Content-Type': 'application/json',
			...options?.headers,
		},
		...options,
	});

	if (!res.ok) {
		const error = await res.json().catch(() => ({ error: res.statusText }));
		throw new Error(error.error || res.statusText);
	}

	return res.json();
}

export const api = {
	// System
	status: () => request<SystemStatus>('/system/status'),

	// Projects
	listProjects: () => request<Project[]>('/projects'),
	createProject: (data: { name: string; description?: string }) =>
		request<Project>('/projects', { method: 'POST', body: JSON.stringify(data) }),
	getProject: (id: string) => request<Project>(`/projects/${id}`),
	deleteProject: (id: string) => request<void>(`/projects/${id}`, { method: 'DELETE' }),

	// Apps
	listApps: (projectId: string) => request<App[]>(`/projects/${projectId}/apps`),
	createApp: (projectId: string, data: CreateAppRequest) =>
		request<App>(`/projects/${projectId}/apps`, { method: 'POST', body: JSON.stringify(data) }),
	getApp: (projectId: string, appId: string) => request<App>(`/projects/${projectId}/apps/${appId}`),
	getAppTasks: (projectId: string, appId: string) =>
		request<TaskInfo[]>(`/projects/${projectId}/apps/${appId}/tasks`),
	getAppEvents: (projectId: string, appId: string) =>
		request<ServiceEvent[]>(`/projects/${projectId}/apps/${appId}/events`),
	getAppPorts: (projectId: string, appId: string) =>
		request<PortMapping[]>(`/projects/${projectId}/apps/${appId}/ports`),
	deleteApp: (projectId: string, appId: string) =>
		request<void>(`/projects/${projectId}/apps/${appId}`, { method: 'DELETE' }),
	deployApp: (projectId: string, appId: string) =>
		request<Deployment>(`/projects/${projectId}/apps/${appId}/deploy`, { method: 'POST' }),
	listDeployments: (projectId: string, appId: string) =>
		request<Deployment[]>(`/projects/${projectId}/apps/${appId}/deployments`),
	restartApp: (projectId: string, appId: string) =>
		request<{ status: string }>(`/projects/${projectId}/apps/${appId}/restart`, { method: 'POST' }),
	stopApp: (projectId: string, appId: string) =>
		request<{ status: string }>(`/projects/${projectId}/apps/${appId}/stop`, { method: 'POST' }),
	startApp: (projectId: string, appId: string) =>
		request<{ status: string }>(`/projects/${projectId}/apps/${appId}/start`, { method: 'POST' }),
	scaleApp: (projectId: string, appId: string, replicas: number) =>
		request<{ scaled: string }>(`/projects/${projectId}/apps/${appId}/scale`, {
			method: 'PUT', body: JSON.stringify({ replicas })
		}),
	rollbackApp: (projectId: string, appId: string) =>
		request<{ status: string }>(`/projects/${projectId}/apps/${appId}/rollback`, { method: 'POST' }),
	updateAppResources: (projectId: string, appId: string, data: { cpu_limit: number; memory_limit: number }) =>
		request<{ updated: string }>(`/projects/${projectId}/apps/${appId}/resources`, {
			method: 'PUT', body: JSON.stringify(data)
		}),
	updateAppHealthCheck: (projectId: string, appId: string, data: { path: string; interval: number }) =>
		request<{ updated: string }>(`/projects/${projectId}/apps/${appId}/healthcheck`, {
			method: 'PUT', body: JSON.stringify(data)
		}),

	// Databases
	listDatabases: (projectId: string) => request<ManagedDatabase[]>(`/projects/${projectId}/databases`),
	createDatabase: (projectId: string, data: { name: string; db_type: string; version?: string }) =>
		request<ManagedDatabase>(`/projects/${projectId}/databases`, { method: 'POST', body: JSON.stringify(data) }),

	// Nodes
	listNodes: () => request<{ nodes: SwarmNode[]; join_tokens?: { worker: string; manager: string } }>('/nodes'),
	getNode: (id: string) => request<SwarmNode>(`/nodes/${id}`),

	// Templates (marketplace - built-in + custom)
	listTemplates: () => request<TemplateListItem[]>('/templates'),
	getTemplate: (name: string) => request<TemplateDetail>(`/templates/${encodeURIComponent(name)}`),
	deployTemplate: (name: string, data: DeployTemplateRequest) =>
		request<App | { stack: Stack }>(`/templates/${encodeURIComponent(name)}/deploy`, {
			method: 'POST',
			body: JSON.stringify(data)
		}),
	checkTemplateUpdates: (name: string) =>
		request<{ update_available: boolean; current_version: string; latest_version: string }>(
			`/templates/${encodeURIComponent(name)}/updates`
		),

	// Template sources
	listTemplateSources: () => request<TemplateSource[]>('/template-sources'),
	createTemplateSource: (data: { name: string; url: string; type?: string }) =>
		request<TemplateSource>('/template-sources', { method: 'POST', body: JSON.stringify(data) }),
	deleteTemplateSource: (id: string) =>
		request<{ deleted: string }>(`/template-sources/${id}`, { method: 'DELETE' }),
	syncTemplateSource: (id: string) =>
		request<{ synced: boolean; imported: number }>(`/template-sources/${id}/sync`, {
			method: 'POST'
		}),

	// Export app as template
	exportAppAsTemplate: (projectId: string, appId: string) =>
		request<CustomTemplate>(
			`/projects/${projectId}/apps/${appId}/export-template`,
			{ method: 'POST' }
		),

	// Custom templates management
	listCustomTemplates: () => request<CustomTemplate[]>('/custom-templates'),
	updateCustomTemplate: (id: string, data: Partial<CustomTemplate>) =>
		request<CustomTemplate>(`/custom-templates/${id}`, {
			method: 'PUT',
			body: JSON.stringify(data)
		}),
	deleteCustomTemplate: (id: string) =>
		request<{ deleted: string }>(`/custom-templates/${id}`, { method: 'DELETE' }),

	// Secrets
	listSecrets: (projectId: string) => request<Secret[]>(`/projects/${projectId}/secrets`),
	createSecret: (projectId: string, data: { name: string; value: string; description?: string }) =>
		request<Secret>(`/projects/${projectId}/secrets`, { method: 'POST', body: JSON.stringify(data) }),
	deleteSecret: (projectId: string, secretId: string) =>
		request<void>(`/projects/${projectId}/secrets/${secretId}`, { method: 'DELETE' }),
	attachSecret: (projectId: string, secretId: string, appId: string, data?: { target?: string; uid?: string; gid?: string; mode?: number }) =>
		request<AppSecret>(`/projects/${projectId}/secrets/${secretId}/attach/${appId}`, { method: 'POST', body: JSON.stringify(data ?? {}) }),
	detachSecret: (projectId: string, secretId: string, appId: string) =>
		request<void>(`/projects/${projectId}/secrets/${secretId}/detach/${appId}`, { method: 'DELETE' }),

	// Volumes
	listVolumes: (projectId: string) => request<Volume[]>(`/projects/${projectId}/volumes`),
	createVolume: (projectId: string, data: CreateVolumeRequest) =>
		request<Volume>(`/projects/${projectId}/volumes`, { method: 'POST', body: JSON.stringify(data) }),
	getVolume: (projectId: string, volumeId: string) => request<Volume>(`/projects/${projectId}/volumes/${volumeId}`),
	deleteVolume: (projectId: string, volumeId: string) =>
		request<void>(`/projects/${projectId}/volumes/${volumeId}`, { method: 'DELETE' }),
	attachVolume: (projectId: string, volumeId: string, appId: string, data: { container_path: string; read_only?: boolean }) =>
		request<AppVolume>(`/projects/${projectId}/volumes/${volumeId}/attach/${appId}`, { method: 'POST', body: JSON.stringify(data) }),
	detachVolume: (projectId: string, volumeId: string, appId: string) =>
		request<void>(`/projects/${projectId}/volumes/${volumeId}/detach/${appId}`, { method: 'DELETE' }),

	// Backups
	listBackupConfigs: () => request<BackupConfig[]>('/backups'),
	createBackupConfig: (data: { resource_id?: string; schedule: string; s3_bucket?: string; s3_prefix?: string; backup_type?: string; volume_id?: string }) =>
		request<BackupConfig>('/backups', { method: 'POST', body: JSON.stringify(data) }),
	triggerBackup: (configId: string) =>
		request<{ status: string }>(`/backups/${configId}/trigger`, { method: 'POST' }),
	listBackupRuns: (configId: string) => request<BackupRun[]>(`/backups/${configId}/runs`),
	restoreBackup: (configId: string, runId: string) =>
		request<{ status: string }>(`/backups/${configId}/restore/${runId}`, { method: 'POST' }),

	// Metrics
	metricsServices: () => request<ServiceHealth[]>('/metrics/services'),
	metricsNodes: () => request<NodeMetrics>('/metrics/nodes'),

	// Notifications
	listNotificationChannels: () => request<NotificationChannel[]>('/notifications'),
	createNotificationChannel: (data: { name?: string; type: string; config: Record<string, string> }) =>
		request<NotificationChannel>('/notifications', { method: 'POST', body: JSON.stringify(data) }),
	deleteNotificationChannel: (id: string) =>
		request<void>(`/notifications/${id}`, { method: 'DELETE' }),
	testNotificationChannel: (id: string) =>
		request<{ status: string }>(`/notifications/${id}/test`, { method: 'POST' }),

	// Proxy Routes
	listProxyRoutes: (projectId: string) => request<ProxyRoute[]>(`/projects/${projectId}/routes`),
	createProxyRoute: (projectId: string, data: CreateProxyRouteRequest) =>
		request<ProxyRoute>(`/projects/${projectId}/routes`, { method: 'POST', body: JSON.stringify(data) }),
	updateProxyRoute: (projectId: string, routeId: string, data: Partial<CreateProxyRouteRequest>) =>
		request<ProxyRoute>(`/projects/${projectId}/routes/${routeId}`, { method: 'PUT', body: JSON.stringify(data) }),
	deleteProxyRoute: (projectId: string, routeId: string) =>
		request<void>(`/projects/${projectId}/routes/${routeId}`, { method: 'DELETE' }),

	// Certificates
	listCertificates: (projectId: string) => request<CustomCertificate[]>(`/projects/${projectId}/certificates`),
	createCertificate: (projectId: string, data: { domain: string; cert_pem: string; key_pem: string; is_wildcard?: boolean }) =>
		request<CustomCertificate>(`/projects/${projectId}/certificates`, { method: 'POST', body: JSON.stringify(data) }),
	deleteCertificate: (projectId: string, certId: string) =>
		request<void>(`/projects/${projectId}/certificates/${certId}`, { method: 'DELETE' }),

	// Stacks
	listStacks: (projectId: string) => request<Stack[]>(`/projects/${projectId}/stacks`),
	createStack: (projectId: string, data: { name: string; compose_content: string }) =>
		request<Stack>(`/projects/${projectId}/stacks`, { method: 'POST', body: JSON.stringify(data) }),
	getStack: (projectId: string, stackId: string) => request<Stack>(`/projects/${projectId}/stacks/${stackId}`),
	updateStack: (projectId: string, stackId: string, data: { compose_content: string }) =>
		request<Stack>(`/projects/${projectId}/stacks/${stackId}`, { method: 'PUT', body: JSON.stringify(data) }),
	deleteStack: (projectId: string, stackId: string) =>
		request<void>(`/projects/${projectId}/stacks/${stackId}`, { method: 'DELETE' }),

	// Node labels
	updateNodeLabels: (nodeId: string, labels: Record<string, string>) =>
		request<{ updated: string }>(`/nodes/${nodeId}/labels`, { method: 'PUT', body: JSON.stringify({ labels }) }),

	// App placement + strategy + labels
	updateAppPlacement: (projectId: string, appId: string, data: { constraints: string[]; preferences: string[] }) =>
		request<{ updated: string }>(`/projects/${projectId}/apps/${appId}/placement`, { method: 'PUT', body: JSON.stringify(data) }),
	updateAppUpdateStrategy: (projectId: string, appId: string, data: UpdateStrategyRequest) =>
		request<{ updated: string }>(`/projects/${projectId}/apps/${appId}/update-strategy`, { method: 'PUT', body: JSON.stringify(data) }),
	updateAppLabels: (projectId: string, appId: string, data: { homepage_labels: Record<string, string>; extra_labels: Record<string, string> }) =>
		request<{ updated: string }>(`/projects/${projectId}/apps/${appId}/labels`, { method: 'PUT', body: JSON.stringify(data) }),

	// Env Vars
	listEnvVars: (projectId: string, appId: string) =>
		request<AppEnvVar[]>(`/projects/${projectId}/apps/${appId}/env-vars`),
	setEnvVar: (projectId: string, appId: string, data: { key: string; value: string; is_secret?: boolean }) =>
		request<AppEnvVar>(`/projects/${projectId}/apps/${appId}/env-vars`, {
			method: 'POST',
			body: JSON.stringify(data),
		}),
	deleteEnvVar: (projectId: string, appId: string, key: string) =>
		request<{ deleted: string }>(`/projects/${projectId}/apps/${appId}/env-vars/${encodeURIComponent(key)}`, {
			method: 'DELETE',
		}),
	importEnvVars: (projectId: string, appId: string, content: string) =>
		request<{ imported: number; message: string }>(`/projects/${projectId}/apps/${appId}/env-vars/import`, {
			method: 'POST',
			body: JSON.stringify({ content }),
		}),
	exportEnvVars: async (projectId: string, appId: string): Promise<string> => {
		const res = await fetch(`${API_BASE}/projects/${projectId}/apps/${appId}/env-vars/export`, {
			credentials: 'include',
		});
		if (!res.ok) {
			const err = await res.json().catch(() => ({ error: res.statusText }));
			throw new Error(err.error || res.statusText);
		}
		return res.text();
	},

	// Service Links
	listServiceLinks: (projectId: string, appId: string) => request<ServiceLink[]>(`/projects/${projectId}/apps/${appId}/links`),
	createServiceLink: (projectId: string, appId: string, data: { target_app_id?: string; target_database_id?: string; env_prefix: string }) =>
		request<ServiceLink>(`/projects/${projectId}/apps/${appId}/links`, { method: 'POST', body: JSON.stringify(data) }),
	deleteServiceLink: (projectId: string, appId: string, linkId: string) =>
		request<void>(`/projects/${projectId}/apps/${appId}/links/${linkId}`, { method: 'DELETE' }),

	// Previews
	listPreviews: (projectId: string, appId: string) => request<PreviewDeployment[]>(`/projects/${projectId}/apps/${appId}/previews`),
	deletePreview: (projectId: string, appId: string, previewId: string) =>
		request<void>(`/projects/${projectId}/apps/${appId}/previews/${previewId}`, { method: 'DELETE' }),

	// Logs
	queryAppLogs: (
		projectId: string,
		appId: string,
		params?: { since?: string; until?: string; search?: string; level?: string; limit?: number }
	) => {
		const sp = new URLSearchParams();
		if (params?.since) sp.set('since', params.since);
		if (params?.until) sp.set('until', params.until);
		if (params?.search) sp.set('search', params.search);
		if (params?.level) sp.set('level', params.level);
		if (params?.limit) sp.set('limit', String(params.limit));
		const q = sp.toString();
		return request<LogEntry[]>(`/projects/${projectId}/apps/${appId}/logs/query${q ? '?' + q : ''}`);
	},
	getSystemLogs: (params?: { since?: string; until?: string; search?: string; level?: string; limit?: number }) => {
		const sp = new URLSearchParams();
		if (params?.since) sp.set('since', params.since);
		if (params?.until) sp.set('until', params.until);
		if (params?.search) sp.set('search', params.search);
		if (params?.level) sp.set('level', params.level);
		if (params?.limit) sp.set('limit', String(params.limit));
		const q = sp.toString();
		return request<LogEntry[]>(`/system/logs${q ? '?' + q : ''}`);
	},

	// Log forwards
	listLogForwards: () => request<LogForwardConfig[]>('/log-forwards'),
	createLogForward: (data: { name: string; type?: string; config?: Record<string, unknown> }) =>
		request<LogForwardConfig>('/log-forwards', { method: 'POST', body: JSON.stringify(data) }),
	deleteLogForward: (id: string) => request<void>(`/log-forwards/${id}`, { method: 'DELETE' }),

	// Registry
	registryStatus: () => request<RegistryStatus>('/registry/status'),
	registryImages: () => request<RegistryImage[]>('/registry/images'),
	registryDeleteImage: (name: string, tag: string) =>
		request<void>(`/registry/images/${name}/${tag}`, { method: 'DELETE' }),

	// Connectivity
	checkConnectivity: () => request<ConnectivityResult>('/system/connectivity'),

	// Alert thresholds
	listAlertThresholds: () => request<AlertThreshold[]>('/alerts'),
	createAlertThreshold: (data: { metric: string; operator: string; value: number; cooldown_minutes?: number }) =>
		request<AlertThreshold>('/alerts', { method: 'POST', body: JSON.stringify(data) }),
	deleteAlertThreshold: (id: string) =>
		request<void>(`/alerts/${id}`, { method: 'DELETE' }),

	// Storage Hosts
	listStorageHosts: () => request<StorageHost[]>('/storage-hosts'),
	createStorageHost: (data: CreateStorageHostRequest) =>
		request<StorageHost>('/storage-hosts', { method: 'POST', body: JSON.stringify(data) }),
	getStorageHost: (id: string) => request<StorageHost>(`/storage-hosts/${id}`),
	updateStorageHost: (id: string, data: Partial<CreateStorageHostRequest>) =>
		request<StorageHost>(`/storage-hosts/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
	deleteStorageHost: (id: string) =>
		request<void>(`/storage-hosts/${id}`, { method: 'DELETE' }),
	testStorageHostConnectivity: (id: string) =>
		request<StorageHostTestResult>(`/storage-hosts/${id}/test`, { method: 'POST' }),

	// Members
	listOrgMembers: () => request<OrgRole[]>('/members'),
	inviteMember: (data: { user_id: string; role: string }) =>
		request<OrgRole>('/members', { method: 'POST', body: JSON.stringify(data) }),
	updateMemberRole: (userId: string, role: string) =>
		request<OrgRole>(`/members/${userId}/role`, { method: 'PUT', body: JSON.stringify({ role }) }),
	removeMember: (userId: string) =>
		request<void>(`/members/${userId}`, { method: 'DELETE' }),

	// Audit
	listAuditLogs: (params?: string) =>
		request<AuditLogEntry[]>(`/audit${params ? '?' + params : ''}`),
	getAuditLogStats: () => request<Record<string, number>>('/audit/stats'),

	// Maintenance
	listMaintenanceTasks: () => request<MaintenanceTask[]>('/maintenance'),
	createMaintenanceTask: (data: {
		type: string;
		schedule: string;
		config?: Record<string, unknown>;
	}) =>
		request<MaintenanceTask>('/maintenance', {
			method: 'POST',
			body: JSON.stringify(data)
		}),
	updateMaintenanceTask: (taskId: string, data: Partial<MaintenanceTask>) =>
		request<MaintenanceTask>(`/maintenance/${taskId}`, {
			method: 'PUT',
			body: JSON.stringify(data)
		}),
	deleteMaintenanceTask: (taskId: string) =>
		request<void>(`/maintenance/${taskId}`, { method: 'DELETE' }),
	triggerMaintenanceTask: (taskId: string) =>
		request<{ status: string }>(`/maintenance/${taskId}/trigger`, { method: 'POST' }),
	listMaintenanceRuns: (taskId: string) =>
		request<MaintenanceRun[]>(`/maintenance/${taskId}/runs`),

	// DNS Providers
	listDNSProviders: () => request<DNSProvider[]>('/dns-providers'),
	createDNSProvider: (data: { name: string; type: string; config: Record<string, string>; is_default?: boolean }) =>
		request<DNSProvider>('/dns-providers', { method: 'POST', body: JSON.stringify(data) }),
	deleteDNSProvider: (id: string) =>
		request<void>(`/dns-providers/${id}`, { method: 'DELETE' }),
	testDNSProvider: (id: string) =>
		request<{ status: string }>(`/dns-providers/${id}/test`, { method: 'POST' }),
	listDNSRecords: (providerId: string) => request<DNSRecord[]>(`/dns-providers/${providerId}/records`),
	deleteDNSRecord: (providerId: string, recordId: string) =>
		request<void>(`/dns-providers/${providerId}/records/${recordId}`, { method: 'DELETE' }),

	// Cluster Metrics (agent-sourced)
	getClusterMetrics: () => request<NodeMetricsReport[]>('/nodes/metrics'),
	getNodeMetrics: (nodeId: string) => request<{ latest: NodeMetricsReport; history: NodeMetricsReport[] }>(`/nodes/${nodeId}/metrics`),
	getNodeMetricsHistory: (nodeId: string, range: string) =>
		request<NodeMetricsReport[]>(`/nodes/${nodeId}/metrics/history?range=${range}`),

	// Git Sources
	listGitSources: () => request<GitSource[]>('/git-sources'),
	createGitSource: (data: { provider: string; token: string }) =>
		request<{ id: string; provider: string }>('/git-sources', { method: 'POST', body: JSON.stringify(data) }),
	listGitRepos: (sourceId: string) => request<GitRepository[]>(`/git-sources/${sourceId}/repos`),
	listGitRepoBranches: (sourceId: string, repo: string) =>
		request<GitBranch[]>(`/git-sources/${sourceId}/repos/${encodeURIComponent(repo)}/branches`),
	registerWebhook: (sourceId: string, repo: string) =>
		request<{ webhook_id: string; status: string }>(`/git-sources/${sourceId}/repos/${encodeURIComponent(repo)}/webhook`, { method: 'POST' }),
	detectBuildType: (sourceId: string, repo: string, branch?: string) =>
		request<{ build_type: string }>(`/git-sources/${sourceId}/repos/${encodeURIComponent(repo)}/detect${branch ? '?branch=' + encodeURIComponent(branch) : ''}`),

	// Ceph Clusters
	listCephClusters: () => request<CephClusterWithHealth[]>('/ceph/clusters'),
	createCephCluster: (data: CreateCephClusterRequest) =>
		request<CephCluster>('/ceph/clusters', { method: 'POST', body: JSON.stringify(data) }),
	getCephCluster: (id: string) => request<{ cluster: CephCluster; health: CephHealthReport | null }>(`/ceph/clusters/${id}`),
	deleteCephCluster: (id: string) =>
		request<void>(`/ceph/clusters/${id}`, { method: 'DELETE' }),
	getCephClusterHealth: (id: string) => request<CephHealthReport>(`/ceph/clusters/${id}/health`),
	listCephOSDs: (clusterId: string) => request<CephOSD[]>(`/ceph/clusters/${clusterId}/osds`),
	addCephOSD: (clusterId: string, data: { node_id: string; hostname: string; device_path: string; device_size?: number; device_type?: string }) =>
		request<CephOSD>(`/ceph/clusters/${clusterId}/osds`, { method: 'POST', body: JSON.stringify(data) }),
	removeCephOSD: (clusterId: string, osdId: string) =>
		request<void>(`/ceph/clusters/${clusterId}/osds/${osdId}`, { method: 'DELETE' }),
	listCephPools: (clusterId: string) => request<CephPool[]>(`/ceph/clusters/${clusterId}/pools`),
	createCephPool: (clusterId: string, data: { name: string; pg_num?: number; size?: number; application?: string }) =>
		request<CephPool>(`/ceph/clusters/${clusterId}/pools`, { method: 'POST', body: JSON.stringify(data) }),
	discoverDisks: (nodeId?: string) =>
		request<NodeDisks[]>(`/ceph/discover-disks${nodeId ? '?node_id=' + nodeId : ''}`),
	discoverAllDisks: () => request<NodeAllDisks[]>('/ceph/all-disks'),
};

// Types
export interface SystemStatus {
	status: string;
	role: string;
	node_count: number;
	multi_node: boolean;
	nats: string;
}

export interface Project {
	id: string;
	name: string;
	org_id: string;
	description: string;
	created_at: string;
	updated_at: string;
}

export interface App {
	id: string;
	project_id: string;
	name: string;
	deploy_type: string;
	image: string;
	git_repo: string;
	git_branch: string;
	domain: string;
	port: number;
	replicas: number;
	status: string;
	cpu_limit: number;
	memory_limit: number;
	health_check_path: string;
	health_check_interval: number;
	homepage_labels: Record<string, string>;
	extra_labels: Record<string, string>;
	placement_constraints: string[];
	placement_preferences: string[];
	update_strategy: string;
	update_parallelism: number;
	update_delay: string;
	update_failure_action: string;
	update_order: string;
	created_at: string;
	updated_at: string;
}

export interface CreateAppRequest {
	name: string;
	deploy_type: string;
	image?: string;
	git_repo?: string;
	git_branch?: string;
	dockerfile_path?: string;
	domain?: string;
	port?: number;
	replicas?: number;
}

export interface TaskInfo {
	id: string;
	node_id: string;
	status: string;
	message: string;
	image: string;
	slot: number;
	created_at: string;
}

export interface ServiceEvent {
	action: string;
	message: string;
	time: string;
}

export interface PortMapping {
	protocol: string;
	target_port: number;
	published_port: number;
	publish_mode: string;
}

export interface Deployment {
	id: string;
	app_id: string;
	status: string;
	commit_sha: string;
	image_digest: string;
	logs: string;
	started_at: string;
	finished_at: string | null;
}

export interface ManagedDatabase {
	id: string;
	project_id: string;
	name: string;
	db_type: string;
	version: string;
	status: string;
	created_at: string;
}

export interface SwarmNode {
	ID: string;
	Description: {
		Hostname: string;
		Platform: { Architecture: string; OS: string };
		Resources: { NanoCPUs: number; MemoryBytes: number };
	};
	Status: { State: string; Addr: string };
	Spec: { Role: string; Availability: string };
}

export interface TemplateListItem {
	id: string;
	name: string;
	description: string;
	category: string;
	icon: string;
	image: string;
	version: string;
	ports: string[];
	env: Record<string, string>;
	volumes: string[];
	domain: string;
	replicas: number;
	is_stack: boolean;
	source: 'builtin' | 'custom';
}

export interface TemplateDetail extends TemplateListItem {
	compose_content?: string;
}

export interface DeployTemplateRequest {
	project_id: string;
	domain?: string;
	env?: Record<string, string>;
	volumes?: string[];
}

export interface TemplateSource {
	id: string;
	org_id: string;
	name: string;
	url: string;
	type: string;
	last_synced_at: string | null;
	created_at: string;
}

export interface CustomTemplate {
	id: string;
	org_id: string;
	source_id: string;
	name: string;
	description: string;
	category: string;
	icon: string;
	image: string;
	version: string;
	ports: string;
	env: string;
	volumes: string;
	domain: string;
	replicas: number;
	is_stack: boolean;
	compose_content: string;
	created_at: string;
	updated_at: string;
}

export interface Secret {
	id: string;
	project_id: string;
	name: string;
	docker_secret_id: string;
	description: string;
	created_at: string;
	updated_at: string;
}

export interface AppSecret {
	app_id: string;
	secret_id: string;
	target: string;
	uid: string;
	gid: string;
	mode: number;
}

export interface Volume {
	id: string;
	project_id: string;
	name: string;
	driver: string;
	driver_opts: Record<string, string>;
	labels: Record<string, string>;
	mount_type: string;
	remote_host: string;
	remote_path: string;
	mount_options: string;
	scope: string;
	status: string;
	storage_host_id: string;
	local_path: string;
	ceph_pool: string;
	ceph_image: string;
	ceph_fs_name: string;
	created_at: string;
}

export interface AppVolume {
	app_id: string;
	volume_id: string;
	container_path: string;
	read_only: boolean;
}

export interface CreateVolumeRequest {
	name: string;
	mount_type?: string;
	remote_host?: string;
	remote_path?: string;
	mount_options?: string;
	username?: string;
	password?: string;
	storage_host_id?: string;
	local_path?: string;
	ceph_pool?: string;
	ceph_image?: string;
	ceph_fs_name?: string;
}

export interface BackupRun {
	id: string;
	config_id: string;
	status: string;
	size: number;
	target_path: string;
	started_at: string;
	finished_at: string | null;
}

export interface ServiceHealth {
	service_name: string;
	replicas: number;
	running: number;
	healthy: boolean;
}

export interface NodeMetrics {
	node_id: string;
	hostname: string;
	cpu_percent: number;
	mem_used: number;
	mem_total: number;
	disk_used: number;
	disk_total: number;
	containers: number;
	services: number;
	timestamp: number;
}

export interface NotificationChannel {
	id: string;
	org_id: string;
	name: string;
	type: string;
	config: Record<string, string>;
	created_at: string;
}

export interface ProxyRoute {
	id: string;
	project_id: string;
	name: string;
	domain: string;
	target_service: string;
	target_port: number;
	protocol: string;
	upstream_port: number | null;
	ssl_mode: string;
	custom_cert_id: string;
	middleware_config: Record<string, unknown>;
	enabled: boolean;
	created_at: string;
}

export interface CreateProxyRouteRequest {
	name: string;
	domain: string;
	target_service: string;
	target_port?: number;
	ssl_mode?: string;
	custom_cert_id?: string;
	middleware_config?: Record<string, unknown>;
	enabled?: boolean;
}

export interface CustomCertificate {
	id: string;
	project_id: string;
	domain: string;
	cert_pem: string;
	is_wildcard: boolean;
	provider: string;
	expires_at: string | null;
	auto_renew: boolean;
	dns_provider_id: string;
	last_renewed_at: string | null;
	renewal_error: string;
	created_at: string;
}

export interface Stack {
	id: string;
	project_id: string;
	name: string;
	compose_content: string;
	status: string;
	created_at: string;
	updated_at: string;
}

export interface UpdateStrategyRequest {
	strategy: string;
	parallelism: number;
	delay: string;
	failure_action: string;
	order: string;
}

export interface RegistryStatus {
	running: boolean;
	image_count?: number;
}

export interface RegistryImage {
	name: string;
	tags: string[];
}

export interface ConnectivityResult {
	port_80: boolean;
	port_443: boolean;
	message: string;
}

export interface AlertThreshold {
	id: string;
	org_id: string;
	metric: string;
	operator: string;
	value: number;
	cooldown_minutes: number;
	enabled: boolean;
	last_fired_at: string | null;
	created_at: string;
}

export interface BackupConfig {
	id: string;
	resource_id: string;
	schedule: string;
	s3_bucket: string;
	s3_prefix: string;
	backup_type: string;
	volume_id: string;
	created_at: string;
}

export interface StorageHost {
	id: string;
	name: string;
	node_id: string;
	address: string;
	type: string;
	default_export_path: string;
	default_mount_type: string;
	mount_options_default: string;
	capabilities: Record<string, boolean>;
	node_label: string;
	status: string;
	created_at: string;
	updated_at: string;
}

export interface CreateStorageHostRequest {
	name: string;
	node_id?: string;
	address: string;
	type?: string;
	default_export_path?: string;
	default_mount_type?: string;
	mount_options_default?: string;
	credentials?: string;
	capabilities?: Record<string, boolean>;
}

export interface StorageHostTestResult {
	host: string;
	address: string;
	type: string;
	ok: boolean;
	message: string;
}

export interface DNSProvider {
	id: string;
	org_id: string;
	name: string;
	type: string;
	is_default: boolean;
	created_at: string;
}

export interface DNSRecord {
	id: string;
	provider_id: string;
	app_id: string;
	domain: string;
	record_type: string;
	value: string;
	proxied: boolean;
	managed: boolean;
	external_id: string;
	created_at: string;
}

export interface NodeMetricsReport {
	node_id: string;
	hostname: string;
	timestamp: number;
	cpu_cores: number;
	cpu_per_core: number[];
	cpu_total_pct: number;
	load_avg_1: number;
	load_avg_5: number;
	load_avg_15: number;
	cpu_temp_celsius: number;
	mem_total: number;
	mem_used: number;
	mem_available: number;
	mem_buffers: number;
	mem_cached: number;
	swap_total: number;
	swap_used: number;
	disks: DiskMetrics[];
	interfaces: NetInterface[];
	os: string;
	kernel: string;
	uptime_seconds: number;
	process_count: number;
	pending_updates: number;
	containers_running: number;
	containers_stopped: number;
	images_count: number;
	volumes_count: number;
	gpus?: GPUMetrics[];
}

export interface DiskMetrics {
	mount_point: string;
	device: string;
	fs_type: string;
	total: number;
	used: number;
	read_bytes: number;
	write_bytes: number;
	iops: number;
	smart_ok?: boolean;
}

export interface NetInterface {
	name: string;
	rx_bytes: number;
	tx_bytes: number;
	rx_packets: number;
	tx_packets: number;
	rx_errors: number;
	tx_errors: number;
	link_speed_mbps: number;
}

export interface GPUMetrics {
	index: number;
	name: string;
	util_pct: number;
	mem_used: number;
	mem_total: number;
	temp_celsius: number;
}

export interface AppEnvVar {
	id: string;
	app_id: string;
	key: string;
	value: string;
	is_secret: boolean;
	source: string;
	created_at: string;
	updated_at: string;
}

export interface ServiceLink {
	id: string;
	source_app_id: string;
	target_app_id: string;
	target_database_id: string;
	env_prefix: string;
	created_at: string;
}

export interface PreviewDeployment {
	id: string;
	app_id: string;
	branch: string;
	pr_number: number | null;
	domain: string;
	status: string;
	service_name: string;
	created_at: string;
}

export interface OrgRole {
	id: string;
	org_id: string;
	user_id: string;
	role: string;
	created_at: string;
}

export interface MaintenanceTask {
	id: string;
	org_id: string;
	type: string;
	schedule: string;
	enabled: boolean;
	last_run_at: string | null;
	last_status: string;
	config: Record<string, unknown>;
	created_at: string;
}

export interface MaintenanceRun {
	id: string;
	task_id: string;
	status: string;
	details: string;
	started_at: string;
	finished_at: string | null;
}

export interface AuditLogEntry {
	id: string;
	user_id: string;
	org_id: string;
	action: string;
	resource: string;
	resource_id: string;
	details: string;
	created_at: string;
}

export interface GitSource {
	id: string;
	provider: string;
	created_at: string;
}

export interface GitRepository {
	full_name: string;
	name: string;
	clone_url: string;
	ssh_url: string;
	private: boolean;
	default_branch: string;
	description: string;
}

export interface GitBranch {
	name: string;
	protected: boolean;
	is_default: boolean;
}

export interface LogEntry {
	id: number;
	app_id: string;
	service_name: string;
	node_id: string;
	stream: string;
	message: string;
	level: string;
	timestamp: string;
}

export interface LogForwardConfig {
	id: string;
	org_id: string;
	name: string;
	type: string;
	enabled: boolean;
	created_at: string;
}

// Ceph Types

export interface CephCluster {
	id: string;
	name: string;
	fsid: string;
	status: string;
	bootstrap_node_id: string;
	mon_hosts: string[];
	public_network: string;
	cluster_network: string;
	replication_size: number;
	storage_host_id: string;
	created_at: string;
	updated_at: string;
}

export interface CephClusterWithHealth extends CephCluster {
	health?: CephHealthReport;
}

export interface CephHealthReport {
	fsid: string;
	node_id: string;
	timestamp: number;
	health: string;
	health_detail: string[];
	mon_count: number;
	mon_quorum: string[];
	osd_total: number;
	osd_up: number;
	osd_in: number;
	pg_count: number;
	pools: CephPoolStat[];
	total_bytes: number;
	used_bytes: number;
	avail_bytes: number;
}

export interface CephPoolStat {
	name: string;
	id: number;
	used_bytes: number;
	max_avail: number;
	objects: number;
}

export interface CephOSD {
	id: string;
	cluster_id: string;
	node_id: string;
	hostname: string;
	osd_id: number | null;
	device_path: string;
	device_size: number;
	device_type: string;
	status: string;
	created_at: string;
}

export interface CephPool {
	id: string;
	cluster_id: string;
	name: string;
	pool_id: number | null;
	pg_num: number;
	size: number;
	type: string;
	application: string;
	created_at: string;
}

export interface BlockDevice {
	name: string;
	path: string;
	size: number;
	type: string;
	mount_point?: string;
	fs_type?: string;
	model?: string;
	serial?: string;
	rotational: boolean;
	transport?: string;
	available: boolean;
}

export interface NodeDisks {
	node_id: string;
	block_devices: BlockDevice[];
}

export interface NodeAllDisks {
	node_id: string;
	hostname: string;
	block_devices: BlockDevice[];
}

export interface CreateCephClusterRequest {
	name: string;
	bootstrap_node_id: string;
	mon_nodes: { node_id: string; hostname: string; ip: string }[];
	osd_selections: { node_id: string; hostname: string; device_path: string; device_size?: number; device_type?: string }[];
	replication_size?: number;
	create_cephfs?: boolean;
	cephfs_name?: string;
	public_network?: string;
}
