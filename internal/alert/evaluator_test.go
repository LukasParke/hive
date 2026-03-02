package alert

import (
	"testing"

	"github.com/lholliger/hive/internal/agent"
	"github.com/stretchr/testify/assert"
)

func TestExtractMetric(t *testing.T) {
	report := &agent.NodeMetricsReport{
		CPUTotalPct:    75.5,
		MemTotal:       16000000000,
		MemUsed:        12000000000,
		LoadAvg1:       2.5,
		LoadAvg5:       1.8,
		LoadAvg15:      1.2,
		CPUTempCelsius: 68.0,
		SwapTotal:      4000000000,
		SwapUsed:       1000000000,
		Disks: []agent.DiskMetrics{
			{Total: 500000000000, Used: 250000000000},
			{Total: 1000000000000, Used: 400000000000},
		},
		ContainersRunning: 12,
		PendingUpdates:    5,
	}

	tests := []struct {
		metric   string
		expected float64
		ok       bool
	}{
		{"cpu_percent", 75.5, true},
		{"cpu_total_pct", 75.5, true},
		{"memory_percent", 75.0, true},
		{"disk_percent", float64(650000000000) / float64(1500000000000) * 100, true},
		{"load_1m", 2.5, true},
		{"load_5m", 1.8, true},
		{"load_15m", 1.2, true},
		{"temperature", 68.0, true},
		{"swap_percent", 25.0, true},
		{"containers_running", 12.0, true},
		{"pending_updates", 5.0, true},
		{"unknown_metric", 0, false},
	}

	for _, tt := range tests {
		val, ok := extractMetric(report, tt.metric)
		assert.Equal(t, tt.ok, ok, "metric: %s", tt.metric)
		if ok {
			assert.InDelta(t, tt.expected, val, 0.01, "metric: %s", tt.metric)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		value     float64
		operator  string
		threshold float64
		expected  bool
	}{
		{80, ">", 70, true},
		{70, ">", 70, false},
		{60, ">", 70, false},
		{70, ">=", 70, true},
		{60, "<", 70, true},
		{70, "<", 70, false},
		{70, "<=", 70, true},
		{70, "==", 70, true},
		{71, "==", 70, false},
		{71, "!=", 70, true},
		{70, "!=", 70, false},
	}

	for _, tt := range tests {
		result := compare(tt.value, tt.operator, tt.threshold)
		assert.Equal(t, tt.expected, result, "%.1f %s %.1f", tt.value, tt.operator, tt.threshold)
	}
}
