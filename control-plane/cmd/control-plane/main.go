package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/luke/hive/control-plane/internal/agentclient"
	"github.com/luke/hive/control-plane/internal/api"
	"github.com/luke/hive/control-plane/internal/auth"
	backupruntime "github.com/luke/hive/control-plane/internal/backup"
	"github.com/luke/hive/control-plane/internal/bootstrap"
	buildruntime "github.com/luke/hive/control-plane/internal/build"
	"github.com/luke/hive/control-plane/internal/ca"
	"github.com/luke/hive/control-plane/internal/config"
	"github.com/luke/hive/control-plane/internal/db"
	"github.com/luke/hive/control-plane/internal/jobs/riverjobs"
	"github.com/luke/hive/control-plane/internal/leader"
	"github.com/luke/hive/control-plane/internal/notify"
	"github.com/luke/hive/control-plane/internal/realtime"
	"github.com/luke/hive/control-plane/internal/reconcile"
	"github.com/luke/hive/control-plane/internal/secrets"
	"github.com/luke/hive/control-plane/internal/swarm"
	"github.com/luke/hive/control-plane/internal/updater"
	"github.com/riverqueue/river/rivertype"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx := context.Background()
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	var initialized atomic.Bool

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.WaitForPing(ctx, pool, 30*time.Second); err != nil {
		return err
	}
	// First-boot initialization with race protection: exactly one replica
	// runs migrations and seeds settings while peers wait for the settings
	// record (plan P7.3). The self-register admin flow is kept instead of
	// the plan's generated admin — documented deviation.
	if err := bootstrap.Run(ctx, bootstrap.NewLockClient(pool), func(mctx context.Context) error {
		if err := db.ApplyMigrations(mctx, pool, os.DirFS("internal/db/migrations")); err != nil {
			return err
		}
		return db.MigrateRiver(mctx, pool)
	}, bootstrap.Options{}); err != nil {
		return err
	}

	sw, err := swarm.New(cfg.DockerHost)
	if err != nil {
		return err
	}

	// Construct the secrets store once and install it as the process-wide
	// runtime. Without a master key file the store stays nil (dev flows);
	// the CA then runs ephemeral instead of persisted.
	key, err := os.ReadFile(cfg.MasterKeyFile)
	if err != nil {
		log.Printf("secrets store disabled: %v", err)
	} else if store, err := secrets.NewStore(pool, []byte(strings.TrimSpace(string(key)))); err != nil {
		log.Printf("secrets store disabled: %v", err)
	} else {
		secrets.SetRuntime(store)
	}

	caAuthority, err := ca.LoadOrCreate(ctx, secrets.Runtime())
	if err != nil {
		return err
	}

	// Publish the CA certificate as a Swarm config so agents can mount it
	// for mTLS verification. Best-effort: dev flows without a Swarm cluster
	// must not crash here.
	if err := sw.EnsureConfig(ctx, "hive-agent-ca", caAuthority.CertPEM()); err != nil {
		log.Printf("warning: could not publish hive-agent-ca swarm config: %v", err)
	}

	fanout := db.NewFanoutWithListenURL(pool, cfg.ListenDatabaseURL)
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
	riverClient, err := riverjobs.NewClient(pool, cfg.RegistryAddr, sw, buildClient, notifier,
		riverjobs.WithCertRenewer(&agentclient.ControlPlaneCertRenewer{Authority: caAuthority, Store: secrets.Runtime()}))
	if err != nil {
		return err
	}
	if err := riverClient.Start(ctx); err != nil {
		return err
	}
	defer func() {
		if err := riverClient.Stop(ctx); err != nil {
			log.Printf("river client stop: %v", err)
		}
	}()

	watcher := reconcile.NewWatcher(sw, fanout, pool)

	// Leader-only work: the swarm event watcher singleton and River periodic
	// job scheduling. Everything else (fanout, river workers, backup runner,
	// updater) keeps running on every replica.
	var (
		periodicMu      sync.Mutex
		periodicHandles []rivertype.PeriodicJobHandle
	)
	elector := leader.New(pool,
		func(lctx context.Context) {
			go func() {
				if err := watcher.Run(lctx); err != nil {
					log.Printf("swarm watcher exited: %v", err)
				}
			}()
			periodicMu.Lock()
			defer periodicMu.Unlock()
			periodicHandles = riverjobs.StartPeriodicJobs(riverClient)
		},
		func() {
			periodicMu.Lock()
			defer periodicMu.Unlock()
			// Remove handles so re-acquisition cannot duplicate schedules.
			riverClient.PeriodicJobs().RemoveMany(periodicHandles)
			periodicHandles = nil
		},
	)
	go elector.Run(ctx)

	backupRunner := backupruntime.NewRunner(pool, notifier)
	go func() {
		if err := backupRunner.Run(ctx); err != nil {
			log.Printf("backup runner stopped: %v", err)
		}
	}()

	cpCert, err := agentclient.LoadOrCreateClientCert(ctx, caAuthority, secrets.Runtime())
	if err != nil {
		return fmt.Errorf("control-plane client certificate: %w", err)
	}
	agentDialer, err := agentclient.NewDialer(caAuthority, cpCert, cfg.AgentMTLSEnabled)
	if err != nil {
		return fmt.Errorf("agent dialer: %w", err)
	}

	updaterInstance := updater.New(sw)
	go updaterInstance.Run(ctx)

	server := &api.Server{
		Pool:                   pool,
		Swarm:                  sw,
		Authority:              caAuthority,
		Auth:                   auth.NewService(pool, cfg.JWTSecret),
		Hub:                    hub,
		AgentDialer:            agentDialer,
		RiverClient:            riverClient,
		BootstrapToken:         cfg.AgentBootstrapKey,
		Initialized:            initialized.Load,
		Updater:                updaterInstance,
		Elector:                elector,
		AuthRateLimitPerMin:    cfg.AuthRateLimitPerMin,
		WebhookRateLimitPerMin: cfg.WebhookRateLimitPerMin,
	}
	initialized.Store(true)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
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

	return nil
}
