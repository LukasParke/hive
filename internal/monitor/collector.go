package monitor

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/client"

	"go.uber.org/zap"
)

type NodeMetrics struct {
	NodeID     string  `json:"node_id"`
	Hostname   string  `json:"hostname"`
	CPUPercent float64 `json:"cpu_percent"`
	MemUsed    uint64  `json:"mem_used"`
	MemTotal   uint64  `json:"mem_total"`
	DiskUsed   uint64  `json:"disk_used"`
	DiskTotal  uint64  `json:"disk_total"`
	Containers int     `json:"containers"`
	Services   int     `json:"services"`
	Timestamp  int64   `json:"timestamp"`
}

type Collector struct {
	docker *client.Client
	log    *zap.SugaredLogger
}

func NewCollector(log *zap.SugaredLogger) (*Collector, error) {
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	if log == nil {
		l, _ := zap.NewNop().Sugar(), error(nil)
		log = l
	}
	return &Collector{docker: docker, log: log}, nil
}

func (c *Collector) CollectOnce(ctx context.Context) (*NodeMetrics, error) {
	info, err := c.docker.Info(ctx)
	if err != nil {
		return nil, err
	}

	metrics := &NodeMetrics{
		NodeID:     info.Swarm.NodeID,
		Hostname:   info.Name,
		MemTotal:   uint64(info.MemTotal),
		Containers: info.ContainersRunning,
		Timestamp:  time.Now().Unix(),
	}

	metrics.CPUPercent = readCPUPercent()

	memInfo := readMemInfo()
	if total, ok := memInfo["MemTotal"]; ok {
		metrics.MemTotal = total * 1024
	}
	if avail, ok := memInfo["MemAvailable"]; ok {
		metrics.MemUsed = metrics.MemTotal - (avail * 1024)
	}

	diskUsed, diskTotal := readDiskUsage()
	metrics.DiskUsed = diskUsed
	metrics.DiskTotal = diskTotal

	return metrics, nil
}

func (c *Collector) CollectLoop(ctx context.Context, interval time.Duration, out chan<- []byte) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			metrics, err := c.CollectOnce(ctx)
			if err != nil {
				c.log.Warnf("collect metrics: %v", err)
				continue
			}

			data, _ := json.Marshal(metrics)
			select {
			case out <- data:
			default:
			}
		}
	}
}

func readMemInfo() map[string]uint64 {
	result := make(map[string]uint64)
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, err := strconv.ParseUint(parts[1], 10, 64)
		if err == nil {
			result[key] = val
		}
	}
	return result
}

func readCPUPercent() float64 {
	data1, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	idle1, total1 := parseCPUStat(string(data1))

	time.Sleep(200 * time.Millisecond)

	data2, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	idle2, total2 := parseCPUStat(string(data2))

	idleDelta := float64(idle2 - idle1)
	totalDelta := float64(total2 - total1)
	if totalDelta == 0 {
		return 0
	}
	return (1.0 - idleDelta/totalDelta) * 100
}

func parseCPUStat(data string) (idle, total uint64) {
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return
			}
			for i := 1; i < len(fields); i++ {
				val, _ := strconv.ParseUint(fields[i], 10, 64)
				total += val
				if i == 4 {
					idle = val
				}
			}
			return
		}
	}
	return
}

func readDiskUsage() (used, total uint64) {
	var stat syscallStatfs
	if err := statfs("/", &stat); err != nil {
		return 0, 0
	}
	total = stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used = total - free
	return
}
