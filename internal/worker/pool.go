package worker

import (
	"context"

	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

type Pool struct {
	nc    *nats.Conn
	cfg   *config.Config
	log   *zap.SugaredLogger
	store *store.Store
}

func NewPool(nc *nats.Conn, cfg *config.Config, db *store.Store, log *zap.SugaredLogger) *Pool {
	return &Pool{nc: nc, cfg: cfg, store: db, log: log}
}

func (p *Pool) Start(ctx context.Context) {
	p.log.Info("starting worker pool")

	p.subscribe("hive.build", p.handleBuild)
	p.subscribe("hive.deploy", p.handleDeploy)
	p.subscribe("hive.backup", p.handleBackup)
	p.subscribe("hive.cleanup", p.handleCleanup)
	p.subscribe("hive.health", p.handleHealth)
	p.subscribe("hive.maintenance", p.handleMaintenance)

	p.log.Info("worker pool subscribed to all subjects")
}

func (p *Pool) subscribe(subject string, handler nats.MsgHandler) {
	_, err := p.nc.QueueSubscribe(subject, "hive-workers", handler)
	if err != nil {
		p.log.Errorf("failed to subscribe to %s: %v", subject, err)
	}
}
