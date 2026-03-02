package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/agent"
)

// MetricsCache holds the latest metrics for all nodes in memory.
var MetricsCache = &metricsCache{
	latest: make(map[string]*agent.NodeMetricsReport),
}

type metricsCache struct {
	mu     sync.RWMutex
	latest map[string]*agent.NodeMetricsReport
}

func (mc *metricsCache) Update(report *agent.NodeMetricsReport) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.latest[report.NodeID] = report
}

func (mc *metricsCache) GetAll() []*agent.NodeMetricsReport {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	result := make([]*agent.NodeMetricsReport, 0, len(mc.latest))
	for _, r := range mc.latest {
		result = append(result, r)
	}
	return result
}

func (mc *metricsCache) Get(nodeID string) *agent.NodeMetricsReport {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.latest[nodeID]
}

func (mc *metricsCache) StaleNodes(threshold time.Duration) []string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	cutoff := time.Now().Add(-threshold).Unix()
	var stale []string
	for id, r := range mc.latest {
		if r.Timestamp < cutoff {
			stale = append(stale, id)
		}
	}
	return stale
}

func ClusterMetrics(w http.ResponseWriter, r *http.Request) {
	reports := MetricsCache.GetAll()
	if reports == nil {
		reports = []*agent.NodeMetricsReport{}
	}
	writeJSON(w, http.StatusOK, reports)
}

func NodeMetricsLatest(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeId")

	latest := MetricsCache.Get(nodeID)
	if latest == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no metrics for node"})
		return
	}

	s := storeFromRequest(r)
	since := time.Now().Add(-24 * time.Hour)
	history, _ := s.GetNodeMetricsHistory(r.Context(), nodeID, since)

	var historyReports []agent.NodeMetricsReport
	for _, snap := range history {
		var report agent.NodeMetricsReport
		if err := json.Unmarshal(snap.Metrics, &report); err == nil {
			historyReports = append(historyReports, report)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"latest":  latest,
		"history": historyReports,
	})
}

func NodeMetricsHistory(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeId")
	rangeParam := r.URL.Query().Get("range")

	var since time.Time
	switch rangeParam {
	case "1h":
		since = time.Now().Add(-1 * time.Hour)
	case "6h":
		since = time.Now().Add(-6 * time.Hour)
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	default:
		since = time.Now().Add(-24 * time.Hour)
	}

	s := storeFromRequest(r)
	snaps, err := s.GetNodeMetricsHistory(r.Context(), nodeID, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var reports []agent.NodeMetricsReport
	for _, snap := range snaps {
		var report agent.NodeMetricsReport
		if err := json.Unmarshal(snap.Metrics, &report); err == nil {
			reports = append(reports, report)
		}
	}
	if reports == nil {
		reports = []agent.NodeMetricsReport{}
	}

	writeJSON(w, http.StatusOK, reports)
}

// MetricsServices remains the existing endpoint for Swarm service health
func MetricsServices(w http.ResponseWriter, r *http.Request) {
	storeFromRequest(r)
	writeJSON(w, http.StatusOK, []struct{}{})
}

// MetricsNodes returns the cluster-wide metrics from the agent cache.
// Replaces the old single-node collector.
func MetricsNodes(w http.ResponseWriter, r *http.Request) {
	reports := MetricsCache.GetAll()
	if reports == nil {
		reports = []*agent.NodeMetricsReport{}
	}
	writeJSON(w, http.StatusOK, reports)
}

