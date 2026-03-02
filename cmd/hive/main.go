package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/agent"
	"github.com/lholliger/hive/internal/alert"
	"github.com/lholliger/hive/internal/api"
	"github.com/lholliger/hive/internal/api/handlers"
	"github.com/lholliger/hive/internal/backup"
	"github.com/lholliger/hive/internal/bootstrap"
	hiveceph "github.com/lholliger/hive/internal/ceph"
	"github.com/lholliger/hive/internal/monitor"
	hivenats "github.com/lholliger/hive/internal/nats"
	"github.com/lholliger/hive/internal/notify"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/internal/tunnel"
	"github.com/lholliger/hive/internal/worker"
	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

func main() {
	cfg := config.Load()

	// Support "hive agent" as a sub-command to override the role
	if len(os.Args) > 1 && os.Args[1] == "agent" {
		cfg.Role = config.RoleAgent
	}

	logger, _ := zap.NewProduction()
	if cfg.DevMode {
		logger, _ = zap.NewDevelopment()
	}
	defer func() { _ = logger.Sync() }()
	log := logger.Sugar()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	switch cfg.Role {
	case config.RoleManager:
		if err := runManager(ctx, cfg, log); err != nil {
			log.Fatalf("manager failed: %v", err)
		}
	case config.RoleWorker:
		if err := runWorker(ctx, cfg, log); err != nil {
			log.Fatalf("worker failed: %v", err)
		}
	case config.RoleAgent:
		if err := runAgent(ctx, cfg, log); err != nil {
			log.Fatalf("agent failed: %v", err)
		}
	default:
		log.Fatalf("unknown role: %s", cfg.Role)
	}

	sig := <-sigCh
	log.Infof("received signal %v, shutting down", sig)
	cancel()
}

func runManager(ctx context.Context, cfg *config.Config, log *zap.SugaredLogger) error {
	log.Info("starting hive in manager mode")

	bs := bootstrap.New(cfg, log)
	if err := bs.Run(ctx); err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}

	var db *store.Store
	if cfg.DatabaseURL != "" {
		var err error
		db, err = store.New(cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("store init: %w", err)
		}
		defer func() { _ = db.Close() }()
	}

	ns, err := hivenats.StartEmbedded(cfg, log)
	if err != nil {
		return fmt.Errorf("nats start failed: %w", err)
	}
	defer ns.Shutdown()

	nc, err := hivenats.Connect(ns, cfg)
	if err != nil {
		return fmt.Errorf("nats connect failed: %w", err)
	}
	defer nc.Close()

	pool := worker.NewPool(nc, cfg, db, log)
	pool.Start(ctx)

	isLeader := tryAcquireLeaderLock(db)

	sched := backup.NewScheduler(nc, log)
	if isLeader {
		sched.Start()
		defer sched.Stop()
		if db != nil {
			go loadExistingBackupSchedules(ctx, db, sched, nc, log)
		}
		go startHealthMonitor(ctx, db, log)
	} else {
		log.Info("not the leader, skipping scheduler and health monitor")
	}

	sc, err := swarm.NewClient(log)
	if err != nil {
		log.Warnf("could not start node watcher: %v", err)
	} else {
		watcher := bootstrap.NewNodeWatcher(sc, cfg, db, log)
		watcher.Start(ctx)

		if cfg.IngressMode == "cloudflare_tunnel" || cfg.IngressMode == "both" {
			tunnelMgr := tunnel.NewCloudflaredManager(sc, cfg, log)
			if err := tunnelMgr.EnsureTunnel(ctx); err != nil {
				log.Warnf("cloudflared tunnel: %v", err)
			}
		}
	}

	server := api.NewServer(cfg, nc, db, log)
	go func() {
		if err := server.Start(); err != nil {
			log.Errorf("api server error: %v", err)
		}
	}()

	// Subscribe to agent metrics via NATS
	startMetricsAggregation(ctx, nc, db, log)

	// Subscribe to Ceph health reports via NATS
	cephAgg := hiveceph.NewHealthAggregator(nc, db, log)
	cephAgg.Start(ctx)

	log.Infof("hive manager ready on :%d", cfg.APIPort)
	return nil
}

func runWorker(ctx context.Context, cfg *config.Config, log *zap.SugaredLogger) error {
	log.Info("starting hive in worker mode")

	nc, err := hivenats.ConnectExternal(cfg)
	if err != nil {
		return fmt.Errorf("nats connect failed: %w", err)
	}
	defer nc.Close()

	pool := worker.NewPool(nc, cfg, nil, log)
	pool.Start(ctx)

	log.Info("hive worker ready")
	return nil
}

