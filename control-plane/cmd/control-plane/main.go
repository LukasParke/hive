package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/luke/hive/control-plane/internal/agentclient"
	"github.com/luke/hive/control-plane/internal/api"
	"github.com/luke/hive/control-plane/internal/auth"
	backupruntime "github.com/luke/hive/control-plane/internal/backup"
	buildruntime "github.com/luke/hive/control-plane/internal/build"
	"github.com/luke/hive/control-plane/internal/ca"
	"github.com/luke/hive/control-plane/internal/config"
	"github.com/luke/hive/control-plane/internal/db"
	"github.com/luke/hive/control-plane/internal/jobs"
	buildjobs "github.com/luke/hive/control-plane/internal/jobs/build"
	"github.com/luke/hive/control-plane/internal/jobs/riverjobs"
	"github.com/luke/hive/control-plane/internal/leader"
	"github.com/luke/hive/control-plane/internal/notify"
	"github.com/luke/hive/control-plane/internal/realtime"
	"github.com/luke/hive/control-plane/internal/reconcile"
	"github.com/luke/hive/control-plane/internal/secrets"
	"github.com/luke/hive/control-plane/internal/swarm"
	"github.com/luke/hive/control-plane/internal/updater"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx := context.Background()
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config: %v", err)
	}
	var initialized atomic.Bool

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := db.WaitForPing(ctx, pool, 30*time.Second); err != nil {
		log.Fatal(err)
	}
	if err := db.ApplyMigrations(ctx, pool, os.DirFS("internal/db/migrations")); err != nil {
		log.Fatal(err)
	}
	if err := db.MigrateRiver(ctx, pool); err != nil {
		log.Fatal(err)
	}

	sw, err := swarm.New(cfg.DockerHost)
	if err != nil {
		log.Fatal(err)
	}

	caAuthority, err := ca.New()
	if err != nil {
		log.Fatal(err)
	}

	key, err := os.ReadFile(cfg.MasterKeyFile)
	if err == nil {
		if _, err := secrets.NewStore(pool, []byte(strings.TrimSpace(string(key)))); err != nil {
			log.Printf("secrets store disabled: %v", err)
		}
	}

	fanout := db.NewFanout(pool)
	hub := realtime.NewHub()
	events := fanout.Subscribe("system", 100)
	go func() {
		for n := range events {
			hub.Broadcast(n)
		}
	}()
	go func() {
		if err := fanout.Run(ctx, []string{"system"}); err != nil {
			log.Printf("fanout stopped: %v", err)
		}
	}()

	notifier := notify.NewDispatcher(pool)
	buildClient := buildruntime.NewClient(cfg.BuildkitAddr)
	riverClient, err := riverjobs.NewClient(pool, cfg.RegistryAddr, sw, buildClient, notifier)
	if err != nil {
		log.Fatal(err)
	}
	if err := riverClient.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := riverClient.Stop(ctx); err != nil {
			log.Printf("river client stop: %v", err)
		}
	}()

	legacyBuildWorker := jobs.NewWorker(pool)
	legacyBuildWorker.Register("api", buildjobs.NewHandler(pool, cfg.RegistryAddr, sw, buildClient, notifier).Handle)
	legacyBuildWorker.Register("webhook", buildjobs.NewHandler(pool, cfg.RegistryAddr, sw, buildClient, notifier).Handle)
	legacyBuildWorker.Register("retry", buildjobs.NewHandler(pool, cfg.RegistryAddr, sw, buildClient, notifier).Handle)
	legacyBuildWorker.Register("rollback", buildjobs.NewHandler(pool, cfg.RegistryAddr, sw, buildClient, notifier).Handle)
	go func() {
		for ctx.Err() == nil {
			if err := legacyBuildWorker.Run(ctx); err != nil && ctx.Err() == nil {
				log.Printf("legacy build worker stopped: %v", err)
				time.Sleep(2 * time.Second)
			}
		}
	}()

	watcher := reconcile.NewWatcher(sw, fanout, pool)
	elector := leader.New(pool, func(lctx context.Context) {
		watcher.Run(lctx)
	}, func() {})
	go elector.Run(ctx)

	backupRunner := backupruntime.NewRunner(pool, notifier)
	go func() {
		if err := backupRunner.Run(ctx); err != nil {
			log.Printf("backup runner stopped: %v", err)
		}
	}()

	agentDialer, err := agentclient.NewDialer(caAuthority)
	if err != nil {
		log.Fatalf("agent dialer: %v", err)
	}

	updaterInstance := updater.New(sw)
	go updaterInstance.Run(ctx)

	server := &api.Server{
		Pool:           pool,
		Swarm:          sw,
		Authority:      caAuthority,
		Auth:           auth.NewService(pool, cfg.JWTSecret),
		Hub:            hub,
		AgentDialer:    agentDialer,
		RiverClient:    riverClient,
		BootstrapToken: cfg.AgentBootstrapKey,
		Initialized: func() bool {
			return initialized.Load()
		},
		Updater: updaterInstance,
	}
	initialized.Store(true)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: server.Router(),
	}

	go func() {
		log.Printf("control-plane listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	log.Printf("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
