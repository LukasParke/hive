package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

type Agent struct {
	nc       *nats.Conn
	log      *zap.SugaredLogger
	nodeID   string
	interval time.Duration
}

func New(nc *nats.Conn, nodeID string, interval time.Duration, log *zap.SugaredLogger) *Agent {
	if nodeID == "" {
		nodeID, _ = os.Hostname()
	}
	return &Agent{
		nc:       nc,
		log:      log,
		nodeID:   nodeID,
		interval: interval,
	}
}

func (a *Agent) Run(ctx context.Context) {
	a.log.Infof("hive-agent starting, node=%s interval=%s", a.nodeID, a.interval)

	// Collect immediately on start
	a.collectAndPublish()

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.log.Info("hive-agent shutting down")
			return
		case <-ticker.C:
			a.collectAndPublish()
		}
	}
}

func (a *Agent) collectAndPublish() {
	report := Collect(a.nodeID)

	data, err := json.Marshal(report)
	if err != nil {
		a.log.Warnf("agent: marshal metrics: %v", err)
		return
	}

	subject := fmt.Sprintf("hive.metrics.%s", a.nodeID)
	if err := a.nc.Publish(subject, data); err != nil {
		a.log.Warnf("agent: publish metrics: %v", err)
		return
	}

	a.log.Debugf("published metrics: cpu=%.1f%% mem=%d/%d disks=%d ifaces=%d",
		report.CPUTotalPct, report.MemUsed, report.MemTotal, len(report.Disks), len(report.Interfaces))
}

// Collect gathers all metrics for this node. Exported for testing.
func Collect(nodeID string) *NodeMetricsReport {
	report := &NodeMetricsReport{
		NodeID:    nodeID,
		Timestamp: time.Now().Unix(),
	}

	collectCPU(report)
	collectMemory(report)
	collectDisk(report)
	collectNetwork(report)
	collectSystem(report)
	collectDocker(report)
	collectGPU(report)
	collectBlockDevices(report)

	return report
}
