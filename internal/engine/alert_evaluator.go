package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lholliger/hive/internal/notify"
	"github.com/lholliger/hive/internal/store"
	"go.uber.org/zap"
)

func StartAlertEvaluator(ctx context.Context, db *store.Store, log *zap.SugaredLogger, intervalSeconds int) {
	if intervalSeconds <= 0 {
		intervalSeconds = 60
	}
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			evaluateAlerts(ctx, db, log)
		}
	}
}

func EvaluateAlertsOnce(ctx context.Context, db *store.Store, log *zap.SugaredLogger) {
	evaluateAlerts(ctx, db, log)
}

func evaluateAlerts(ctx context.Context, db *store.Store, log *zap.SugaredLogger) {
	thresholds, err := db.ListAllAlertThresholds(ctx)
	if err != nil {
		log.Warnf("alert evaluator: list thresholds: %v", err)
		return
	}
	if len(thresholds) == 0 {
		return
	}

	snapshots, err := db.GetLatestMetricsSnapshots(ctx)
	if err != nil {
		log.Warnf("alert evaluator: get metrics: %v", err)
		return
	}

	type nodeMetrics struct {
		CPUPercent    float64 `json:"cpu_percent"`
		MemoryPercent float64 `json:"memory_percent"`
		DiskPercent   float64 `json:"disk_percent"`
		Hostname      string  `json:"hostname"`
	}

	var nodes []nodeMetrics
	for _, snap := range snapshots {
		var nm nodeMetrics
		if err := json.Unmarshal(snap.Metrics, &nm); err != nil {
			continue
		}
		nodes = append(nodes, nm)
	}

	dispatcher := notify.NewDispatcher(db, log)

	for _, t := range thresholds {
		if !t.Enabled {
			continue
		}

		if t.LastFiredAt.Valid && t.CooldownMinutes > 0 {
			if time.Since(t.LastFiredAt.Time) < time.Duration(t.CooldownMinutes)*time.Minute {
				continue
			}
		}

		for _, node := range nodes {
			var metricValue float64
			switch t.Metric {
			case "cpu":
				metricValue = node.CPUPercent
			case "memory":
				metricValue = node.MemoryPercent
			case "disk":
				metricValue = node.DiskPercent
			default:
				continue
			}

			fired := false
			switch t.Operator {
			case ">", "gt":
				fired = metricValue > t.Value
			case ">=", "gte":
				fired = metricValue >= t.Value
			case "<", "lt":
				fired = metricValue < t.Value
			case "<=", "lte":
				fired = metricValue <= t.Value
			case "==", "eq":
				fired = metricValue == t.Value
			}

			if fired {
				_ = db.UpdateAlertThresholdFired(ctx, t.ID)
				hostname := node.Hostname
				if hostname == "" {
					hostname = "unknown"
				}
				dispatcher.Send(ctx, notify.Event{
					Type:    "alert.threshold",
					Title:   fmt.Sprintf("Alert: %s threshold exceeded on %s", t.Metric, hostname),
					Message: fmt.Sprintf("%s is %.1f%% (threshold: %s %.0f%%) on node %s", t.Metric, metricValue, t.Operator, t.Value, hostname),
					OrgID:   t.OrgID,
				})
				log.Infof("alert fired: %s %s %.1f on %s (value: %.1f)", t.Metric, t.Operator, t.Value, hostname, metricValue)
				break
			}
		}
	}
}
