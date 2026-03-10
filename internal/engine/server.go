package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/internal/tunnel"
	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

type Server struct {
	nc          *nats.Conn
	sc          *swarm.Client
	store       *store.Store
	cfg         *config.Config
	log         *zap.SugaredLogger
	port        int
	secret      string
	router      chi.Router
	cfManager   *tunnel.CloudflaredManager
	updateCache *UpdateCache
	taskManager *SystemTaskManager
}

func NewServer(nc *nats.Conn, sc *swarm.Client, db *store.Store, cfg *config.Config, log *zap.SugaredLogger, port int, secret string) *Server {
	s := &Server{
		nc:     nc,
		sc:     sc,
		store:  db,
		cfg:    cfg,
		log:    log,
		port:   port,
		secret: secret,
	}
	if sc != nil {
		s.cfManager = tunnel.NewCloudflaredManager(sc, cfg, log)
	}
	s.updateCache = NewUpdateCache(nc, db, log)
	s.router = s.buildRouter()
	return s
}

func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	s.log.Infof("hive-engine API listening on %s", addr)
	return http.ListenAndServe(addr, s.router)
}

func (s *Server) StartUpdateCache(ctx context.Context) {
	s.updateCache.Start(ctx)
}

func (s *Server) GetUpdateCache() *UpdateCache {
	return s.updateCache
}

