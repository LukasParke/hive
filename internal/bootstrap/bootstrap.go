package bootstrap

import (
	"context"
	"fmt"

	"github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

type Bootstrapper struct {
	cfg    *config.Config
	log    *zap.SugaredLogger
	swarm  *swarm.Client
}

func New(cfg *config.Config, log *zap.SugaredLogger) *Bootstrapper {
	return &Bootstrapper{cfg: cfg, log: log}
}

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
