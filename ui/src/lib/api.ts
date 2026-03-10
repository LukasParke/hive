import type {
	AlertThreshold,
	App,
	AppEnvVar,
	AppSecret,
	AppVolume,
	AuditLogEntry,
	BackupConfig,
	BackupRun,
	BlockDevice,
	BespokeAppClass,
	CreateAppRequest,
	CreateCephClusterRequest,
	CreateProxyRouteRequest,
	CreateStorageHostRequest,
	CreateVolumeRequest,
	CephCluster,
	CephClusterWithHealth,
	CephHealthReport,
	CephOSD,
	CephPool,
	ConnectivityResult,
	CustomCertificate,
	CustomTemplate,
	DeployTemplateRequest,
	Deployment,
	DiskMetrics,
	DNSProvider,
	DNSRecord,
	GitBranch,
	GitRepository,
	GitSource,
	LogEntry,
	LogForwardConfig,
	MaintenanceRun,
	MaintenanceTask,
	ManagedDatabase,
	NetInterface,
	NodeAllDisks,
	NodeDisks,
	NodeMetricsReport,
	NotificationChannel,
	OrgRole,
	PortMapping,
	PreviewDeployment,
	PrometheusClusterSummary,
	PrometheusNodeCurrent,
	PrometheusNodeHistory,
	PrometheusTimeSeriesPoint,
	Project,
	ProxyRoute,
	RegistryImage,
	RegistryStatus,
	Secret,
	ServiceEvent,
	ServiceHealth,
	ServiceLink,
	Stack,
	StorageHost,
	StorageHostTestResult,
	SwarmNode,
	SystemStatus,
	TaskInfo,
	TemplateDetail,
	TemplateListItem,
	TemplateSource,
	UpdateEvent,
	UpdatePolicy,
	UpdatesSummary,
	UpdateStrategyRequest,
	NodeUpdateStatus,
	ServiceUpdateStatus,
	SystemTask,
	Volume,
	DockerConfig,
	ScheduledJob,
	JobRun,
	VulnerabilityScan,
	Vulnerability,
	ResourceQuota,
	NodePowerConfig,
	UPSDevice,
	UPSStatusSnapshot,
	APIToken,
	WebhookEndpoint,
	WebhookDelivery,
	VPNServer,
	VPNPeer,
	OverlayNetwork,
	ClusterInfo,
	TemplateRatingEntry,
	SearchResult,
	FileEntry,
	ContainerInfo,
} from './types';

const API_BASE = '/api/v1';

