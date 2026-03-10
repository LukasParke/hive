package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/robfig/cron/v3"

	"github.com/lholliger/hive/internal/backup"
	hiveceph "github.com/lholliger/hive/internal/ceph"
	"github.com/lholliger/hive/internal/engine"
	"github.com/lholliger/hive/internal/monitor"
	hivenats "github.com/lholliger/hive/internal/nats"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/internal/worker"
	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

func main() {
	cfg := config.Load()
	cfg.Role = "engine"

	logger, _ := buildLogger(cfg)
	defer func() { _ = logger.Sync() }()
	log := logger.Sugar()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Infof("received signal %v, shutting down", sig)
		cancel()
	}()

	if err := run(ctx, cfg, log); err != nil {
		log.Fatalf("engine failed: %v", err)
	}
}

func run(ctx context.Context, cfg *config.Config, log *zap.SugaredLogger) error {
	log.Info("starting hive-engine (infrastructure control plane)")

	nc, err := hivenats.ConnectExternal(cfg, log)
	if err != nil {
		return fmt.Errorf("nats connect: %w", err)
	}
	defer nc.Close()

	var db *store.Store
	if cfg.DatabaseURL != "" {
		db, err = store.New(cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("store init: %w", err)
		}
		defer func() { _ = db.Close() }()

		log.Info("running database migrations...")
		if err := db.RunMigrations(); err != nil {
			log.Warnf("migration: %v", err)
		}

		if t, e := db.GetSetting(ctx, "cf_tunnel_token"); e == nil && t != "" && cfg.CFTunnelToken == "" {
			cfg.CFTunnelToken = t
		}
	}

	pool := worker.NewPool(nc, cfg, db, log)
	pool.Start(ctx)

	sched := backup.NewScheduler(nc, log)
	sched.Start()
	defer sched.Stop()
	if db != nil {
		go loadExistingBackupSchedules(ctx, db, sched, nc, log)
	}

	sc, err := swarm.NewClient(log)
	if err != nil {
		log.Warnf("swarm client init: %v", err)
	}

	cephAgg := hiveceph.NewHealthAggregator(nc, db, log)
	cephAgg.Start(ctx)

	enginePort := 9090
	if p := os.Getenv("HIVE_ENGINE_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &enginePort)
	}
	engineSecret := readSecretOrEnv("/run/secrets/hive-engine-secret", "HIVE_ENGINE_SECRET")

	srv := engine.NewServer(nc, sc, db, cfg, log, enginePort, engineSecret)

	go srv.StartUpdateCache(ctx)
	go srv.StartMetricsBroadcast(ctx, 5)

	// --- System Task Manager ---
	mgr := engine.NewSystemTaskManager(db, log)

	// Health Monitor
	healthCollector, healthErr := monitor.NewCollector(log)
	if healthErr != nil {
		log.Warnf("health monitor init: %v", healthErr)
	}
	startedAt := time.Now()
	gracePeriod := 60 * time.Second
	if healthCollector != nil {
		mgr.Register("health_check", "Service Health Check",
			"Monitor Docker Swarm service health and update stack statuses",
			"monitoring", 30*time.Second, 0,
			func(ctx context.Context) error {
				return runHealthCheck(ctx, healthCollector, db, log, startedAt, gracePeriod)
			},
		)
	}

	// Log Collector
	if db != nil {
		logCollector, lcErr := monitor.NewLogCollector(db, log)
		if lcErr != nil {
			log.Warnf("log collector init: %v", lcErr)
		}
		if logCollector != nil {
			mgr.Register("log_collector", "Log Collector",
				"Collect logs from Docker Swarm services and store them",
				"monitoring", 10*time.Second, 0,
				logCollector.CollectOnce,
			)
		}
	}

	// Log Retention
	if db != nil {
		mgr.Register("log_retention", "Log Retention",
			"Purge log entries older than 7 days",
			"maintenance", 1*time.Hour, 0,
			func(ctx context.Context) error {
				retention := 7 * 24 * time.Hour
				deleted, err := db.PurgeOldLogs(ctx, retention)
				if err != nil {
					return err
				}
				if deleted > 0 {
					log.Infof("log retention: purged %d entries older than %s", deleted, retention)
				}
				return nil
			},
		)
	}

	// Image Update Checker
	if sc != nil && db != nil {
		imageChecker := engine.NewImageChecker(sc, db, log)
		mgr.Register("image_checker", "Image Update Checker",
			"Check Docker registries for newer images of deployed services",
			"updates", 4*time.Hour, 30*time.Second,
			imageChecker.CheckOnce,
		)

		serviceUpdater := engine.NewServiceUpdater(sc, nc, db, log)
		updateScheduler := engine.NewUpdateScheduler(sc, nc, db, log, serviceUpdater, srv.GetUpdateCache())
		mgr.Register("update_scheduler", "Auto-Update Scheduler",
			"Evaluate auto-update policies and apply updates within maintenance windows",
			"updates", 60*time.Second, 0,
			updateScheduler.EvalOnce,
		)

		gitPoller := engine.NewGitPoller(nc, db, log)
		mgr.Register("git_poller", "Git Poller",
			"Check for new commits on auto-deploy repositories and trigger builds",
			"deployment", 15*time.Minute, 2*time.Minute,
			gitPoller.PollOnce,
		)
	}

	// Alert Evaluator
	if db != nil {
		mgr.Register("alert_evaluator", "Alert Evaluator",
			"Check alert thresholds against node metrics and send notifications",
			"monitoring", 60*time.Second, 0,
			func(ctx context.Context) error {
				engine.EvaluateAlertsOnce(ctx, db, log)
				return nil
			},
		)
	}

	// DNS Validation
	if db != nil {
		mgr.Register("dns_validation", "DNS Record Validation",
			"Validate managed DNS records match expected configuration and repair drift",
			"networking", 1*time.Hour, 2*time.Minute,
			func(ctx context.Context) error {
				return srv.ValidateDNSRecords(ctx)
			},
		)
	}

	// Tunnel Health Check
	if sc != nil {
		mgr.Register("tunnel_health", "Tunnel Health Check",
			"Verify Cloudflare tunnel service is running and redeploy if needed",
			"networking", 5*time.Minute, 30*time.Second,
			func(ctx context.Context) error {
				return srv.CheckTunnelHealth(ctx)
			},
		)
	}

	// Traefik Health Check
	if sc != nil {
		mgr.Register("traefik_health", "Traefik Health Check",
			"Verify Traefik service health and force-update if replicas are failing",
			"networking", 2*time.Minute, 30*time.Second,
			func(ctx context.Context) error {
				return srv.CheckTraefikHealth(ctx)
			},
		)
	}

	// CF Token Sync
	if db != nil && sc != nil {
		mgr.Register("cf_token_sync", "Cloudflare Token Sync",
			"Ensure Cloudflare API token is propagated to the Traefik service",
			"networking", 1*time.Hour, 15*time.Second,
			func(ctx context.Context) error {
				return srv.EnsureTraefikCFToken(ctx)
			},
		)
	}

	// Stale Run Reconciliation
	if db != nil {
		mgr.Register("stale_run_reconcile", "Stale Run Reconciliation",
			"Mark backup and maintenance runs stuck in running state as failed",
			"maintenance", 15*time.Minute, 2*time.Minute,
			func(ctx context.Context) error {
				staleThreshold := 2 * time.Hour
				reconciled, err := db.ReconcileStaleRuns(ctx, staleThreshold)
				if err != nil {
					return err
				}
				if reconciled > 0 {
					log.Infof("stale run reconcile: marked %d stale runs as failed", reconciled)
				}
				return nil
			},
		)
	}

	// Template Source Sync
	if db != nil {
		mgr.Register("template_sync", "Template Source Sync",
			"Sync external template sources to keep the catalog up to date",
			"deployment", 24*time.Hour, 5*time.Minute,
			func(ctx context.Context) error {
				return srv.SyncAllTemplateSources(ctx)
			},
		)
	}

	// Scheduled Job Dispatcher
	if db != nil {
		mgr.Register("scheduled_jobs_dispatch", "Scheduled Job Dispatcher",
			"Dispatch due scheduled jobs to worker pool and compute next_run_at",
			"operations", 1*time.Minute, 15*time.Second,
			func(ctx context.Context) error {
				return dispatchDueScheduledJobs(ctx, db, nc, log)
			},
		)
	}

	srv.SetTaskManager(mgr)

	mgr.Start(ctx)
	log.Info("system task manager started")

	go func() {
		if err := srv.Start(); err != nil {
			log.Errorf("engine API error: %v", err)
		}
	}()

	log.Infof("hive-engine ready on :%d", enginePort)

	<-ctx.Done()
	log.Info("shutting down hive-engine")
	return nil
}

