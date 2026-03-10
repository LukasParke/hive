package agent

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// Agent now only maintains a NATS connection for Ceph operations.
// System metrics are collected by Prometheus node-exporter and cAdvisor.
type Agent struct {
	nc     *nats.Conn
	log    *zap.SugaredLogger
	nodeID string
}

func New(nc *nats.Conn, nodeID string, log *zap.SugaredLogger) *Agent {
	return &Agent{
		nc:     nc,
		log:    log,
		nodeID: nodeID,
	}
}

func (a *Agent) Run(ctx context.Context) {
	a.log.Infof("hive-agent ready, node=%s (metrics via Prometheus)", a.nodeID)
	<-ctx.Done()
	a.log.Info("hive-agent shutting down")
}