function createApiClient(customFetch: typeof fetch = fetch) {
	async function request<T>(path: string, options?: RequestInit): Promise<T> {
		const res = await customFetch(`${API_BASE}${path}`, {
			credentials: 'include',
			headers: {
				'Content-Type': 'application/json',
				...options?.headers,
			},
			...options,
		});

		if (!res.ok) {
			const body = await res.json().catch(() => ({}));
			const message = body.error || body.message || res.statusText;
			throw new Error(message);
		}

		return res.json();
	}

	return {
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
	listAllApps: () => request<(App & { project_name: string })[]>('/apps'),
	createApp: (projectId: string, data: CreateAppRequest) =>
		request<App>(`/projects/${projectId}/apps`, { method: 'POST', body: JSON.stringify(data) }),
	getApp: (projectId: string, appId: string) => request<App>(`/projects/${projectId}/apps/${appId}`),
	getAppTasks: (projectId: string, appId: string) =>
		request<TaskInfo[]>(`/projects/${projectId}/apps/${appId}/tasks`),
	getAppEvents: (projectId: string, appId: string) =>
		request<ServiceEvent[]>(`/projects/${projectId}/apps/${appId}/events`),
	getAppPorts: (projectId: string, appId: string) =>
		request<PortMapping[]>(`/projects/${projectId}/apps/${appId}/ports`),
	updateApp: (projectId: string, appId: string, data: { domain?: string; port?: number; replicas?: number; image?: string }) =>
		request<App>(`/projects/${projectId}/apps/${appId}`, { method: 'PUT', body: JSON.stringify(data) }),
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
	listNodes: () => request<{ nodes: SwarmNode[]; metrics?: NodeMetricsReport[]; join_tokens?: { worker: string; manager: string }; advertise_addr?: string }>('/nodes'),
	getNode: (id: string) => request<SwarmNode>(`/nodes/${id}`),
	updateNodeAvailability: (nodeId: string, availability: string) =>
		request<{ updated: string }>(`/nodes/${nodeId}/availability`, { method: 'PUT', body: JSON.stringify({ availability }) }),
	updateNodeRole: (nodeId: string, role: string) =>
		request<{ updated: string }>(`/nodes/${nodeId}/role`, { method: 'PUT', body: JSON.stringify({ role }) }),
	nodeMaintenanceAction: (nodeId: string, action: string) =>
		request<{ status: string }>(`/nodes/${nodeId}/maintenance`, { method: 'POST', body: JSON.stringify({ action }) }),

	// Templates (marketplace - built-in + custom)
	listTemplates: () => request<TemplateListItem[]>('/templates'),
	getTemplate: (name: string) => request<TemplateDetail>(`/templates/${encodeURIComponent(name)}`),
	listBespokeApps: () => request<BespokeAppClass[]>('/bespoke/apps'),
	getBespokeApp: (slug: string) => request<BespokeAppClass>(`/bespoke/apps/${encodeURIComponent(slug)}`),
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

	// Metrics (Prometheus-backed)
	metricsCluster: () => request<PrometheusClusterSummary>('/metrics/cluster'),
	metricsNodes: () => request<PrometheusNodeCurrent[]>('/metrics/nodes'),
	metricsServices: () => request<ServiceHealth[]>('/metrics/services'),
	metricsNodeHistory: (hostname: string, range = '1h') =>
		request<PrometheusNodeHistory>(`/nodes/${encodeURIComponent(hostname)}/metrics/history?range=${range}`),

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
	listAllStacks: () => request<(Stack & { project_name: string })[]>('/stacks'),
	listStacks: (projectId: string) => request<Stack[]>(`/projects/${projectId}/stacks`),
	createStack: (projectId: string, data: { name: string; compose_content: string; domain?: string }) =>
		request<Stack>(`/projects/${projectId}/stacks`, { method: 'POST', body: JSON.stringify(data) }),
	getStack: (projectId: string, stackId: string) => request<Stack>(`/projects/${projectId}/stacks/${stackId}`),
	updateStack: (projectId: string, stackId: string, data: { compose_content?: string; domain?: string; name?: string }) =>
		request<Stack>(`/projects/${projectId}/stacks/${stackId}`, { method: 'PUT', body: JSON.stringify(data) }),
	deleteStack: (projectId: string, stackId: string) =>
		request<void>(`/projects/${projectId}/stacks/${stackId}`, { method: 'DELETE' }),
	getStackServices: (projectId: string, stackId: string) =>
		request<{ name: string; replicas: number; running: number; healthy: boolean; image: string }[]>(`/projects/${projectId}/stacks/${stackId}/services`),

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
		const res = await customFetch(`${API_BASE}/projects/${projectId}/apps/${appId}/env-vars/export`, {
			credentials: 'include',
		});
		if (!res.ok) {
			const body = await res.json().catch(() => ({}));
			throw new Error(body.error || body.message || res.statusText);
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
	updateDNSProvider: (id: string, data: { name: string; type: string; config: Record<string, string>; is_default?: boolean }) =>
		request<DNSProvider>(`/dns-providers/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
	deleteDNSProvider: (id: string) =>
		request<void>(`/dns-providers/${id}`, { method: 'DELETE' }),
	testDNSProvider: (id: string) =>
		request<{ status: string }>(`/dns-providers/${id}/test`, { method: 'POST' }),
	createDNSRecord: (providerId: string, data: { domain: string; record_type: string; value: string; proxied?: boolean }) =>
		request<DNSRecord>(`/dns-providers/${providerId}/records`, { method: 'POST', body: JSON.stringify(data) }),
	listDNSRecords: (providerId: string) => request<DNSRecord[]>(`/dns-providers/${providerId}/records`),
	deleteDNSRecord: (providerId: string, recordId: string) =>
		request<void>(`/dns-providers/${providerId}/records/${recordId}`, { method: 'DELETE' }),

	// Cluster Metrics (Prometheus-backed)
	getClusterMetrics: () => request<PrometheusClusterSummary>('/metrics/cluster'),
	getNodeMetricsList: () => request<PrometheusNodeCurrent[]>('/metrics/nodes'),
	getNodeMetricsHistory: (hostname: string, range: string) =>
		request<PrometheusNodeHistory>(`/nodes/${encodeURIComponent(hostname)}/metrics/history?range=${range}`),

	// Git Sources
	listGitSources: () => request<GitSource[]>('/git-sources'),
	createGitSource: (data: { provider: string; provider_name?: string; token: string }) =>
		request<{ id: string; provider: string }>('/git-sources', { method: 'POST', body: JSON.stringify(data) }),
	deleteGitSource: (sourceId: string) =>
		request<void>(`/git-sources/${sourceId}`, { method: 'DELETE' }),
	listGitRepos: (sourceId: string) => request<GitRepository[]>(`/git-sources/${sourceId}/repos`),
	listGitRepoBranches: (sourceId: string, repo: string) =>
		request<GitBranch[]>(`/git-sources/${sourceId}/repos/${encodeURIComponent(repo)}/branches`),
	registerWebhook: (sourceId: string, repo: string) =>
		request<{ webhook_id: string; status: string }>(`/git-sources/${sourceId}/repos/${encodeURIComponent(repo)}/webhook`, { method: 'POST' }),
	detectBuildType: (sourceId: string, repo: string, branch?: string) =>
		request<{ build_type: string }>(`/git-sources/${sourceId}/repos/${encodeURIComponent(repo)}/detect${branch ? '?branch=' + encodeURIComponent(branch) : ''}`),

	// GitHub App integration
	githubAppStatus: () =>
		request<{ configured: boolean; slug: string; installed: boolean; installation_id?: number; html_url?: string; app_id?: number }>('/integrations/github/status'),
	githubAppManifest: () =>
		request<{ manifest: string; redirect_url: string }>('/integrations/github/manifest', { method: 'POST' }),
	githubAppComplete: (code: string) =>
		request<{ id: string; app_id: number; slug: string; html_url: string }>('/integrations/github/complete', { method: 'POST', body: JSON.stringify({ code }) }),
	githubAppInstallation: (installationId: number) =>
		request<{ status: string }>('/integrations/github/installation', { method: 'POST', body: JSON.stringify({ installation_id: installationId }) }),
	githubAppDelete: () =>
		request<void>('/integrations/github', { method: 'DELETE' }),

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

	// Container exec & file browser
	createExec: (projectId: string, appId: string, data?: { command?: string; container_id?: string }) =>
		request<{ exec_id: string; container_id: string }>(`/projects/${projectId}/apps/${appId}/exec`, { method: 'POST', body: JSON.stringify(data || {}) }),
	listAppContainers: (projectId: string, appId: string) =>
		request<ContainerInfo[]>(`/projects/${projectId}/apps/${appId}/containers`),
	listFiles: (projectId: string, appId: string, data: { path: string; container_id?: string }) =>
		request<{ path: string; entries: FileEntry[] }>(`/projects/${projectId}/apps/${appId}/files/list`, { method: 'POST', body: JSON.stringify(data) }),
	viewFile: (projectId: string, appId: string, filePath: string, container?: string) =>
		request<{ path: string; content: string; size: number }>(`/projects/${projectId}/apps/${appId}/files/view?path=${encodeURIComponent(filePath)}${container ? '&container=' + container : ''}`),

	// Networks
	listNetworks: () => request<OverlayNetwork[]>('/networks'),
	createNetwork: (data: { name: string; encrypted?: boolean; attachable?: boolean; subnet?: string; gateway?: string }) =>
		request<{ id: string }>('/networks', { method: 'POST', body: JSON.stringify(data) }),
	inspectNetwork: (id: string) => request<any>(`/networks/${id}`),
	removeNetwork: (id: string) => request<void>(`/networks/${id}`, { method: 'DELETE' }),

	// Docker Configs
	listConfigs: (projectId: string) => request<DockerConfig[]>(`/projects/${projectId}/configs`),
	createConfig: (projectId: string, data: { name: string; data: string }) =>
		request<DockerConfig>(`/projects/${projectId}/configs`, { method: 'POST', body: JSON.stringify(data) }),
	deleteConfig: (projectId: string, configId: string) =>
		request<void>(`/projects/${projectId}/configs/${configId}`, { method: 'DELETE' }),

	// Scheduled Jobs
	listJobs: (projectId: string) => request<ScheduledJob[]>(`/projects/${projectId}/jobs`),
	createJob: (projectId: string, data: { name: string; image: string; command?: string; schedule: string; timezone?: string; env?: Record<string, string> }) =>
		request<ScheduledJob>(`/projects/${projectId}/jobs`, { method: 'POST', body: JSON.stringify(data) }),
	updateJob: (projectId: string, jobId: string, data: Partial<ScheduledJob>) =>
		request<void>(`/projects/${projectId}/jobs/${jobId}`, { method: 'PUT', body: JSON.stringify(data) }),
	deleteJob: (projectId: string, jobId: string) =>
		request<void>(`/projects/${projectId}/jobs/${jobId}`, { method: 'DELETE' }),
	triggerJob: (projectId: string, jobId: string) =>
		request<{ status: string }>(`/projects/${projectId}/jobs/${jobId}/trigger`, { method: 'POST' }),
	listJobRuns: (projectId: string, jobId: string) =>
		request<JobRun[]>(`/projects/${projectId}/jobs/${jobId}/runs`),

	// Resource Quotas
	getProjectQuotas: (projectId: string) => request<ResourceQuota>(`/projects/${projectId}/quotas`),
	setProjectQuotas: (projectId: string, data: { cpu_limit: number; memory_limit: number; storage_limit: number }) =>
		request<ResourceQuota>(`/projects/${projectId}/quotas`, { method: 'PUT', body: JSON.stringify(data) }),
	getProjectUsage: (projectId: string) => request<{ cpu_used: number; memory_used: number; storage_used: number }>(`/projects/${projectId}/usage`),

	// Vulnerability Scanning
	triggerScan: (projectId: string, appId: string) =>
		request<VulnerabilityScan>(`/projects/${projectId}/apps/${appId}/scan`, { method: 'POST' }),
	listScans: (projectId: string, appId: string) =>
		request<VulnerabilityScan[]>(`/projects/${projectId}/apps/${appId}/scans`),
	getScan: (projectId: string, appId: string, scanId: string) =>
		request<{ scan: VulnerabilityScan; vulnerabilities: Vulnerability[] }>(`/projects/${projectId}/apps/${appId}/scans/${scanId}`),
	securitySummary: () => request<{ critical: number; high: number; medium: number; low: number }>('/security/summary'),

	// Per-App Metrics
	appMetricsCurrent: (projectId: string, appId: string) =>
		request<any>(`/projects/${projectId}/apps/${appId}/metrics/current`),
	appMetricsHistory: (projectId: string, appId: string, range_param = '1h') =>
		request<any>(`/projects/${projectId}/apps/${appId}/metrics/history?range=${range_param}`),

	// Search
	search: (q: string) => request<SearchResult[]>(`/search?q=${encodeURIComponent(q)}`),

	// Node power & config
	nodePower: (nodeId: string, action: string) =>
		request<{ status: string }>(`/nodes/${nodeId}/power`, { method: 'POST', body: JSON.stringify({ action }) }),
	getNodeConfig: (nodeId: string) => request<NodePowerConfig>(`/nodes/${nodeId}/config`),
	setNodeConfig: (nodeId: string, data: Partial<NodePowerConfig>) =>
		request<NodePowerConfig>(`/nodes/${nodeId}/config`, { method: 'PUT', body: JSON.stringify(data) }),
	nodeHardware: (nodeId: string) => request<any>(`/nodes/${nodeId}/hardware`),

	// UPS
	listUPS: () => request<any[]>('/ups'),
	createUPS: (data: { name: string; nut_host: string; nut_port?: number; ups_name?: string; poll_interval_seconds?: number; shutdown_threshold?: number }) =>
		request<UPSDevice>('/ups', { method: 'POST', body: JSON.stringify(data) }),
	updateUPS: (id: string, data: Partial<UPSDevice>) =>
		request<void>(`/ups/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
	deleteUPS: (id: string) => request<void>(`/ups/${id}`, { method: 'DELETE' }),
	upsHistory: (id: string) => request<UPSStatusSnapshot[]>(`/ups/${id}/history`),

	// Dynamic DNS
	enableDDNS: (providerId: string, interval?: number) =>
		request<void>(`/dns-providers/${providerId}/ddns/enable`, { method: 'POST', body: JSON.stringify({ interval_minutes: interval || 5 }) }),
	disableDDNS: (providerId: string) =>
		request<void>(`/dns-providers/${providerId}/ddns/disable`, { method: 'POST' }),
	ddnsStatus: (providerId: string) =>
		request<{ enabled: boolean; interval_minutes: number; last_ip: string; last_update?: string }>(`/dns-providers/${providerId}/ddns/status`),

	// API Tokens
	listTokens: () => request<APIToken[]>('/tokens'),
	createToken: (data: { name: string; scopes?: string[]; expires_in_days?: number }) =>
		request<APIToken>('/tokens', { method: 'POST', body: JSON.stringify(data) }),
	deleteToken: (id: string) => request<void>(`/tokens/${id}`, { method: 'DELETE' }),

	// Webhooks
	listWebhooks: () => request<WebhookEndpoint[]>('/webhooks'),
	createWebhook: (data: { name: string; url: string; events?: string[] }) =>
		request<WebhookEndpoint>('/webhooks', { method: 'POST', body: JSON.stringify(data) }),
	updateWebhook: (id: string, data: Partial<WebhookEndpoint & { events: string[] }>) =>
		request<void>(`/webhooks/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
	deleteWebhook: (id: string) => request<void>(`/webhooks/${id}`, { method: 'DELETE' }),
	webhookDeliveries: (id: string) => request<WebhookDelivery[]>(`/webhooks/${id}/deliveries`),
	testWebhook: (id: string) => request<{ status: string }>(`/webhooks/${id}/test`, { method: 'POST' }),

	// VPN
	listVPNServers: () => request<VPNServer[]>('/vpn/servers'),
	createVPNServer: (data: { name: string; node_id?: string; listen_port?: number; address_range?: string; dns?: string; endpoint?: string }) =>
		request<VPNServer>('/vpn/servers', { method: 'POST', body: JSON.stringify(data) }),
	deleteVPNServer: (id: string) => request<void>(`/vpn/servers/${id}`, { method: 'DELETE' }),
	listVPNPeers: (serverId: string) => request<VPNPeer[]>(`/vpn/servers/${serverId}/peers`),
	createVPNPeer: (serverId: string, data: { name: string }) =>
		request<VPNPeer>(`/vpn/servers/${serverId}/peers`, { method: 'POST', body: JSON.stringify(data) }),
	deleteVPNPeer: (serverId: string, peerId: string) =>
		request<void>(`/vpn/servers/${serverId}/peers/${peerId}`, { method: 'DELETE' }),

	// Dashboard
	getDashboardLayout: () => request<any>('/dashboard/layout'),
	saveDashboardLayout: (layout: any) =>
		request<any>('/dashboard/layout', { method: 'PUT', body: JSON.stringify({ layout }) }),

	// Clusters
	listClusters: () => request<ClusterInfo[]>('/clusters'),
	createCluster: (data: { name: string; api_endpoint?: string; auth_token?: string }) =>
		request<ClusterInfo>('/clusters', { method: 'POST', body: JSON.stringify(data) }),
	deleteCluster: (id: string) => request<void>(`/clusters/${id}`, { method: 'DELETE' }),

	// Template Ratings
	rateTemplate: (name: string, data: { rating: number; review?: string }) =>
		request<TemplateRatingEntry>(`/templates/${encodeURIComponent(name)}/rate`, { method: 'POST', body: JSON.stringify(data) }),
	templateRatings: (name: string) => request<TemplateRatingEntry[]>(`/templates/${encodeURIComponent(name)}/ratings`),
	popularTemplates: () => request<{ name: string; count: number }[]>('/templates/popular'),
	topRatedTemplates: () => request<{ name: string; avg_rating: number; count: number }[]>('/templates/top-rated'),

	// Deployment diff
	deploymentDiff: (projectId: string, appId: string, id1: string, id2: string) =>
		request<any>(`/projects/${projectId}/apps/${appId}/deployments/${id1}/diff/${id2}`),
	rollbackToDeployment: (projectId: string, appId: string, deploymentId: string) =>
		request<{ status: string }>(`/projects/${projectId}/apps/${appId}/rollback/${deploymentId}`, { method: 'POST' }),

	// Generic helpers
	get: <T = any>(path: string) => request<T>(path),
	put: <T = any>(path: string, data: any) => request<T>(path, { method: 'PUT', body: JSON.stringify(data) }),
	post: <T = any>(path: string, data: any) => request<T>(path, { method: 'POST', body: JSON.stringify(data) }),

	// Networking
	getNetworkingSettings: () => request<any>('/networking'),
	updateNetworkingSettings: (data: { ingress_mode?: string; tunnel_token?: string }) =>
		request<any>('/networking', { method: 'PUT', body: JSON.stringify(data) }),
	testTunnelConnection: () => request<any>('/networking/test-tunnel', { method: 'POST' }),

	// Updates
	updatesSummary: () => request<UpdatesSummary>('/updates/summary'),
	updatesNodes: () => request<NodeUpdateStatus[]>('/updates/nodes'),
	updatesNodeDetail: (nodeId: string) => request<NodeUpdateStatus>(`/updates/nodes/${nodeId}`),
	checkNodeUpdates: (nodeId: string) =>
		request<any>(`/updates/nodes/${nodeId}/check`, { method: 'POST' }),
	applyNodeUpdates: (nodeId: string, opts?: { security_only?: boolean; action?: string }) =>
		request<any>(`/updates/nodes/${nodeId}/apply`, { method: 'POST', body: JSON.stringify(opts || {}) }),
	checkAllNodeUpdates: () =>
		request<any>('/updates/nodes/check-all', { method: 'POST' }),
	applyAllNodeUpdates: (opts?: { security_only?: boolean }) =>
		request<any>('/updates/nodes/apply-all', { method: 'POST', body: JSON.stringify(opts || {}) }),
	updatesServices: () => request<ServiceUpdateStatus[]>('/updates/services'),
	applyServiceUpdate: (serviceName: string) =>
		request<any>(`/updates/services/${serviceName}/apply`, { method: 'POST' }),
	applyAllServiceUpdates: () =>
		request<any>('/updates/services/apply-all', { method: 'POST' }),
	updatesHistory: (opts?: { type?: string; limit?: number }) => {
		const params = new URLSearchParams();
		if (opts?.type) params.set('type', opts.type);
		if (opts?.limit) params.set('limit', String(opts.limit));
		const qs = params.toString();
		return request<UpdateEvent[]>(`/updates/history${qs ? '?' + qs : ''}`);
	},
	listUpdatePolicies: () => request<UpdatePolicy[]>('/updates/policies'),
	createUpdatePolicy: (data: Partial<UpdatePolicy>) =>
		request<UpdatePolicy>('/updates/policies', { method: 'POST', body: JSON.stringify(data) }),
	updateUpdatePolicy: (id: string, data: Partial<UpdatePolicy>) =>
		request<UpdatePolicy>(`/updates/policies/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
	deleteUpdatePolicy: (id: string) =>
		request<void>(`/updates/policies/${id}`, { method: 'DELETE' }),

	// System Tasks
	listSystemTasks: () => request<SystemTask[]>('/system-tasks'),
	triggerSystemTask: (taskId: string) =>
		request<{ status: string; task_id: string }>(`/system-tasks/${taskId}/trigger`, { method: 'POST' }),
	updateSystemTask: (taskId: string, data: { enabled?: boolean }) =>
		request<SystemTask>(`/system-tasks/${taskId}`, { method: 'PUT', body: JSON.stringify(data) }),
	};
}

export type ApiClient = ReturnType<typeof createApiClient>;
export const createApi = createApiClient;
export const api = createApiClient();

export * from './types';