func runAgent(ctx context.Context, cfg *config.Config, log *zap.SugaredLogger) error {
	log.Info("starting hive in agent mode")

	natsURL := cfg.NATSManagerURL
	if natsURL == "" {
		natsURL = fmt.Sprintf("nats://127.0.0.1:%d", cfg.NATSPort)
	}

	nc, err := nats.Connect(natsURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return fmt.Errorf("nats connect: %w", err)
	}
	defer nc.Close()

	interval := time.Duration(cfg.AgentInterval) * time.Second
	if interval < time.Second {
		interval = 10 * time.Second
	}

	hostname, _ := os.Hostname()
	a := agent.New(nc, hostname, interval, log)
	go a.Run(ctx)

	cephExec := agent.NewCephExecutor(nc, hostname, log)
	cephExec.Start(ctx)

	cephHealth := agent.NewCephHealthReporter(nc, hostname, 30*time.Second, log)
	go cephHealth.Run(ctx)

	log.Infof("hive agent ready, reporting every %s", interval)
	return nil
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
		} else {
			log.Infof("loaded backup schedule: %s (cron: %s)", c.ID, c.Schedule)
		}
	}

	// Listen for new schedule events
	if _, err := nc.Subscribe("hive.backup.schedule", func(msg *nats.Msg) {
		var ev map[string]string
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			return
		}
		if ev["action"] == "schedule" {
			if err := sched.AddJob(ev["schedule"], ev["config_id"]); err != nil {
				log.Warnf("dynamic add backup schedule: %v", err)
			}
		}
	}); err != nil {
		log.Warnf("subscribe hive.backup.schedule: %v", err)
	}
}

func startMetricsAggregation(ctx context.Context, nc *nats.Conn, db *store.Store, log *zap.SugaredLogger) {
	// Subscribe to all agent metric reports
	if _, err := nc.Subscribe("hive.metrics.>", func(msg *nats.Msg) {
		var report agent.NodeMetricsReport
		if err := json.Unmarshal(msg.Data, &report); err != nil {
			log.Debugf("metrics decode: %v", err)
			return
		}
		handlers.MetricsCache.Update(&report)
	}); err != nil {
		log.Warnf("subscribe hive.metrics: %v", err)
	}

	// Periodically write snapshots to DB and purge old data
	go func() {
		snapshotTicker := time.NewTicker(60 * time.Second)
		purgeTicker := time.NewTicker(1 * time.Hour)
		staleTicker := time.NewTicker(60 * time.Second)
		defer snapshotTicker.Stop()
		defer purgeTicker.Stop()
		defer staleTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
		case <-snapshotTicker.C:
			if db == nil {
				continue
			}
			reports := handlers.MetricsCache.GetAll()
			for _, report := range reports {
				data, err := json.Marshal(report)
				if err != nil {
					continue
				}
				if err := db.InsertMetricsSnapshot(ctx, report.NodeID, data); err != nil {
					log.Debugf("metrics snapshot write: %v", err)
				}
			}

			evaluator := alert.NewEvaluator(db, log)
			fired := evaluator.Evaluate(ctx, reports)
			for _, f := range fired {
				d := notify.NewDispatcher(db, log)
				d.Send(ctx, notify.Event{
					Type:  "alert.fired",
					OrgID: f.OrgID,
					Title: fmt.Sprintf("Alert: %s on %s", f.Metric, f.NodeID),
					Message: fmt.Sprintf("Node **%s**: %s = %.1f (threshold: %s %.1f)",
						f.NodeID, f.Metric, f.Value, f.Operator, f.Threshold),
				})
				if err := db.UpdateAlertThresholdFired(ctx, f.ThresholdID); err != nil {
					log.Warnf("update alert threshold fired: %v", err)
				}
			}
			case <-purgeTicker.C:
				if db == nil {
					continue
				}
				deleted, err := db.PurgeOldMetricsSnapshots(ctx, 7*24*time.Hour)
				if err != nil {
					log.Debugf("metrics purge: %v", err)
				} else if deleted > 0 {
					log.Infof("purged %d old metrics snapshots", deleted)
				}
			case <-staleTicker.C:
				staleInterval := 30 * time.Second // 3x the default 10s interval
				staleNodes := handlers.MetricsCache.StaleNodes(staleInterval)
				for _, nodeID := range staleNodes {
					log.Warnf("stale node detected (no metrics): %s", nodeID)
				}
			}
		}
	}()

	log.Info("metrics aggregation started (NATS subscription on hive.metrics.>)")
}

func tryAcquireLeaderLock(db *store.Store) bool {
	if db == nil {
		return true
	}
	var acquired bool
	err := db.DB().QueryRow("SELECT pg_try_advisory_lock(42)").Scan(&acquired)
	if err != nil {
		return true
	}
	return acquired
}

func startHealthMonitor(ctx context.Context, db *store.Store, log *zap.SugaredLogger) {
	collector, err := monitor.NewCollector(log)
	if err != nil {
		log.Warnf("health monitor: %v", err)
		return
	}

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			health, err := collector.CheckServices(ctx)
			if err != nil {
				log.Warnf("health check: %v", err)
				continue
			}
			for _, svc := range health {
				if !svc.Healthy {
					log.Warnf("unhealthy service: %s (running=%d, desired=%d)", svc.ServiceName, svc.Running, svc.Replicas)
				}
			}
		}
	}
}
