package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/agent"
	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

// The hive binary now primarily serves the agent role.
// Manager role is handled by SvelteKit (hive-manager).
// Engine role is handled by hive-engine (cmd/engine).

func main() {
	cfg := config.Load()

	if len(os.Args) > 1 && os.Args[1] == "agent" {
		cfg.Role = config.RoleAgent
	}

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

	switch cfg.Role {
	case config.RoleAgent:
		if err := runAgent(ctx, cfg, log); err != nil {
			log.Fatalf("agent failed: %v", err)
		}
	default:
		log.Infof("hive binary now only supports agent role")
		log.Infof("  manager → use SvelteKit (hive-manager image)")
		log.Infof("  engine  → use hive-engine binary")
		log.Fatalf("unsupported role for hive binary: %s", cfg.Role)
	}
}

func runAgent(ctx context.Context, cfg *config.Config, log *zap.SugaredLogger) error {
	log.Info("starting hive in agent mode")

	natsURL := cfg.NATSManagerURL

	nc, err := nats.Connect(natsURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return fmt.Errorf("nats connect: %w", err)
	}
	defer nc.Close()

	hostname := os.Getenv("NODE_HOSTNAME")
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	a := agent.New(nc, hostname, log)
	go a.Run(ctx)

	maint := agent.NewMaintenanceHandler(nc, hostname, log)
	go maint.Start(ctx)

	osUpdates := agent.NewOSUpdateChecker(nc, hostname, log)
	go osUpdates.Start(ctx)

	cephExec := agent.NewCephExecutor(nc, hostname, log)
	cephExec.Start(ctx)

	cephHealth := agent.NewCephHealthReporter(nc, hostname, 30*time.Second, log)
	go cephHealth.Run(ctx)

	log.Infof("hive agent ready (metrics via Prometheus node-exporter)")

	<-ctx.Done()
	log.Info("shutting down hive agent")
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
