package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/agentclient"
	"github.com/luke/hive/control-plane/internal/api/agents"
	"github.com/luke/hive/control-plane/internal/api/apikeys"
	"github.com/luke/hive/control-plane/internal/api/applications"
	authapi "github.com/luke/hive/control-plane/internal/api/auth"
	"github.com/luke/hive/control-plane/internal/api/backups"
	"github.com/luke/hive/control-plane/internal/api/builds"
	"github.com/luke/hive/control-plane/internal/api/databases"
	"github.com/luke/hive/control-plane/internal/api/deployments"
	"github.com/luke/hive/control-plane/internal/api/domains"
	"github.com/luke/hive/control-plane/internal/api/environments"
	"github.com/luke/hive/control-plane/internal/api/events"
	"github.com/luke/hive/control-plane/internal/api/gitproviders"
	"github.com/luke/hive/control-plane/internal/api/infrastructure"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/api/mounts"
	"github.com/luke/hive/control-plane/internal/api/notifications"
	"github.com/luke/hive/control-plane/internal/api/organizations"
	"github.com/luke/hive/control-plane/internal/api/orgmembers"
	"github.com/luke/hive/control-plane/internal/api/password"
	"github.com/luke/hive/control-plane/internal/api/ports"
	"github.com/luke/hive/control-plane/internal/api/previews"
	"github.com/luke/hive/control-plane/internal/api/profile"
	"github.com/luke/hive/control-plane/internal/api/projects"
	"github.com/luke/hive/control-plane/internal/api/redirects"
	"github.com/luke/hive/control-plane/internal/api/registries"
	"github.com/luke/hive/control-plane/internal/api/schedules"
	"github.com/luke/hive/control-plane/internal/api/security"
	"github.com/luke/hive/control-plane/internal/api/settings"
	"github.com/luke/hive/control-plane/internal/api/stacks"
	"github.com/luke/hive/control-plane/internal/api/update"
	"github.com/luke/hive/control-plane/internal/api/volumebackups"
	"github.com/luke/hive/control-plane/internal/api/webhooks"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/ca"
	"github.com/luke/hive/control-plane/internal/realtime"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/luke/hive/control-plane/internal/updater"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/riverqueue/river"
)

