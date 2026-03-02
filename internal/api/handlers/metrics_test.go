package handlers

import (
	"testing"
	"time"

	"github.com/lholliger/hive/internal/agent"
	"github.com/stretchr/testify/assert"
)

func TestMetricsCacheUpdateAndGet(t *testing.T) {
	cache := &metricsCache{latest: make(map[string]*agent.NodeMetricsReport)}

	report := &agent.NodeMetricsReport{
		NodeID:      "node-1",
		Hostname:    "host-1",
		Timestamp:   time.Now().Unix(),
		CPUCores:    4,
		CPUTotalPct: 42.5,
		MemTotal:    8 * 1024 * 1024 * 1024,
		MemUsed:     3 * 1024 * 1024 * 1024,
	}

	cache.Update(report)

	got := cache.Get("node-1")
	assert.NotNil(t, got)
	assert.Equal(t, "host-1", got.Hostname)
	assert.Equal(t, 42.5, got.CPUTotalPct)

	assert.Nil(t, cache.Get("nonexistent"))
}

func TestMetricsCacheGetAll(t *testing.T) {
	cache := &metricsCache{latest: make(map[string]*agent.NodeMetricsReport)}

	cache.Update(&agent.NodeMetricsReport{NodeID: "node-1", Timestamp: time.Now().Unix()})
	cache.Update(&agent.NodeMetricsReport{NodeID: "node-2", Timestamp: time.Now().Unix()})

	all := cache.GetAll()
	assert.Len(t, all, 2)
}

func TestMetricsCacheStaleNodes(t *testing.T) {
	cache := &metricsCache{latest: make(map[string]*agent.NodeMetricsReport)}

	cache.Update(&agent.NodeMetricsReport{NodeID: "fresh", Timestamp: time.Now().Unix()})
	cache.Update(&agent.NodeMetricsReport{NodeID: "stale", Timestamp: time.Now().Add(-5 * time.Minute).Unix()})

	stale := cache.StaleNodes(1 * time.Minute)
	assert.Len(t, stale, 1)
	assert.Equal(t, "stale", stale[0])
}
