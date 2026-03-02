package alert

import (
	"context"
	"time"

	"github.com/lholliger/hive/internal/agent"
	"github.com/lholliger/hive/internal/store"
	"go.uber.org/zap"
)

type FiredAlert struct {
	ThresholdID string
	OrgID       string
	NodeID      string
	Metric      string
	Value       float64
	Threshold   float64
	Operator    string
}

type Evaluator struct {
	store *store.Store
	log   *zap.SugaredLogger
}

func NewEvaluator(s *store.Store, log *zap.SugaredLogger) *Evaluator {
	return &Evaluator{store: s, log: log}
}

func (e *Evaluator) Evaluate(ctx context.Context, reports []*agent.NodeMetricsReport) []FiredAlert {
	thresholds, err := e.store.ListAllAlertThresholds(ctx)
	if err != nil {
		e.log.Warnf("alert eval: load thresholds: %v", err)
		return nil
	}

	var fired []FiredAlert
	now := time.Now()

	for _, at := range thresholds {
		if !at.Enabled {
			continue
		}

		if at.LastFiredAt.Valid {
			cooldown := time.Duration(at.CooldownMinutes) * time.Minute
			if now.Sub(at.LastFiredAt.Time) < cooldown {
				continue
			}
		}

		for _, report := range reports {
			value, ok := extractMetric(report, at.Metric)
			if !ok {
				continue
			}
			if compare(value, at.Operator, at.Value) {
				fired = append(fired, FiredAlert{
					ThresholdID: at.ID,
					OrgID:       at.OrgID,
					NodeID:      report.NodeID,
					Metric:      at.Metric,
					Value:       value,
					Threshold:   at.Value,
					Operator:    at.Operator,
				})
			}
		}
	}

	return fired
}

func extractMetric(r *agent.NodeMetricsReport, metric string) (float64, bool) {
	switch metric {
	case "cpu_percent", "cpu_total_pct":
		return r.CPUTotalPct, true
	case "memory_percent":
		if r.MemTotal == 0 {
			return 0, false
		}
		return float64(r.MemUsed) / float64(r.MemTotal) * 100, true
	case "disk_percent":
		var totalUsed, totalCap uint64
		for _, d := range r.Disks {
			totalUsed += d.Used
			totalCap += d.Total
		}
		if totalCap == 0 {
			return 0, false
		}
		return float64(totalUsed) / float64(totalCap) * 100, true
	case "load_1m", "load_avg_1":
		return r.LoadAvg1, true
	case "load_5m", "load_avg_5":
		return r.LoadAvg5, true
	case "load_15m", "load_avg_15":
		return r.LoadAvg15, true
	case "temperature", "cpu_temp":
		return r.CPUTempCelsius, true
	case "swap_percent":
		if r.SwapTotal == 0 {
			return 0, false
		}
		return float64(r.SwapUsed) / float64(r.SwapTotal) * 100, true
	case "containers_running":
		return float64(r.ContainersRunning), true
	case "pending_updates":
		return float64(r.PendingUpdates), true
	default:
		return 0, false
	}
}

func compare(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return value > threshold
	}
}