func runHealthCheck(ctx context.Context, collector *monitor.Collector, db *store.Store, log *zap.SugaredLogger, startedAt time.Time, gracePeriod time.Duration) error {
	health, err := collector.CheckServices(ctx)
	if err != nil {
		return err
	}
	inGrace := time.Since(startedAt) < gracePeriod
	for _, svc := range health {
		if !svc.Healthy {
			if inGrace {
				log.Debugf("unhealthy service (startup grace): %s (%d/%d)", svc.ServiceName, svc.Running, svc.Replicas)
			} else {
				log.Warnf("unhealthy service: %s (%d/%d)", svc.ServiceName, svc.Running, svc.Replicas)
			}
		}
	}
	monitor.ServiceHealthCache.Update(health)

	if !inGrace && db != nil {
		stackHealth := make(map[string]bool)
		for _, svc := range health {
			if sid, ok := svc.Labels["hive.stack_id"]; ok && sid != "" {
				if _, exists := stackHealth[sid]; !exists {
					stackHealth[sid] = true
				}
				if !svc.Healthy {
					stackHealth[sid] = false
				}
			}
		}
		for sid, allHealthy := range stackHealth {
			st, err := db.GetStack(ctx, sid)
			if err != nil || st == nil {
				continue
			}
			if !allHealthy && st.Status == "running" {
				_ = db.UpdateStackStatus(ctx, sid, "degraded")
			} else if allHealthy && (st.Status == "degraded" || st.Status == "failed") {
				_ = db.UpdateStackStatus(ctx, sid, "running")
			}
		}
	}
	return nil
}

