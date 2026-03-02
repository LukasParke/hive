package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/api/handlers"
	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/api/ws"
	"github.com/lholliger/hive/internal/rbac"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

type Server struct {
	cfg    *config.Config
	nc     *nats.Conn
	store  *store.Store
	log    *zap.SugaredLogger
	router chi.Router
}

func NewServer(cfg *config.Config, nc *nats.Conn, s *store.Store, log *zap.SugaredLogger) *Server {
	srv := &Server{cfg: cfg, nc: nc, store: s, log: log}
	srv.router = srv.buildRouter()
	return srv
}

func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.cfg.APIPort)
	s.log.Infof("API server listening on %s", addr)
	return http.ListenAndServe(addr, s.router)
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(s.corsMiddleware())

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.Auth(s.cfg.AuthBaseURL))
		if s.store != nil {
			r.Use(middleware.StoreMiddleware(s.store))
			r.Use(middleware.AuditLogger(s.store))
		}

		r.Get("/system/status", handlers.SystemStatus(s.nc, s.cfg))
		r.Get("/system/logs", handlers.GetSystemLogs)
		r.Route("/log-forwards", func(r chi.Router) {
			r.Get("/", handlers.ListLogForwards)
			r.Post("/", handlers.CreateLogForward)
			r.Delete("/{forwardId}", handlers.DeleteLogForward)
		})

		r.Route("/projects", func(r chi.Router) {
			r.With(rbac.RequirePermission(rbac.PermManageProject)).Post("/", handlers.CreateProject)
			r.Get("/", handlers.ListProjects)
			r.Route("/{projectId}", func(r chi.Router) {
				r.Get("/", handlers.GetProject)
				r.With(rbac.RequirePermission(rbac.PermManageProject)).Delete("/", handlers.DeleteProject)

				r.Route("/apps", func(r chi.Router) {
					r.With(rbac.RequirePermission(rbac.PermManageApp)).Post("/", handlers.CreateApp(s.nc))
					r.Get("/", handlers.ListApps)
					r.Route("/{appId}", func(r chi.Router) {
						r.Get("/", handlers.GetApp)
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Post("/export-template", handlers.ExportAppAsTemplate)
						r.Get("/tasks", handlers.AppTasks)
						r.Get("/events", handlers.AppEvents)
						r.Get("/ports", handlers.AppPorts)
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Delete("/", handlers.DeleteApp(s.nc))
						r.With(rbac.RequirePermission(rbac.PermDeployApp)).Post("/deploy", handlers.DeployApp(s.nc))
						r.Get("/deployments", handlers.ListDeployments)
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Put("/env", handlers.UpdateAppEnv)
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Put("/domains", handlers.UpdateAppDomains)
						r.With(rbac.RequirePermission(rbac.PermDeployApp)).Post("/restart", handlers.RestartApp(s.nc))
						r.With(rbac.RequirePermission(rbac.PermDeployApp)).Post("/stop", handlers.StopApp)
						r.With(rbac.RequirePermission(rbac.PermDeployApp)).Post("/start", handlers.StartApp)
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Put("/scale", handlers.ScaleApp)
						r.With(rbac.RequirePermission(rbac.PermDeployApp)).Post("/rollback", handlers.RollbackApp)
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Put("/resources", handlers.UpdateAppResources)
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Put("/healthcheck", handlers.UpdateAppHealthCheck)
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Put("/placement", handlers.UpdateAppPlacement)
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Put("/update-strategy", handlers.UpdateAppUpdateStrategy)
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Put("/labels", handlers.UpdateAppLabels)
						r.Get("/logs/query", handlers.QueryAppLogs)
						r.Get("/logs", ws.AppLogs(s.nc))
						r.Get("/container-logs", ws.ContainerLogs())
						r.Get("/previews", handlers.ListPreviews)
						r.With(rbac.RequirePermission(rbac.PermDeployApp)).Post("/previews", handlers.CreatePreview(s.nc))
						r.With(rbac.RequirePermission(rbac.PermManageApp)).Delete("/previews/{previewId}", handlers.DeletePreview)
						r.Route("/links", func(r chi.Router) {
							r.With(rbac.RequirePermission(rbac.PermManageApp)).Post("/", handlers.CreateServiceLink)
							r.Get("/", handlers.ListServiceLinks)
							r.With(rbac.RequirePermission(rbac.PermManageApp)).Delete("/{linkId}", handlers.DeleteServiceLink)
						})
						r.Route("/env-vars", func(r chi.Router) {
							r.Get("/", handlers.ListEnvVars)
							r.With(rbac.RequirePermission(rbac.PermManageApp)).Post("/", handlers.SetEnvVar)
							r.With(rbac.RequirePermission(rbac.PermManageApp)).Delete("/{key}", handlers.DeleteEnvVar)
							r.With(rbac.RequirePermission(rbac.PermManageApp)).Post("/import", handlers.ImportEnvVars)
							r.Get("/export", handlers.ExportEnvVars)
						})
					})
				})

				r.Route("/databases", func(r chi.Router) {
					r.With(rbac.RequirePermission(rbac.PermManageApp)).Post("/", handlers.CreateDatabase(s.nc))
					r.Get("/", handlers.ListDatabases)
				})

				r.Route("/secrets", func(r chi.Router) {
					r.Use(rbac.RequirePermission(rbac.PermManageSecrets))
					r.Post("/", handlers.CreateSecret(s.nc))
					r.Get("/", handlers.ListSecrets)
					r.Delete("/{secretId}", handlers.DeleteSecret)
					r.Post("/{secretId}/attach/{appId}", handlers.AttachSecret)
					r.Delete("/{secretId}/detach/{appId}", handlers.DetachSecret)
				})

				r.Route("/volumes", func(r chi.Router) {
					r.Get("/", handlers.ListVolumes)
					r.Get("/{volumeId}", handlers.GetVolume)
					r.With(rbac.RequirePermission(rbac.PermManageStorage)).Post("/", handlers.CreateVolume)
					r.With(rbac.RequirePermission(rbac.PermManageStorage)).Delete("/{volumeId}", handlers.DeleteVolume)
					r.With(rbac.RequirePermission(rbac.PermManageStorage)).Post("/{volumeId}/attach/{appId}", handlers.AttachVolume)
					r.With(rbac.RequirePermission(rbac.PermManageStorage)).Delete("/{volumeId}/detach/{appId}", handlers.DetachVolume)
				})

				r.Route("/routes", func(r chi.Router) {
					r.Get("/", handlers.ListProxyRoutes)
					r.With(rbac.RequirePermission(rbac.PermManageApp)).Post("/", handlers.CreateProxyRoute)
					r.With(rbac.RequirePermission(rbac.PermManageApp)).Put("/{routeId}", handlers.UpdateProxyRoute)
					r.With(rbac.RequirePermission(rbac.PermManageApp)).Delete("/{routeId}", handlers.DeleteProxyRoute)
				})

				r.Route("/certificates", func(r chi.Router) {
					r.Get("/", handlers.ListCustomCertificates)
					r.With(rbac.RequirePermission(rbac.PermManageSettings)).Post("/", handlers.CreateCustomCertificate)
					r.With(rbac.RequirePermission(rbac.PermManageSettings)).Delete("/{certId}", handlers.DeleteCustomCertificate)
				})

				r.Route("/stacks", func(r chi.Router) {
					r.Get("/", handlers.ListStacks)
					r.Get("/{stackId}", handlers.GetStack)
					r.With(rbac.RequirePermission(rbac.PermDeployApp)).Post("/", handlers.CreateStack(s.nc))
					r.With(rbac.RequirePermission(rbac.PermDeployApp)).Put("/{stackId}", handlers.UpdateStack(s.nc))
					r.With(rbac.RequirePermission(rbac.PermManageApp)).Delete("/{stackId}", handlers.DeleteStack(s.nc))
				})
			})
		})

		r.Route("/nodes", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermViewMetrics))
			r.Get("/", handlers.ListNodes)
			r.Get("/metrics", handlers.ClusterMetrics)
			r.Get("/{nodeId}", handlers.GetNode)
			r.Get("/{nodeId}/stats", handlers.NodeStats)
			r.Get("/{nodeId}/metrics", handlers.NodeMetricsLatest)
			r.Get("/{nodeId}/metrics/history", handlers.NodeMetricsHistory)
			r.With(rbac.RequirePermission(rbac.PermManageSettings)).Put("/{nodeId}/labels", handlers.UpdateNodeLabels)
		})

		r.Route("/catalog", func(r chi.Router) {
			r.Get("/", handlers.ListCatalog)
			r.With(rbac.RequirePermission(rbac.PermDeployApp)).Post("/{templateId}/deploy", handlers.DeployCatalogApp(s.nc))
		})

		r.Route("/templates", func(r chi.Router) {
			r.Get("/", handlers.ListAllTemplates)
			r.Get("/{name}/updates", handlers.CheckTemplateUpdates)
			r.Get("/{name}", handlers.GetTemplate)
			r.With(rbac.RequirePermission(rbac.PermDeployApp)).Post("/{name}/deploy", handlers.DeployTemplate(s.nc))
		})
		r.Route("/template-sources", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageSettings))
			r.Get("/", handlers.ListTemplateSources)
			r.Post("/", handlers.CreateTemplateSource)
			r.Delete("/{sourceId}", handlers.DeleteTemplateSource)
			r.Post("/{sourceId}/sync", handlers.SyncTemplateSource)
		})
		r.Route("/custom-templates", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageSettings))
			r.Get("/", handlers.ListCustomTemplates)
			r.Put("/{templateId}", handlers.UpdateCustomTemplate)
			r.Delete("/{templateId}", handlers.DeleteCustomTemplate)
		})

		r.Route("/git-sources", func(r chi.Router) {
			r.Get("/", handlers.ListGitSources)
			r.With(rbac.RequirePermission(rbac.PermManageSettings)).Post("/", handlers.CreateGitSource)
			r.Route("/{sourceId}", func(r chi.Router) {
				r.Get("/repos", handlers.ListGitRepos(s.cfg))
				r.Get("/repos/{repo}/branches", handlers.ListGitRepoBranches)
				r.With(rbac.RequirePermission(rbac.PermManageSettings)).Post("/repos/{repo}/webhook", handlers.RegisterWebhook(s.cfg))
				r.Get("/repos/{repo}/detect", handlers.DetectBuildType)
			})
		})

		r.Route("/backups", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageBackups))
			r.Post("/", handlers.CreateBackupConfig(s.nc))
			r.Get("/", handlers.ListBackups)
			r.Post("/{configId}/trigger", handlers.TriggerBackup(s.nc))
			r.Get("/{configId}/runs", handlers.ListBackupRuns)
			r.Post("/{configId}/restore/{runId}", handlers.RestoreBackup(s.nc))
		})

		r.Route("/metrics", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermViewMetrics))
			r.Get("/services", handlers.MetricsServices)
			r.Get("/nodes", handlers.MetricsNodes)
		})

		r.Route("/notifications", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageSettings))
			r.Post("/", handlers.CreateNotificationChannel)
			r.Get("/", handlers.ListNotificationChannels)
			r.Delete("/{channelId}", handlers.DeleteNotificationChannel)
			r.Post("/{channelId}/test", handlers.TestNotificationChannel)
		})

		r.Get("/system/connectivity", handlers.CheckConnectivity)

		r.Route("/registry", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageSettings))
			r.Get("/status", handlers.RegistryStatus)
			r.Get("/images", handlers.RegistryImages)
			r.Delete("/images/{name}/{tag}", handlers.RegistryDeleteImage)
		})

		r.Route("/alerts", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageSettings))
			r.Post("/", handlers.CreateAlertThreshold)
			r.Get("/", handlers.ListAlertThresholds)
			r.Delete("/{alertId}", handlers.DeleteAlertThreshold)
		})

		r.Route("/storage-hosts", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageStorage))
			r.Post("/", handlers.CreateStorageHost)
			r.Get("/", handlers.ListStorageHosts)
			r.Route("/{hostId}", func(r chi.Router) {
				r.Get("/", handlers.GetStorageHost)
				r.Put("/", handlers.UpdateStorageHost)
				r.Delete("/", handlers.DeleteStorageHost)
				r.Post("/test", handlers.TestStorageHostConnectivity)
			})
		})

		r.Route("/ceph", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageStorage))
			r.Post("/clusters", handlers.CreateCephCluster(s.nc))
			r.Get("/clusters", handlers.ListCephClusters)
			r.Get("/discover-disks", handlers.DiscoverDisks)
			r.Get("/all-disks", handlers.DiscoverAllDisks)
			r.Route("/clusters/{clusterId}", func(r chi.Router) {
				r.Get("/", handlers.GetCephCluster)
				r.Delete("/", handlers.DeleteCephCluster(s.nc))
				r.Get("/health", handlers.GetCephClusterHealth)
				r.Get("/osds", handlers.ListCephOSDs)
				r.Post("/osds", handlers.AddCephOSD(s.nc))
				r.Delete("/osds/{osdId}", handlers.RemoveCephOSD(s.nc))
				r.Get("/pools", handlers.ListCephPools)
				r.Post("/pools", handlers.CreateCephPool(s.nc))
			})
		})

		r.Route("/dns-providers", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageDNS))
			r.Post("/", handlers.CreateDNSProvider)
			r.Get("/", handlers.ListDNSProviders)
			r.Route("/{providerId}", func(r chi.Router) {
				r.Get("/", handlers.GetDNSProvider)
				r.Delete("/", handlers.DeleteDNSProvider)
				r.Post("/test", handlers.TestDNSProvider)
				r.Get("/records", handlers.ListDNSRecords)
				r.Delete("/records/{recordId}", handlers.DeleteDNSRecord)
			})
		})

		r.Route("/members", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageMembers))
			r.Get("/", handlers.ListOrgMembers)
			r.Post("/", handlers.InviteMember)
			r.Put("/{userId}/role", handlers.UpdateMemberRole)
			r.Delete("/{userId}", handlers.RemoveMember)
		})
		r.Route("/audit", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageSettings))
			r.Get("/", handlers.ListAuditLogs)
			r.Get("/stats", handlers.GetAuditLogStats)
		})
		r.Route("/maintenance", func(r chi.Router) {
			r.Use(rbac.RequirePermission(rbac.PermManageMaintenance))
			r.Post("/", handlers.CreateMaintenanceTask)
			r.Get("/", handlers.ListMaintenanceTasks)
			r.Route("/{taskId}", func(r chi.Router) {
				r.Put("/", handlers.UpdateMaintenanceTask)
				r.Delete("/", handlers.DeleteMaintenanceTask)
				r.Post("/trigger", handlers.TriggerMaintenanceTask(s.nc))
				r.Get("/runs", handlers.ListMaintenanceRuns)
			})
		})
	})

	// Webhook endpoint (no auth required)
	r.Post("/api/v1/webhooks/{sourceId}", handlers.GitWebhook(s.nc, s.store))

	return r
}

func (s *Server) corsMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				origin = fmt.Sprintf("http://localhost:%d", s.cfg.UIPort)
			}
			if s.cfg.AllowedOrigins != "" {
				origin = s.cfg.AllowedOrigins
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
