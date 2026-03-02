package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/lholliger/hive/internal/agent"
	"github.com/lholliger/hive/internal/notify"
	"github.com/lholliger/hive/internal/store"
)

var HealthCache = &healthCache{
	latest: make(map[string]*agent.CephHealthReport),
}

type healthCache struct {
	mu     sync.RWMutex
	latest map[string]*agent.CephHealthReport
}

func (hc *healthCache) Update(report *agent.CephHealthReport) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.latest[report.FSID] = report
}

func (hc *healthCache) Get(fsid string) *agent.CephHealthReport {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.latest[fsid]
}

func (hc *healthCache) GetAll() []*agent.CephHealthReport {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	result := make([]*agent.CephHealthReport, 0, len(hc.latest))
	for _, r := range hc.latest {
		result = append(result, r)
	}
	return result
}

type HealthAggregator struct {
	nc    *nats.Conn
	store *store.Store
	log   *zap.SugaredLogger
}

func NewHealthAggregator(nc *nats.Conn, s *store.Store, log *zap.SugaredLogger) *HealthAggregator {
	return &HealthAggregator{nc: nc, store: s, log: log}
}

func (ha *HealthAggregator) Start(ctx context.Context) {
	if _, err := ha.nc.Subscribe("hive.ceph.health.>", func(msg *nats.Msg) {
		var report agent.CephHealthReport
		if err := json.Unmarshal(msg.Data, &report); err != nil {
			ha.log.Debugf("ceph health decode: %v", err)
			return
		}
		ha.processReport(ctx, &report)
	}); err != nil {
		ha.log.Errorf("failed to subscribe to ceph health: %v", err)
		return
	}

	ha.log.Info("ceph health aggregation started (NATS subscription on hive.ceph.health.>)")
}

func (ha *HealthAggregator) processReport(ctx context.Context, report *agent.CephHealthReport) {
	prev := HealthCache.Get(report.FSID)
	HealthCache.Update(report)

	if prev == nil || prev.Health == report.Health {
		return
	}

	ha.log.Infof("ceph cluster %s health changed: %s -> %s", report.FSID, prev.Health, report.Health)

	cluster, err := ha.store.GetCephClusterByFSID(ctx, report.FSID)
	if err != nil {
		return
	}

	newStatus := cephHealthToStatus(report.Health)
	if cluster.Status != newStatus && cluster.Status != "bootstrapping" && cluster.Status != "expanding" && cluster.Status != "destroying" {
		if err := ha.store.UpdateCephClusterStatus(ctx, cluster.ID, newStatus); err != nil {
			ha.log.Warnf("update ceph cluster status: %v", err)
		}
	}

	if report.Health != "HEALTH_OK" && ha.store != nil {
		d := notify.NewDispatcher(ha.store, ha.log)
		d.Send(ctx, notify.Event{
			Type:    "ceph.health_change",
			Title:   "Ceph Health: " + report.Health,
			Message: formatHealthMessage(report),
		})
	}
}

func cephHealthToStatus(health string) string {
	switch health {
	case "HEALTH_OK":
		return "healthy"
	case "HEALTH_WARN":
		return "degraded"
	case "HEALTH_ERR":
		return "error"
	default:
		return "degraded"
	}
}

func formatHealthMessage(report *agent.CephHealthReport) string {
	msg := "Cluster " + report.FSID + " is " + report.Health
	if len(report.HealthDetail) > 0 {
		msg += "\n"
		for _, detail := range report.HealthDetail {
			msg += "- " + detail + "\n"
		}
	}
	msg += "\n"
	msg += "OSDs: " + itoa(report.OSDUp) + "/" + itoa(report.OSDTotal) + " up, " +
		itoa(report.OSDIn) + "/" + itoa(report.OSDTotal) + " in\n"
	msg += "Monitors: " + itoa(report.MonCount) + " (" + itoa(len(report.MonQuorum)) + " in quorum)"
	return msg
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