func buildLogger(cfg *config.Config) (*zap.Logger, error) {
	var zapCfg zap.Config
	if cfg.DevMode {
		zapCfg = zap.NewDevelopmentConfig()
	} else {
		zapCfg = zap.NewProductionConfig()
	}
	switch cfg.LogLevel {
	case "debug":
		zapCfg.Level.SetLevel(zap.DebugLevel)
	case "warn":
		zapCfg.Level.SetLevel(zap.WarnLevel)
	case "error":
		zapCfg.Level.SetLevel(zap.ErrorLevel)
	default:
		zapCfg.Level.SetLevel(zap.InfoLevel)
	}
	return zapCfg.Build()
}

func loadExistingBackupSchedules(ctx context.Context, db *store.Store, sched *backup.Scheduler, nc *nats.Conn, log *zap.SugaredLogger) {
	configs, err := db.ListBackupConfigs(ctx)
	if err != nil {
		log.Warnf("load backup schedules: %v", err)
		return
	}
	for _, c := range configs {
		if err := sched.AddJob(c.Schedule, c.ID); err != nil {
			log.Warnf("add backup schedule %s: %v", c.ID, err)
		}
	}

	if _, err := nc.Subscribe("hive.backup.schedule", func(msg *nats.Msg) {
		var ev map[string]string
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			return
		}
		if ev["action"] == "schedule" {
			_ = sched.AddJob(ev["schedule"], ev["config_id"])
		}
	}); err != nil {
		log.Warnf("subscribe hive.backup.schedule: %v", err)
	}
}

func readSecretOrEnv(filePath, envVar string) string {
	if data, err := os.ReadFile(filePath); err == nil {
		val := string(data)
		return strings.TrimSpace(val)
	}
	return os.Getenv(envVar)
}

func dispatchDueScheduledJobs(ctx context.Context, db *store.Store, nc *nats.Conn, log *zap.SugaredLogger) error {
	jobs, err := db.ListAllEnabledJobs(ctx)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	now := time.Now().UTC()

	for _, job := range jobs {
		location := time.UTC
		if job.Timezone != "" {
			if loc, err := time.LoadLocation(job.Timezone); err == nil {
				location = loc
			}
		}

		sched, err := parser.Parse(job.Schedule)
		if err != nil {
			log.Warnf("scheduled job %s has invalid cron %q: %v", job.ID, job.Schedule, err)
			continue
		}

		due := false
		if job.NextRunAt == nil {
			due = true
		} else if !job.NextRunAt.After(now) {
			due = true
		}
		if !due {
			continue
		}

		payload, _ := json.Marshal(map[string]string{
			"action": "run_job",
			"job_id": job.ID,
		})
		if err := nc.Publish("hive.deploy", payload); err != nil {
			log.Warnf("scheduled job publish %s: %v", job.ID, err)
			continue
		}

		next := sched.Next(now.In(location)).UTC()
		if err := db.UpdateJobLastRun(ctx, job.ID, &next); err != nil {
			log.Warnf("scheduled job update next_run_at %s: %v", job.ID, err)
		}
	}
	return nil
}
