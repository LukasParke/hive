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

type CephHealthReport struct {
	FSID         string         `json:"fsid"`
	NodeID       string         `json:"node_id"`
	Timestamp    int64          `json:"timestamp"`
	Health       string         `json:"health"`
	HealthDetail []string       `json:"health_detail"`
	MonCount     int            `json:"mon_count"`
	MonQuorum    []string       `json:"mon_quorum"`
	OSDTotal     int            `json:"osd_total"`
	OSDUp        int            `json:"osd_up"`
	OSDIn        int            `json:"osd_in"`
	PGCount      int            `json:"pg_count"`
	Pools        []CephPoolStat `json:"pools"`
	TotalBytes   uint64         `json:"total_bytes"`
	UsedBytes    uint64         `json:"used_bytes"`
	AvailBytes   uint64         `json:"avail_bytes"`
}

type CephPoolStat struct {
	Name      string `json:"name"`
	ID        int    `json:"id"`
	UsedBytes uint64 `json:"used_bytes"`
	MaxAvail  uint64 `json:"max_avail"`
	Objects   int    `json:"objects"`
}

type CephHealthReporter struct {
	nc       *nats.Conn
	nodeID   string
	log      *zap.SugaredLogger
	interval time.Duration
}

func NewCephHealthReporter(nc *nats.Conn, nodeID string, interval time.Duration, log *zap.SugaredLogger) *CephHealthReporter {
	if interval == 0 {
		interval = 30 * time.Second
	}
	return &CephHealthReporter{nc: nc, nodeID: nodeID, log: log, interval: interval}
}

func (chr *CephHealthReporter) Run(ctx context.Context) {
	if !chr.isCephNode() {
		chr.log.Debug("ceph health reporter: no ceph.conf found, skipping")
		return
	}

	chr.log.Info("ceph health reporter starting")
	chr.collectAndPublish(ctx)

	ticker := time.NewTicker(chr.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !chr.isCephNode() {
				continue
			}
			chr.collectAndPublish(ctx)
		}
	}
}

func (chr *CephHealthReporter) isCephNode() bool {
	_, err := os.Stat(cephConfPath)
	return err == nil
}

func (chr *CephHealthReporter) collectAndPublish(ctx context.Context) {
	out, err := runCephadmShell(ctx, 30*time.Second,
		"shell", "--", "ceph", "status", "-f", "json",
	)
	if err != nil {
		chr.log.Debugf("ceph health collection failed: %v", err)
		return
	}

	report, err := parseCephStatus(out, chr.nodeID)
	if err != nil {
		chr.log.Debugf("ceph health parse failed: %v", err)
		return
	}

	data, err := json.Marshal(report)
	if err != nil {
		return
	}

	subject := fmt.Sprintf("hive.ceph.health.%s", report.FSID)
	if err := chr.nc.Publish(subject, data); err != nil {
		chr.log.Warnf("ceph health publish: %v", err)
	}
}

func parseCephStatus(raw string, nodeID string) (*CephHealthReport, error) {
	var status cephStatusJSON
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return nil, fmt.Errorf("parse ceph status: %w", err)
	}

	report := &CephHealthReport{
		FSID:      status.FSID,
		NodeID:    nodeID,
		Timestamp: time.Now().Unix(),
		Health:    status.Health.Status,
		MonCount:  len(status.MonMap.Mons),
		OSDTotal:  status.OSDMap.OSDMap.NumOSDs,
		OSDUp:     status.OSDMap.OSDMap.NumUpOSDs,
		OSDIn:     status.OSDMap.OSDMap.NumInOSDs,
	}

	for _, name := range status.Quorum {
		report.MonQuorum = append(report.MonQuorum, fmt.Sprintf("%v", name))
	}

	for _, check := range status.Health.Checks {
		if check.Summary.Message != "" {
			report.HealthDetail = append(report.HealthDetail, check.Summary.Message)
		}
	}

	if status.PGMap.NumPGs > 0 {
		report.PGCount = status.PGMap.NumPGs
	}
	report.TotalBytes = status.PGMap.BytesTotal
	report.UsedBytes = status.PGMap.BytesUsed
	report.AvailBytes = status.PGMap.BytesAvail

	for _, pool := range status.PGMap.PoolStats {
		report.Pools = append(report.Pools, CephPoolStat{
			Name:      pool.Name,
			ID:        pool.ID,
			UsedBytes: pool.BytesUsed,
			MaxAvail:  pool.MaxAvail,
			Objects:   pool.Objects,
		})
	}

	return report, nil
}

type cephStatusJSON struct {
	FSID   string          `json:"fsid"`
	Health cephHealthJSON  `json:"health"`
	MonMap cephMonMapJSON  `json:"monmap"`
	OSDMap cephOSDMapJSON  `json:"osdmap"`
	PGMap  cephPGMapJSON   `json:"pgmap"`
	Quorum []interface{}   `json:"quorum_names"`
}

type cephHealthJSON struct {
	Status string                       `json:"status"`
	Checks map[string]cephHealthCheck   `json:"checks"`
}

type cephHealthCheck struct {
	Severity string               `json:"severity"`
	Summary  cephHealthSummary    `json:"summary"`
}

type cephHealthSummary struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}

type cephMonMapJSON struct {
	Mons []struct {
		Name string `json:"name"`
		Addr string `json:"addr"`
	} `json:"mons"`
}

type cephOSDMapJSON struct {
	OSDMap struct {
		NumOSDs   int `json:"num_osds"`
		NumUpOSDs int `json:"num_up_osds"`
		NumInOSDs int `json:"num_in_osds"`
	} `json:"osdmap"`
}

type cephPGMapJSON struct {
	NumPGs     int                `json:"num_pgs"`
	BytesTotal uint64             `json:"bytes_total"`
	BytesUsed  uint64             `json:"bytes_used"`
	BytesAvail uint64             `json:"bytes_avail"`
	PoolStats  []cephPoolStatJSON `json:"pool_stats,omitempty"`
}

type cephPoolStatJSON struct {
	Name      string `json:"name"`
	ID        int    `json:"id"`
	BytesUsed uint64 `json:"bytes_used"`
	MaxAvail  uint64 `json:"max_avail"`
	Objects   int    `json:"objects"`
}