type Server struct {
	Pool           *pgxpool.Pool
	Swarm          *swarmclient.Client
	Authority      *ca.Authority
	Auth           *auth.Service
	Hub            *realtime.Hub
	AgentDialer    *agentclient.Dialer
	RiverClient    *river.Client[pgx.Tx]
	BootstrapToken string
	Initialized    func() bool
	Updater        *updater.Updater
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(apimiddleware.WithLogger())
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(30 * time.Second))

	authHandler := authapi.NewHandler(s.Auth)
	orgHandler := organizations.NewHandler(s.Pool)
	apiKeyHandler := apikeys.NewHandler(s.Pool)
	orgMemberHandler := orgmembers.NewHandler(s.Pool)
	schedulesHandler := schedules.NewHandler(s.Pool)
	backupsHandler := backups.NewHandler(s.Pool)
	databasesHandler := databases.NewHandler(s.Pool, s.Swarm)
	gitProvidersHandler := gitproviders.NewHandler(s.Pool)
	projectsHandler := projects.NewHandler(s.Pool)
	environmentsHandler := environments.NewHandler(s.Pool)
	applicationsHandler := applications.NewHandler(s.Pool, s.Swarm)
	deploymentsHandler := deployments.NewHandler(s.Pool)
	buildsHandler := builds.NewHandler(s.Pool)
	domainsHandler := domains.NewHandler(s.Pool, s.Swarm)
	registriesHandler := registries.NewHandler(s.Pool)
	stacksHandler := stacks.NewHandler(s.Pool, s.Swarm)
	notificationsHandler := notifications.NewHandler(s.Pool)
	passwordHandler := password.NewHandler(s.Pool)
	eventsHandler := events.NewHandler(s.Pool, s.Auth, s.Hub)
	agentsHandler := agents.NewHandler(s.Pool, s.Swarm, s.AgentDialer, s.Authority, s.BootstrapToken)
	settingsHandler := settings.NewHandler(s.Pool)
	webhooksHandler := webhooks.NewHandler(s.Pool, s.RiverClient)
	infraHandler := infrastructure.NewHandler(s.Pool, s.Swarm)
	redirectsHandler := redirects.NewHandler(s.Pool)
	mountsHandler := mounts.NewHandler(s.Pool)
	portsHandler := ports.NewHandler(s.Pool)
	volumeBackupsHandler := volumebackups.NewHandler(s.Pool)
	previewsHandler := previews.NewHandler(s.Pool, s.RiverClient)
	profileHandler := profile.NewHandler(s.Pool)
	securityHandler := security.NewHandler(s.Pool, s.Swarm)
	updateHandler := update.NewHandler(s.Updater)

	r.Get("/api/v1/health", s.health)
	r.Get("/api/v1/ready", s.ready)
	r.Get("/api/v1/metrics", s.metrics)
	r.Get("/metrics", promhttp.Handler().ServeHTTP)

	// Rate-limited public endpoints
	authRateLimiter := apimiddleware.NewRateLimiter(10, time.Minute)
	webhookRateLimiter := apimiddleware.NewRateLimiter(60, time.Minute)

	r.With(authRateLimiter.Handler).Post("/api/v1/auth/register", authHandler.RegisterUser)
	r.With(authRateLimiter.Handler).Post("/api/v1/auth/login", authHandler.Login)
	r.With(authRateLimiter.Handler).Post("/api/v1/auth/refresh", authHandler.Refresh)
	r.With(authRateLimiter.Handler).Post("/api/v1/auth/logout", authHandler.Logout)
	r.With(authRateLimiter.Handler).Post("/api/v1/auth/send-reset-password", passwordHandler.SendResetPassword)
	r.With(authRateLimiter.Handler).Post("/api/v1/auth/reset-password", passwordHandler.ResetPassword)
	r.Post("/internal/agent/register", agentsHandler.RegisterAgent)
	r.With(webhookRateLimiter.Handler).Post("/api/v1/webhooks/github", webhooksHandler.GithubWebhook)
	r.With(webhookRateLimiter.Handler).Post("/api/v1/webhooks/gitlab", webhooksHandler.GitlabWebhook)
	r.With(webhookRateLimiter.Handler).Post("/api/v1/webhooks/bitbucket", webhooksHandler.BitbucketWebhook)
	r.With(webhookRateLimiter.Handler).Post("/api/v1/webhooks/gitea", webhooksHandler.GiteaWebhook)
	r.Get("/api/v1/invitations/{token}", orgMemberHandler.GetInvitationByToken)
	r.Get("/api/v1/ws/events", eventsHandler.WsEvents)

	r.Group(func(pr chi.Router) {
		pr.Use(apimiddleware.WithAuth(s.Auth, s.Pool))
		pr.Get("/api/v1/auth/me", authHandler.Me)
		pr.Get("/api/v1/profile", profileHandler.GetProfile)
		pr.Put("/api/v1/profile", profileHandler.UpdateProfile)
		pr.Post("/api/v1/profile/change-password", profileHandler.ChangePassword)
		pr.Get("/api/v1/projects", projectsHandler.ListProjects)
		pr.Post("/api/v1/projects", projectsHandler.CreateProject)
		pr.Get("/api/v1/projects/{id}", projectsHandler.GetProject)
		pr.Put("/api/v1/projects/{id}", projectsHandler.UpdateProject)
		pr.Delete("/api/v1/projects/{id}", projectsHandler.DeleteProject)
		pr.Get("/api/v1/environments", environmentsHandler.ListEnvironments)
		pr.Post("/api/v1/environments", environmentsHandler.CreateEnvironment)
		pr.Get("/api/v1/environments/{id}", environmentsHandler.GetEnvironment)
		pr.Put("/api/v1/environments/{id}", environmentsHandler.UpdateEnvironment)
		pr.Delete("/api/v1/environments/{id}", environmentsHandler.DeleteEnvironment)
		pr.Get("/api/v1/applications", applicationsHandler.ListApplications)
		pr.Post("/api/v1/applications", applicationsHandler.CreateApplication)
		pr.Get("/api/v1/applications/{id}", applicationsHandler.GetApplication)
		pr.Put("/api/v1/applications/{id}", applicationsHandler.UpdateApplication)
		pr.Delete("/api/v1/applications/{id}", applicationsHandler.DeleteApplication)
		pr.Post("/api/v1/applications/{id}/deploy", deploymentsHandler.EnqueueDeploy)
		pr.Post("/api/v1/applications/{id}/start", applicationsHandler.StartApplication)
		pr.Post("/api/v1/applications/{id}/stop", applicationsHandler.StopApplication)
		pr.Post("/api/v1/applications/{id}/restart", applicationsHandler.RestartApplication)
		pr.Get("/api/v1/applications/{id}/logs", deploymentsHandler.ApplicationLogs)
		pr.Get("/api/v1/applications/{id}/deployments", deploymentsHandler.ListApplicationDeployments)
		pr.Post("/api/v1/applications/{id}/rollback", deploymentsHandler.RollbackApplication)
		pr.Get("/api/v1/applications/{id}/env", applicationsHandler.ListAppEnvVars)
		pr.Post("/api/v1/applications/{id}/env", applicationsHandler.CreateAppEnvVar)
		pr.Put("/api/v1/applications/{id}/env/{varId}", applicationsHandler.UpdateAppEnvVar)
		pr.Delete("/api/v1/applications/{id}/env/{varId}", applicationsHandler.DeleteAppEnvVar)
		pr.Get("/api/v1/services", agentsHandler.ListServices)
		pr.Get("/api/v1/nodes", agentsHandler.ListNodes)

		// Terminal and log proxy routes
		pr.Get("/api/v1/ws/terminal/{containerID}", agentsHandler.WsTerminal)
		pr.Get("/api/v1/ws/logs/{containerID}", agentsHandler.WsLogs)

		// Host management routes
		pr.Get("/api/v1/nodes/{id}/metrics", agentsHandler.GetNodeMetrics)
		pr.Get("/api/v1/nodes/{id}/packages", agentsHandler.GetNodePackages)
		pr.Post("/api/v1/nodes/{id}/packages/check", agentsHandler.TriggerPackageCheck)
		pr.Post("/api/v1/nodes/{id}/maintain", agentsHandler.TriggerNodeMaintenance)
		pr.Get("/api/v1/cluster/resources", agentsHandler.GetClusterResources)

		pr.Get("/api/v1/organizations", orgHandler.ListOrganizations)
		pr.Post("/api/v1/organizations", orgHandler.CreateOrganization)
		pr.Post("/api/v1/organizations/{id}/api-keys", apiKeyHandler.CreateAPIKey)
		pr.Get("/api/v1/organizations/{id}/api-keys", apiKeyHandler.ListAPIKeys)
		pr.Delete("/api/v1/organizations/{id}/api-keys/{keyId}", apiKeyHandler.DeleteAPIKey)
		pr.Post("/api/v1/organizations/{id}/api-keys/{keyId}/regenerate", apiKeyHandler.RegenerateAPIKey)
		pr.Get("/api/v1/organizations/{id}/members", orgMemberHandler.ListOrganizationMembers)
		pr.Put("/api/v1/organizations/{id}/members/{userId}", orgMemberHandler.UpdateOrganizationMemberRole)
		pr.Get("/api/v1/organizations/{id}/invitations", orgMemberHandler.ListOrganizationInvitations)
		pr.Post("/api/v1/organizations/{id}/invitations", orgMemberHandler.CreateOrganizationInvitation)
		pr.Delete("/api/v1/organizations/{id}/invitations/{inviteId}", orgMemberHandler.DeleteOrganizationInvitation)
		pr.Post("/api/v1/organizations/{id}/invitations/{inviteId}/resend", orgMemberHandler.ResendOrganizationInvitation)
		pr.Post("/api/v1/organizations/{id}/invitations/{inviteId}/revoke", orgMemberHandler.RevokeOrganizationInvitation)
		pr.Post("/api/v1/invitations/{token}/accept", orgMemberHandler.AcceptInvitationByToken)
		pr.Get("/api/v1/domains", domainsHandler.ListDomains)
		pr.Post("/api/v1/domains", domainsHandler.CreateDomain)
		pr.Get("/api/v1/domains/{id}", domainsHandler.GetDomain)
		pr.Put("/api/v1/domains/{id}", domainsHandler.UpdateDomain)
		pr.Delete("/api/v1/domains/{id}", domainsHandler.DeleteDomain)
		pr.Get("/api/v1/registries", registriesHandler.ListRegistries)
		pr.Post("/api/v1/registries", registriesHandler.CreateRegistry)
		pr.Get("/api/v1/registries/{id}", registriesHandler.GetRegistry)
		pr.Put("/api/v1/registries/{id}", registriesHandler.UpdateRegistry)
		pr.Delete("/api/v1/registries/{id}", registriesHandler.DeleteRegistry)
		pr.Post("/api/v1/registries/{id}/test", registriesHandler.TestRegistry)
		pr.Get("/api/v1/stacks", stacksHandler.ListStacks)
		pr.Post("/api/v1/stacks", stacksHandler.CreateStack)
		pr.Get("/api/v1/stacks/{id}", stacksHandler.GetStack)
		pr.Put("/api/v1/stacks/{id}", stacksHandler.UpdateStack)
		pr.Delete("/api/v1/stacks/{id}", stacksHandler.DeleteStack)
		pr.Post("/api/v1/stacks/{id}/deploy", stacksHandler.DeployStack)
		pr.Post("/api/v1/stacks/{id}/start", stacksHandler.StartStack)
		pr.Post("/api/v1/stacks/{id}/stop", stacksHandler.StopStack)
		pr.Post("/api/v1/stacks/{id}/restart", stacksHandler.RestartStack)
		pr.Get("/api/v1/secrets", infraHandler.ListSecrets)
		pr.Post("/api/v1/secrets", infraHandler.CreateSecret)
		pr.Get("/api/v1/configs", infraHandler.ListConfigs)
		pr.Post("/api/v1/configs", infraHandler.CreateConfig)
		pr.Get("/api/v1/networks", infraHandler.ListNetworks)
		pr.Post("/api/v1/networks", infraHandler.CreateNetwork)
		pr.Get("/api/v1/redirects", redirectsHandler.ListRedirects)
		pr.Post("/api/v1/redirects", redirectsHandler.CreateRedirect)
		pr.Get("/api/v1/redirects/{id}", redirectsHandler.GetRedirect)
		pr.Put("/api/v1/redirects/{id}", redirectsHandler.UpdateRedirect)
		pr.Delete("/api/v1/redirects/{id}", redirectsHandler.DeleteRedirect)
		pr.Get("/api/v1/mounts", mountsHandler.ListMounts)
		pr.Post("/api/v1/mounts", mountsHandler.CreateMount)
		pr.Get("/api/v1/mounts/{id}", mountsHandler.GetMount)
		pr.Put("/api/v1/mounts/{id}", mountsHandler.UpdateMount)
		pr.Delete("/api/v1/mounts/{id}", mountsHandler.DeleteMount)
		pr.Get("/api/v1/ports", portsHandler.ListPortPolicies)
		pr.Post("/api/v1/ports", portsHandler.CreatePortPolicy)
		pr.Get("/api/v1/ports/{id}", portsHandler.GetPortPolicy)
		pr.Put("/api/v1/ports/{id}", portsHandler.UpdatePortPolicy)
		pr.Delete("/api/v1/ports/{id}", portsHandler.DeletePortPolicy)
		pr.Get("/api/v1/volume-backups", volumeBackupsHandler.ListVolumeBackups)
		pr.Post("/api/v1/volume-backups", volumeBackupsHandler.CreateVolumeBackup)
		pr.Get("/api/v1/volume-backups/{id}", volumeBackupsHandler.GetVolumeBackup)
		pr.Delete("/api/v1/volume-backups/{id}", volumeBackupsHandler.DeleteVolumeBackup)
		pr.Get("/api/v1/security-rules", securityHandler.ListSecurityRules)
		pr.Post("/api/v1/security-rules", securityHandler.CreateSecurityRule)
		pr.Get("/api/v1/security-rules/{id}", securityHandler.GetSecurityRule)
		pr.Put("/api/v1/security-rules/{id}", securityHandler.UpdateSecurityRule)
		pr.Delete("/api/v1/security-rules/{id}", securityHandler.DeleteSecurityRule)
		pr.Get("/api/v1/applications/{id}/previews", previewsHandler.ListPreviewDeployments)
		pr.Post("/api/v1/applications/{id}/previews", previewsHandler.CreatePreviewDeployment)
		pr.Get("/api/v1/applications/{id}/previews/{previewId}", previewsHandler.GetPreviewDeployment)
		pr.Delete("/api/v1/applications/{id}/previews/{previewId}", previewsHandler.DeletePreviewDeployment)
		pr.Get("/api/v1/builds", buildsHandler.ListBuilds)
		pr.Get("/api/v1/deployments", deploymentsHandler.ListDeployments)
		pr.Delete("/api/v1/deployments/{id}", deploymentsHandler.DeleteDeployment)
		pr.Get("/api/v1/builds/queue", buildsHandler.ListBuildQueue)
		pr.Post("/api/v1/builds/{id}/cancel", buildsHandler.CancelBuild)
		pr.Post("/api/v1/builds/{id}/retry", buildsHandler.RetryBuild)
		pr.Get("/api/v1/settings", settingsHandler.GetSettings)
		pr.Put("/api/v1/settings", settingsHandler.PutSettings)
		pr.Get("/api/v1/settings/servers", agentsHandler.ListServers)
		pr.Post("/api/v1/settings/servers", agentsHandler.CreateServer)
		pr.Get("/api/v1/settings/cluster", agentsHandler.ClusterInfo)
		pr.Get("/api/v1/settings/ssh-keys", infraHandler.ListSSHKeys)
		pr.Post("/api/v1/settings/ssh-keys", infraHandler.CreateSSHKey)
		pr.Get("/api/v1/settings/certificates", infraHandler.ListCertificates)
		pr.Post("/api/v1/settings/certificates", infraHandler.CreateCertificate)
		pr.Get("/api/v1/settings/requests", eventsHandler.ListRequestEvents)
		pr.Get("/api/v1/schedules", schedulesHandler.ListSchedules)
		pr.Post("/api/v1/schedules", schedulesHandler.CreateSchedule)
		pr.Put("/api/v1/schedules/{id}", schedulesHandler.UpdateSchedule)
		pr.Delete("/api/v1/schedules/{id}", schedulesHandler.DeleteSchedule)
		pr.Post("/api/v1/schedules/{id}/run", schedulesHandler.RunScheduleNow)
		pr.Get("/api/v1/backups", backupsHandler.ListBackups)
		pr.Post("/api/v1/backups", backupsHandler.CreateBackup)
		pr.Post("/api/v1/backups/{id}/restore", backupsHandler.RestoreBackup)
		pr.Get("/api/v1/backup/destinations", backupsHandler.ListBackupDestinations)
		pr.Post("/api/v1/backup/destinations", backupsHandler.CreateBackupDestination)
		pr.Get("/api/v1/backup/destinations/{id}", backupsHandler.GetBackupDestination)
		pr.Put("/api/v1/backup/destinations/{id}", backupsHandler.UpdateBackupDestination)
		pr.Delete("/api/v1/backup/destinations/{id}", backupsHandler.DeleteBackupDestination)
		pr.Post("/api/v1/backup/destinations/{id}/test", backupsHandler.TestBackupDestination)
		pr.Get("/api/v1/database-services", databasesHandler.ListDatabaseServices)
		pr.Post("/api/v1/database-services", databasesHandler.CreateDatabaseService)
		pr.Get("/api/v1/database-services/{id}", databasesHandler.GetDatabaseService)
		pr.Delete("/api/v1/database-services/{id}", databasesHandler.DeleteDatabaseService)
		pr.Get("/api/v1/git/providers", gitProvidersHandler.ListGitProviders)
		pr.Post("/api/v1/git/providers", gitProvidersHandler.CreateGitProvider)
		pr.Get("/api/v1/notifications", notificationsHandler.ListNotifications)
		pr.Post("/api/v1/notifications", notificationsHandler.CreateNotification)
		pr.Get("/api/v1/notifications/{id}", notificationsHandler.GetNotification)
		pr.Put("/api/v1/notifications/{id}", notificationsHandler.UpdateNotification)
		pr.Delete("/api/v1/notifications/{id}", notificationsHandler.DeleteNotification)
		pr.Post("/api/v1/notifications/{id}/test", notificationsHandler.TestNotification)
		pr.Get("/api/v1/system/update", updateHandler.GetStatus)
		pr.Post("/api/v1/system/update", updateHandler.TriggerUpdate)
	})

	// Serve UI static files if they exist. BrowserRouter-based SPA routes (for
	// example /register and /dashboard/overview) must fall back to index.html;
	// real static assets are still served directly from /ui/dist.
	if stat, err := os.Stat("/ui/dist"); err == nil && stat.IsDir() {
		const uiDist = "/ui/dist"
		indexPath := filepath.Join(uiDist, "index.html")
		fsys := http.FileServer(http.Dir(uiDist))
		spaHandler := func(w http.ResponseWriter, r *http.Request) {
			// If the path starts with /api or /internal, let Chi report a real 404
			// instead of returning the SPA shell for a missing backend endpoint.
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/") {
				http.NotFound(w, r)
				return
			}

			filePath := filepath.Join(uiDist, filepath.Clean(r.URL.Path))
			if fileInfo, err := os.Stat(filePath); err == nil && !fileInfo.IsDir() {
				if strings.HasPrefix(r.URL.Path, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-store")
				}
				fsys.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, indexPath)
		}
		r.Get("/*", spaHandler)
		r.Head("/*", spaHandler)
	}

	return r
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Pool.Ping(ctx); err != nil {
		http.Error(w, `{"message":"db unhealthy"}`, http.StatusServiceUnavailable)
		return
	}
	if err := s.Swarm.Ping(ctx); err != nil {
		http.Error(w, `{"message":"docker unhealthy"}`, http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if !s.Initialized() {
		http.Error(w, `{"message":"initializing"}`, http.StatusServiceUnavailable)
		return
	}
	s.health(w, r)
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	var projects int
	var applications int
	var queuedBuilds int
	_ = s.Pool.QueryRow(r.Context(), `select count(*) from projects`).Scan(&projects)
	_ = s.Pool.QueryRow(r.Context(), `select count(*) from applications`).Scan(&applications)
	_ = s.Pool.QueryRow(r.Context(), `select count(*) from build_jobs where status in ('queued','building')`).Scan(&queuedBuilds)
	services, _ := s.Swarm.ListServices(r.Context())
	nodes, _ := s.Swarm.ListNodes(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"projects":     projects,
		"applications": applications,
		"buildQueue":   queuedBuilds,
		"services":     len(services),
		"nodes":        len(nodes),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"error":   code,
		"message": message,
	})
}