func (s *Server) SetTaskManager(mgr *SystemTaskManager) {
	s.taskManager = mgr
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.secret == "" {
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get("Authorization")
		if token != "Bearer "+s.secret {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "unauthorized", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-User-Id, X-Org-Id, X-Role")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(s.corsMiddleware)

	r.Get("/engine/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Legacy internal engine API (SvelteKit→engine, uses shared secret)
	r.Route("/engine/v1", func(r chi.Router) {
		r.Use(s.authMiddleware)

		r.Post("/services", s.createService)
		r.Put("/services/{name}", s.updateService)
		r.Delete("/services/{name}", s.removeService)
		r.Get("/services/{name}/logs", s.serviceLogs)
		r.Get("/services/{name}/tasks", s.serviceTasks)
		r.Get("/services/{name}/events", s.serviceEvents)
		r.Get("/services/{name}/ports", s.servicePorts)
		r.Post("/services/{name}/rollback", s.serviceRollback)

		r.Post("/build", s.triggerBuild)
		r.Post("/deploy", s.triggerDeploy)

		r.Post("/backup", s.triggerBackup)
		r.Post("/restore", s.triggerRestore)

		r.Get("/nodes", s.listNodes)
		r.Put("/nodes/{id}/labels", s.updateNodeLabels)
		r.Put("/nodes/{id}/availability", s.updateNodeAvailability)
		r.Put("/nodes/{id}/role", s.updateNodeRole)
		r.Delete("/nodes/{id}", s.removeNode)
		r.Post("/nodes/{id}/maintenance", s.nodeMaintenanceAction)

		r.Get("/swarm/info", s.swarmInfo)
		r.Get("/services/health", s.serviceHealth)

		r.Post("/ceph/deploy", s.cephDeploy)
		r.Get("/ceph/health", s.cephHealth)
		r.Post("/ceph/cmd", s.cephCommand)

		r.Post("/secrets", s.createDockerSecret)
		r.Delete("/secrets/{id}", s.deleteDockerSecret)

		r.Get("/volumes", s.listDockerVolumes)
		r.Post("/volumes", s.createDockerVolume)
		r.Delete("/volumes/{name}", s.deleteDockerVolume)

		r.Get("/swarm/join-tokens", s.swarmJoinTokens)

		r.Post("/services/{name}/scale", s.directScale)
		r.Put("/services/{name}/labels", s.updateServiceLabels)

		r.Post("/db/provision", s.provisionDatabase)
		r.Get("/discover-disks", s.discoverDisks)

		r.Get("/registry/status", s.registryStatus)
		r.Get("/registry/images", s.registryImages)
		r.Get("/routes/active", s.activeRoutes)
		r.Post("/connectivity/check", s.connectivityCheck)
		r.Post("/maintenance/trigger", s.triggerMaintenance)
	})

	// Public API (browser-facing, uses session cookie auth)
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.sessionAuthMiddleware)

		// System
		r.Get("/system/status", s.apiSystemStatus)
		r.Get("/system/logs", s.apiSystemLogs)
		r.Get("/system/connectivity", s.apiConnectivityCheck)

		// Projects
		r.Get("/projects", s.apiListProjects)
		r.Post("/projects", s.apiCreateProject)
		r.Get("/projects/{projectId}", s.apiGetProject)
		r.Delete("/projects/{projectId}", s.apiDeleteProject)

		// Apps (global)
		r.Get("/apps", s.apiListAllApps)

		// Stacks (global)
		r.Get("/stacks", s.apiListAllStacks)

		// Apps (nested under projects)
		r.Get("/projects/{projectId}/apps", s.apiListApps)
		r.Post("/projects/{projectId}/apps", s.apiCreateApp)
		r.Route("/projects/{projectId}/apps/{appId}", func(r chi.Router) {
			r.Get("/", s.apiGetApp)
			r.Put("/", s.apiUpdateApp)
			r.Delete("/", s.apiDeleteApp)
			r.Post("/deploy", s.apiDeployApp)
			r.Post("/restart", s.apiRestartApp)
			r.Post("/stop", s.apiStopApp)
			r.Post("/start", s.apiStartApp)
			r.Put("/scale", s.apiScaleApp)
			r.Post("/rollback", s.apiRollbackApp)
			r.Get("/tasks", s.apiAppTasks)
			r.Get("/events", s.apiAppEvents)
			r.Get("/ports", s.apiAppPorts)
			r.Get("/deployments", s.apiListDeployments)
			r.Put("/resources", s.apiUpdateAppResources)
			r.Put("/healthcheck", s.apiUpdateAppHealthCheck)
			r.Put("/placement", s.apiUpdateAppPlacement)
			r.Put("/update-strategy", s.apiUpdateAppStrategy)
			r.Put("/labels", s.apiUpdateAppLabels)

			// Env vars
			r.Get("/env-vars", s.apiListAppEnvVars)
			r.Post("/env-vars", s.apiCreateAppEnvVar)
			r.Put("/env-vars", s.apiBulkUpsertAppEnvVars)
			r.Post("/env-vars/import", s.apiImportEnvVars)
			r.Get("/env-vars/export", s.apiExportEnvVars)
			r.Get("/env-vars/{key}", s.apiGetAppEnvVarByKey)
			r.Put("/env-vars/{key}", s.apiUpdateAppEnvVar)
			r.Delete("/env-vars/{key}", s.apiDeleteAppEnvVarByKey)

			// Service links
			r.Get("/links", s.apiListServiceLinks)
			r.Post("/links", s.apiCreateServiceLink)
			r.Delete("/links/{linkId}", s.apiDeleteServiceLink)

			// Previews
			r.Get("/previews", s.apiListPreviewDeployments)
			r.Post("/previews", s.apiCreatePreviewDeployment)
			r.Delete("/previews/{previewId}", s.apiDeletePreviewDeployment)

			// Log queries
			r.Get("/logs/query", s.apiQueryLogEntries)

			// Export as template
			r.Post("/export-template", s.apiExportAppAsTemplate)

			// Container exec
			r.Post("/exec", s.apiCreateExec)
			r.Get("/containers", s.apiListContainersForApp)

			// File browser
			r.Post("/files/list", s.apiListFiles)
			r.Get("/files/download", s.apiDownloadFile)
			r.Post("/files/upload", s.apiUploadFile)
			r.Get("/files/view", s.apiViewFile)

			// Per-app metrics
			r.Get("/metrics/current", s.apiAppMetricsCurrent)
			r.Get("/metrics/history", s.apiAppMetricsHistory)

			// Vulnerability scanning
			r.Post("/scan", s.apiTriggerScan)
			r.Get("/scans", s.apiListScans)
			r.Get("/scans/{scanId}", s.apiGetScan)
		})

		// Secrets (nested under projects)
		r.Get("/projects/{projectId}/secrets", s.apiListSecrets)
		r.Post("/projects/{projectId}/secrets", s.apiCreateSecret)
		r.Delete("/projects/{projectId}/secrets/{secretId}", s.apiDeleteSecret)
		r.Post("/projects/{projectId}/secrets/{secretId}/attach/{appId}", s.apiAttachSecret)
		r.Delete("/projects/{projectId}/secrets/{secretId}/detach/{appId}", s.apiDetachSecret)

		// Volumes (nested under projects)
		r.Get("/projects/{projectId}/volumes", s.apiListVolumes)
		r.Post("/projects/{projectId}/volumes", s.apiCreateVolume)
		r.Get("/projects/{projectId}/volumes/{volumeId}", s.apiGetVolume)
		r.Delete("/projects/{projectId}/volumes/{volumeId}", s.apiDeleteVolume)
		r.Post("/projects/{projectId}/volumes/{volumeId}/attach/{appId}", s.apiAttachVolume)
		r.Delete("/projects/{projectId}/volumes/{volumeId}/detach/{appId}", s.apiDetachVolume)

		// Databases (nested under projects)
		r.Get("/projects/{projectId}/databases", s.apiListManagedDatabases)
		r.Post("/projects/{projectId}/databases", s.apiCreateManagedDatabase)
		r.Get("/projects/{projectId}/databases/{databaseId}", s.apiGetManagedDatabase)
		r.Delete("/projects/{projectId}/databases/{databaseId}", s.apiDeleteManagedDatabase)

		// Stacks (nested under projects)
		r.Get("/projects/{projectId}/stacks", s.apiListStacks)
		r.Post("/projects/{projectId}/stacks", s.apiCreateStack)
		r.Get("/projects/{projectId}/stacks/{stackId}", s.apiGetStack)
		r.Put("/projects/{projectId}/stacks/{stackId}", s.apiUpdateStack)
		r.Delete("/projects/{projectId}/stacks/{stackId}", s.apiDeleteStack)
		r.Get("/projects/{projectId}/stacks/{stackId}/services", s.apiStackServices)

		// Proxy routes (nested under projects)
		r.Get("/projects/{projectId}/routes", s.apiListProxyRoutes)
		r.Post("/projects/{projectId}/routes", s.apiCreateProxyRoute)
		r.Get("/projects/{projectId}/routes/{routeId}", s.apiGetProxyRoute)
		r.Put("/projects/{projectId}/routes/{routeId}", s.apiUpdateProxyRoute)
		r.Delete("/projects/{projectId}/routes/{routeId}", s.apiDeleteProxyRoute)

		// Certificates (nested under projects)
		r.Get("/projects/{projectId}/certificates", s.apiListCustomCertificates)
		r.Post("/projects/{projectId}/certificates", s.apiCreateCustomCertificate)
		r.Delete("/projects/{projectId}/certificates/{certId}", s.apiDeleteCustomCertificate)

		// Active routes (cross-project)
		r.Get("/routes/active", s.activeRoutes)

		// Nodes
		r.Get("/nodes", s.apiListNodes)
		r.Get("/nodes/{nodeId}/labels", s.apiGetNodeLabels)
		r.Put("/nodes/{nodeId}/labels", s.apiUpdateNodeLabelsV1)
		r.Put("/nodes/{nodeId}/availability", s.apiUpdateNodeAvailabilityV1)
		r.Put("/nodes/{nodeId}/role", s.apiUpdateNodeRoleV1)
		r.Post("/nodes/{nodeId}/maintenance", s.apiNodeMaintenance)
		r.Get("/nodes/{nodeId}/metrics/history", s.apiNodeMetricsHistory)
		r.Get("/nodes/{nodeId}/containers", s.apiNodeContainers)

		// Metrics
		r.Get("/metrics/cluster", s.apiMetricsCluster)
		r.Get("/metrics/nodes", s.apiMetricsNodes)
		r.Get("/metrics/services", s.apiMetricsServices)

		// Templates
		r.Get("/templates", s.apiListBuiltinTemplates)
		r.Get("/templates/{name}", s.apiGetBuiltinTemplate)
		r.Post("/templates/{name}/deploy", s.apiDeployTemplate)
		r.Get("/bespoke/apps", s.apiListBespokeApps)
		r.Get("/bespoke/apps/{slug}", s.apiGetBespokeApp)
		r.Get("/custom-templates", s.apiListCustomTemplates)
		r.Post("/custom-templates", s.apiCreateCustomTemplate)
		r.Get("/custom-templates/{id}", s.apiGetCustomTemplate)
		r.Put("/custom-templates/{id}", s.apiUpdateCustomTemplate)
		r.Delete("/custom-templates/{id}", s.apiDeleteCustomTemplate)

		// Template sources
		r.Get("/template-sources", s.apiListTemplateSources)
		r.Post("/template-sources", s.apiCreateTemplateSource)
		r.Delete("/template-sources/{sourceId}", s.apiDeleteTemplateSource)
		r.Post("/template-sources/{sourceId}/sync", s.apiSyncTemplateSource)

		// Notifications
		r.Get("/notifications", s.apiListNotificationChannels)
		r.Post("/notifications", s.apiCreateNotificationChannel)
		r.Get("/notifications/{channelId}", s.apiGetNotificationChannel)
		r.Delete("/notifications/{channelId}", s.apiDeleteNotificationChannel)
		r.Post("/notifications/{channelId}/test", s.apiTestNotificationChannel)

		// Alerts
		r.Get("/alerts", s.apiListAlertThresholds)
		r.Post("/alerts", s.apiCreateAlertThreshold)
		r.Delete("/alerts/{alertId}", s.apiDeleteAlertThreshold)

		// DNS
		r.Get("/dns-providers", s.apiListDNSProviders)
		r.Post("/dns-providers", s.apiCreateDNSProvider)
		r.Get("/dns-providers/{providerId}", s.apiGetDNSProvider)
		r.Put("/dns-providers/{providerId}", s.apiUpdateDNSProvider)
		r.Delete("/dns-providers/{providerId}", s.apiDeleteDNSProvider)
		r.Post("/dns-providers/{providerId}/test", s.apiTestDNSProvider)
		r.Get("/dns-providers/{providerId}/records", s.apiListDNSRecords)
		r.Post("/dns-providers/{providerId}/records", s.apiCreateDNSRecord)
		r.Delete("/dns-providers/{providerId}/records/{recordId}", s.apiDeleteDNSRecord)

		// Git sources
		r.Get("/git-sources", s.apiListGitSources)
		r.Post("/git-sources", s.apiCreateGitSource)
		r.Get("/git-sources/{sourceId}", s.apiGetGitSource)
		r.Delete("/git-sources/{sourceId}", s.apiDeleteGitSource)
		r.Get("/git-sources/{sourceId}/repos", s.apiListGitRepos)
		r.Get("/git-sources/{sourceId}/repos/{repo}/branches", s.apiListGitBranches)
		r.Post("/git-sources/{sourceId}/repos/{repo}/webhook", s.apiRegisterGitWebhook)
		r.Get("/git-sources/{sourceId}/repos/{repo}/detect", s.apiDetectBuildType)

		// Storage hosts
		r.Get("/storage-hosts", s.apiListStorageHosts)
		r.Post("/storage-hosts", s.apiCreateStorageHost)
		r.Get("/storage-hosts/{hostId}", s.apiGetStorageHost)
		r.Put("/storage-hosts/{hostId}", s.apiUpdateStorageHost)
		r.Delete("/storage-hosts/{hostId}", s.apiDeleteStorageHost)
		r.Post("/storage-hosts/{hostId}/test", s.apiTestStorageHost)

		// Backups
		r.Get("/backups", s.apiListBackupConfigsByOrg)
		r.Post("/backups", s.apiCreateBackupConfig)
		r.Post("/backups/{configId}/trigger", s.apiTriggerBackup)
		r.Get("/backups/{configId}/runs", s.apiListBackupRuns)
		r.Post("/backups/{configId}/restore/{runId}", s.apiTriggerRestore)

		// Maintenance
		r.Get("/maintenance", s.apiListMaintenanceTasks)
		r.Post("/maintenance", s.apiCreateMaintenanceTask)
		r.Get("/maintenance/{taskId}", s.apiGetMaintenanceTask)
		r.Put("/maintenance/{taskId}", s.apiUpdateMaintenanceTask)
		r.Delete("/maintenance/{taskId}", s.apiDeleteMaintenanceTask)
		r.Post("/maintenance/{taskId}/trigger", s.apiCreateMaintenanceRun)
		r.Get("/maintenance/{taskId}/runs", s.apiListMaintenanceRuns)

		// Updates
		r.Get("/updates/summary", s.apiUpdatesSummary)
		r.Get("/updates/nodes", s.apiUpdatesNodes)
		r.Get("/updates/nodes/{nodeId}", s.apiUpdatesNodeDetail)
		r.Post("/updates/nodes/{nodeId}/check", s.apiCheckNodeUpdates)
		r.Post("/updates/nodes/{nodeId}/apply", s.apiApplyNodeUpdates)
		r.Post("/updates/nodes/check-all", s.apiCheckAllNodeUpdates)
		r.Post("/updates/nodes/apply-all", s.apiApplyAllNodeUpdates)
		r.Get("/updates/services", s.apiUpdatesServices)
		r.Post("/updates/services/{serviceName}/apply", s.apiApplyServiceUpdate)
		r.Post("/updates/services/apply-all", s.apiApplyAllServiceUpdates)
		r.Get("/updates/history", s.apiUpdatesHistory)
		r.Get("/updates/policies", s.apiListUpdatePolicies)
		r.Post("/updates/policies", s.apiCreateUpdatePolicy)
		r.Put("/updates/policies/{policyId}", s.apiUpdateUpdatePolicy)
		r.Delete("/updates/policies/{policyId}", s.apiDeleteUpdatePolicy)

		// Members
		r.Get("/members", s.apiListOrgRoles)
		r.Post("/members", s.apiCreateOrgRole)
		r.Put("/members/{userId}", s.apiUpdateOrgRole)
		r.Put("/members/{userId}/role", s.apiUpdateOrgRole)
		r.Delete("/members/{userId}", s.apiDeleteOrgRole)

		// Audit
		r.Get("/audit", s.apiListAuditLogs)
		r.Get("/audit/stats", s.apiGetAuditLogStats)

		// Log forwards
		r.Get("/log-forwards", s.apiListLogForwardConfigs)
		r.Post("/log-forwards", s.apiCreateLogForwardConfig)
		r.Delete("/log-forwards/{id}", s.apiDeleteLogForwardConfig)

		// Networking
		r.Get("/networking", s.apiGetNetworkingSettings)
		r.Put("/networking", s.apiUpdateNetworkingSettings)
		r.Post("/networking/test-tunnel", s.apiTestTunnelConnection)

		// Registry
		r.Get("/registry/status", s.registryStatus)
		r.Get("/registry/images", s.registryImages)
		r.Delete("/registry/images/{name}/{tag}", s.apiDeleteRegistryImage)

		// Networks
		r.Get("/networks", s.apiListNetworks)
		r.Post("/networks", s.apiCreateNetwork)
		r.Get("/networks/{networkId}", s.apiInspectNetwork)
		r.Delete("/networks/{networkId}", s.apiRemoveNetwork)
		r.Post("/networks/{networkId}/connect", s.apiConnectServiceToNetwork)
		r.Post("/networks/{networkId}/disconnect", s.apiDisconnectServiceFromNetwork)

		// Docker Configs (project-scoped)
		r.Get("/projects/{projectId}/configs", s.apiListConfigs)
		r.Post("/projects/{projectId}/configs", s.apiCreateConfig)
		r.Delete("/projects/{projectId}/configs/{configId}", s.apiDeleteConfig)
		r.Post("/projects/{projectId}/configs/{configId}/attach/{appId}", s.apiAttachConfig)
		r.Delete("/projects/{projectId}/configs/{configId}/detach/{appId}", s.apiDetachConfig)

		// Scheduled Jobs (project-scoped)
		r.Get("/projects/{projectId}/jobs", s.apiListJobs)
		r.Post("/projects/{projectId}/jobs", s.apiCreateJob)
		r.Put("/projects/{projectId}/jobs/{jobId}", s.apiUpdateJob)
		r.Delete("/projects/{projectId}/jobs/{jobId}", s.apiDeleteJob)
		r.Post("/projects/{projectId}/jobs/{jobId}/trigger", s.apiTriggerJob)
		r.Get("/projects/{projectId}/jobs/{jobId}/runs", s.apiListJobRuns)
		r.Get("/projects/{projectId}/jobs/{jobId}/runs/{runId}/logs", s.apiJobRunLogs)

		// Resource Quotas (project-scoped)
		r.Get("/projects/{projectId}/quotas", s.apiGetProjectQuotas)
		r.Put("/projects/{projectId}/quotas", s.apiSetProjectQuotas)
		r.Get("/projects/{projectId}/usage", s.apiGetProjectUsage)

		// Deployment diff
		r.Get("/projects/{projectId}/apps/{appId}/deployments/{deployId1}/diff/{deployId2}", s.apiDeploymentDiff)
		r.Post("/projects/{projectId}/apps/{appId}/rollback/{deploymentId}", s.apiRollbackToDeployment)

		// Security / vulnerability scanning
		r.Get("/security/summary", s.apiSecuritySummary)

		// Search
		r.Get("/search", s.apiSearch)

		// Node power management
		r.Post("/nodes/{nodeId}/power", s.apiNodePower)
		r.Get("/nodes/{nodeId}/config", s.apiGetNodeConfig)
		r.Put("/nodes/{nodeId}/config", s.apiSetNodeConfig)
		r.Get("/nodes/{nodeId}/hardware", s.apiNodeHardware)

		// UPS Monitoring
		r.Get("/ups", s.apiListUPS)
		r.Post("/ups", s.apiCreateUPS)
		r.Put("/ups/{upsId}", s.apiUpdateUPS)
		r.Delete("/ups/{upsId}", s.apiDeleteUPS)
		r.Get("/ups/{upsId}/history", s.apiUPSHistory)

		// Dynamic DNS
		r.Post("/dns-providers/{providerId}/ddns/enable", s.apiEnableDDNS)
		r.Post("/dns-providers/{providerId}/ddns/disable", s.apiDisableDDNS)
		r.Get("/dns-providers/{providerId}/ddns/status", s.apiDDNSStatus)

		// API Tokens
		r.Get("/tokens", s.apiListTokens)
		r.Post("/tokens", s.apiCreateToken)
		r.Delete("/tokens/{tokenId}", s.apiDeleteToken)

		// Webhooks
		r.Get("/webhooks", s.apiListWebhooks)
		r.Post("/webhooks", s.apiCreateWebhook)
		r.Put("/webhooks/{webhookId}", s.apiUpdateWebhook)
		r.Delete("/webhooks/{webhookId}", s.apiDeleteWebhook)
		r.Get("/webhooks/{webhookId}/deliveries", s.apiListWebhookDeliveries)
		r.Post("/webhooks/{webhookId}/test", s.apiTestWebhook)

		// VPN
		r.Get("/vpn/servers", s.apiListVPNServers)
		r.Post("/vpn/servers", s.apiCreateVPNServer)
		r.Delete("/vpn/servers/{serverId}", s.apiDeleteVPNServer)
		r.Get("/vpn/servers/{serverId}/peers", s.apiListVPNPeers)
		r.Post("/vpn/servers/{serverId}/peers", s.apiCreateVPNPeer)
		r.Delete("/vpn/servers/{serverId}/peers/{peerId}", s.apiDeleteVPNPeer)
		r.Get("/vpn/servers/{serverId}/peers/{peerId}/config", s.apiVPNPeerConfig)
		r.Get("/vpn/servers/{serverId}/peers/{peerId}/qr", s.apiVPNPeerQR)

		// System Tasks
		r.Get("/system-tasks", s.apiListSystemTasks)
		r.Post("/system-tasks/{taskId}/trigger", s.apiTriggerSystemTask)
		r.Put("/system-tasks/{taskId}", s.apiUpdateSystemTask)

		// Dashboard layout
		r.Get("/dashboard/layout", s.apiGetDashboardLayout)
		r.Put("/dashboard/layout", s.apiSaveDashboardLayout)

		// Clusters
		r.Get("/clusters", s.apiListClusters)
		r.Post("/clusters", s.apiCreateCluster)
		r.Delete("/clusters/{clusterId}", s.apiDeleteCluster)
		r.Post("/clusters/{clusterId}/test", s.apiTestCluster)

		// Template ratings
		r.Post("/templates/{name}/rate", s.apiRateTemplate)
		r.Get("/templates/{name}/ratings", s.apiListTemplateRatings)
		r.Get("/templates/popular", s.apiPopularTemplates)
		r.Get("/templates/top-rated", s.apiTopRatedTemplates)

		// GitHub App integration
		r.Get("/integrations/github/status", s.apiGitHubAppStatus)
		r.Post("/integrations/github/manifest", s.apiGitHubAppManifest)
		r.Post("/integrations/github/complete", s.apiGitHubAppComplete)
		r.Post("/integrations/github/installation", s.apiGitHubAppInstallation)
		r.Delete("/integrations/github", s.apiGitHubAppDelete)

		// Ceph
		r.Get("/ceph/clusters", s.apiListCephClusters)
		r.Post("/ceph/clusters", s.apiCreateCephCluster)
		r.Get("/ceph/clusters/{clusterId}", s.apiGetCephCluster)
		r.Delete("/ceph/clusters/{clusterId}", s.apiDeleteCephCluster)
		r.Get("/ceph/clusters/{clusterId}/health", s.apiCephClusterHealth)
		r.Get("/ceph/clusters/{clusterId}/osds", s.apiListCephOSDs)
		r.Post("/ceph/clusters/{clusterId}/osds", s.apiCreateCephOSD)
		r.Delete("/ceph/clusters/{clusterId}/osds/{osdId}", s.apiDeleteCephOSD)
		r.Get("/ceph/clusters/{clusterId}/pools", s.apiListCephPools)
		r.Post("/ceph/clusters/{clusterId}/pools", s.apiCreateCephPool)
		r.Delete("/ceph/clusters/{clusterId}/pools/{poolId}", s.apiDeleteCephPool)
		r.Get("/ceph/discover-disks", s.discoverDisks)
		r.Get("/ceph/all-disks", s.apiAllDisks)
	})

	// Public webhook endpoints (no auth)
	r.Post("/api/v1/webhooks/github", s.apiGitHubWebhook)

	// WebSocket endpoints (session cookie auth)
	r.Route("/ws", func(r chi.Router) {
		r.Use(s.sessionAuthMiddleware)
		r.Get("/metrics", s.wsMetrics)
		r.Get("/logs/{appId}", s.wsLogs)
		r.Get("/events", s.wsEvents)
		r.Get("/build/{deploymentId}", s.wsBuildLogs)
		r.Get("/updates", s.wsUpdates)
		r.Get("/exec/{execId}", s.wsExec)
	})

	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
