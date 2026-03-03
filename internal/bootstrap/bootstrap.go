package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

type Bootstrapper struct {
	cfg        *config.Config
	log        *zap.SugaredLogger
	swarm      *swarm.Client
	pgSecretID string
}

func New(cfg *config.Config, log *zap.SugaredLogger) *Bootstrapper {
	return &Bootstrapper{cfg: cfg, log: log}
}

// runPhase executes a named bootstrap step with structured timing logs.
func (b *Bootstrapper) runPhase(name string, fn func() error) error {
	b.log.Infow("bootstrap phase starting", "phase", name)
	start := time.Now()
	if err := fn(); err != nil {
		b.log.Errorw("bootstrap phase failed", "phase", name, "error", err, "elapsed_ms", time.Since(start).Milliseconds())
		return fmt.Errorf("%s: %w", name, err)
	}
	b.log.Infow("bootstrap phase complete", "phase", name, "elapsed_ms", time.Since(start).Milliseconds())
	return nil
}

// RunLauncher executes the launcher bootstrap: init swarm, create network,
// deploy Postgres and the hive-manager service, then wait for the service
// to become healthy and exit.
func (b *Bootstrapper) RunLauncher(ctx context.Context) error {
	b.log.Info("starting launcher bootstrap sequence")

	if err := b.runPhase("swarm_client", func() error {
		sc, err := swarm.NewClient(b.log)
		if err != nil {
			return err
		}
		b.swarm = sc
		return nil
	}); err != nil {
		return err
	}

	if err := b.runPhase("ensure_swarm", func() error { return b.swarm.EnsureSwarm(ctx) }); err != nil {
		return err
	}

	if err := b.runPhase("ensure_network", func() error { return b.ensureNetwork(ctx) }); err != nil {
		return err
	}

	if err := b.runPhase("ensure_postgres_secret", func() error { return b.ensurePostgresSecret(ctx) }); err != nil {
		return err
	}

	if err := b.runPhase("ensure_postgres", func() error { return b.ensurePostgres(ctx) }); err != nil {
		return err
	}

	if err := b.runPhase("ensure_manager", func() error { return b.ensureManager(ctx) }); err != nil {
		return err
	}

	if err := b.runPhase("wait_for_manager", func() error { return b.waitForManager(ctx) }); err != nil {
		return err
	}

	b.log.Info("hive is deployed and running as a Swarm service")
	return nil
}

// RunService executes the service bootstrap: wait for Postgres, run
// migrations, deploy Traefik/registry/agent. Called when running as the
// Swarm-managed hive-manager service (HIVE_MANAGED=true).
func (b *Bootstrapper) RunService(ctx context.Context) error {
	b.log.Info("starting service bootstrap sequence")

	if err := b.runPhase("swarm_client", func() error {
		sc, err := swarm.NewClient(b.log)
		if err != nil {
			return err
		}
		b.swarm = sc
		return nil
	}); err != nil {
		return err
	}

	if err := b.runPhase("wait_for_postgres", func() error { return b.waitForPostgres(ctx) }); err != nil {
		return err
	}

	if err := b.runPhase("run_migrations", func() error { return b.runMigrations() }); err != nil {
		return err
	}

	if err := b.runPhase("ensure_traefik", func() error { return b.ensureTraefik(ctx) }); err != nil {
		return err
	}

	multiNode, err := b.swarm.IsMultiNode(ctx)
	if err != nil {
		b.log.Warnf("could not determine node count: %v", err)
	}
	b.cfg.MultiNode = multiNode

	if multiNode {
		if err := b.runPhase("ensure_registry", func() error { return b.ensureRegistry(ctx) }); err != nil {
			return err
		}
	}

	if err := b.ensureAgent(ctx); err != nil {
		b.log.Warnf("ensure agent: %v", err)
	}

	b.log.Info("service bootstrap sequence complete")
	return nil
}

// Run is the legacy bootstrap that does everything in a single pass.
// Kept for dev mode where the launcher/service split isn't needed.
func (b *Bootstrapper) Run(ctx context.Context) error {
	b.log.Info("starting bootstrap sequence")

	sc, err := swarm.NewClient(b.log)
	if err != nil {
		return fmt.Errorf("swarm client: %w", err)
	}
	b.swarm = sc

	if err := b.swarm.EnsureSwarm(ctx); err != nil {
		return fmt.Errorf("ensure swarm: %w", err)
	}

	if err := b.ensureNetwork(ctx); err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}

	if err := b.ensurePostgres(ctx); err != nil {
		return fmt.Errorf("ensure postgres: %w", err)
	}

	if err := b.waitForPostgres(ctx); err != nil {
		return fmt.Errorf("wait for postgres: %w", err)
	}

	if err := b.runMigrations(); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	if err := b.ensureTraefik(ctx); err != nil {
		return fmt.Errorf("ensure traefik: %w", err)
	}

	multiNode, err := b.swarm.IsMultiNode(ctx)
	if err != nil {
		b.log.Warnf("could not determine node count: %v", err)
	}
	b.cfg.MultiNode = multiNode

	if multiNode {
		if err := b.ensureRegistry(ctx); err != nil {
			return fmt.Errorf("ensure registry: %w", err)
		}
	}

	if err := b.ensureAgent(ctx); err != nil {
		b.log.Warnf("ensure agent: %v", err)
	}

	b.log.Info("bootstrap sequence complete")
	return nil
}

func (b *Bootstrapper) waitForManager(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", b.cfg.APIPort)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("hive-manager did not become healthy within timeout")
		case <-ticker.C:
			resp, err := http.Get(healthURL)
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}
