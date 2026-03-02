package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCollectProducesReport(t *testing.T) {
	report := Collect("test-node")
	assert.NotNil(t, report)
	assert.Equal(t, "test-node", report.NodeID)
	assert.Greater(t, report.Timestamp, int64(0))
	assert.Greater(t, report.CPUCores, 0)
	assert.Greater(t, report.MemTotal, uint64(0))
	assert.NotEmpty(t, report.Hostname)
}

func TestCollectCPU(t *testing.T) {
	report := &NodeMetricsReport{}
	collectCPU(report)
	assert.Greater(t, report.CPUCores, 0)
	assert.GreaterOrEqual(t, report.CPUTotalPct, float64(0))
}

func TestCollectMemory(t *testing.T) {
	report := &NodeMetricsReport{}
	collectMemory(report)
	assert.Greater(t, report.MemTotal, uint64(0))
}

func TestCollectDisk(t *testing.T) {
	report := &NodeMetricsReport{}
	collectDisk(report)
	assert.Greater(t, len(report.Disks), 0, "should find at least one disk")
}

func TestCollectNetwork(t *testing.T) {
	report := &NodeMetricsReport{}
	collectNetwork(report)
	// May or may not find non-virtual interfaces depending on environment
	assert.NotNil(t, report.Interfaces)
}

func TestCollectSystem(t *testing.T) {
	report := &NodeMetricsReport{}
	collectSystem(report)
	assert.NotEmpty(t, report.Hostname)
	assert.Greater(t, report.Uptime, uint64(0))
}

func TestIsVirtualInterface(t *testing.T) {
	assert.True(t, isVirtualInterface("lo"))
	assert.True(t, isVirtualInterface("docker0"))
	assert.True(t, isVirtualInterface("veth1234abc"))
	assert.True(t, isVirtualInterface("br-abcdef"))
	assert.False(t, isVirtualInterface("eth0"))
	assert.False(t, isVirtualInterface("enp3s0"))
	assert.False(t, isVirtualInterface("wlan0"))
}

func TestIsPhysicalFS(t *testing.T) {
	assert.True(t, isPhysicalFS("ext4"))
	assert.True(t, isPhysicalFS("btrfs"))
	assert.True(t, isPhysicalFS("zfs"))
	assert.True(t, isPhysicalFS("nfs"))
	assert.True(t, isPhysicalFS("ceph"))
	assert.False(t, isPhysicalFS("tmpfs"))
	assert.False(t, isPhysicalFS("sysfs"))
	assert.False(t, isPhysicalFS("proc"))
}
