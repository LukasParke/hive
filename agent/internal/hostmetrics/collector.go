package hostmetrics

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"

	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
)

// Collector gathers host-level metrics and caches them.
type Collector struct {
	hostRoot        string
	hostMgmtEnabled bool

	mu       sync.RWMutex
	cached   *agentv1.HostMetricsResponse
	pkgCache *agentv1.PackageStatusResponse
}

// NewCollector creates a new host metrics collector.
// hostRoot is the path to the host filesystem mount (e.g. "/host"), empty for native.
func NewCollector(hostRoot string, hostMgmt bool) *Collector {
	// Set gopsutil env vars for host filesystem access
	if hostRoot != "" {
		os.Setenv("HOST_PROC", hostRoot+"/proc")
		os.Setenv("HOST_SYS", hostRoot+"/sys")
		os.Setenv("HOST_ETC", hostRoot+"/etc")
		os.Setenv("HOST_VAR", hostRoot+"/var")
		os.Setenv("HOST_RUN", hostRoot+"/run")
	}
	return &Collector{
		hostRoot:        hostRoot,
		hostMgmtEnabled: hostMgmt,
	}
}

// Run starts the background collection loop. It collects metrics immediately
// and then every 15 seconds.
func (c *Collector) Run(ctx context.Context) {
	c.collect(ctx)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collect(ctx)
		}
	}
}

// Metrics returns the most recently cached host metrics snapshot.
func (c *Collector) Metrics() *agentv1.HostMetricsResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cached
}

// PackageStatus returns the cached package status info.
func (c *Collector) PackageStatus() *agentv1.PackageStatusResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pkgCache
}

// RefreshPackageStatus forces a re-check of package status.
func (c *Collector) RefreshPackageStatus(ctx context.Context) {
	status := collectPackageStatus(c.hostRoot)
	c.mu.Lock()
	c.pkgCache = status
	c.mu.Unlock()
}

// HostMgmtEnabled returns whether host management operations are enabled.
func (c *Collector) HostMgmtEnabled() bool {
	return c.hostMgmtEnabled
}

func (c *Collector) collect(ctx context.Context) {
	resp := &agentv1.HostMetricsResponse{
		CollectedAt: time.Now().Unix(),
	}

	// Host info
	if info, err := host.InfoWithContext(ctx); err == nil {
		resp.OsName = info.Platform + " " + info.PlatformVersion
		resp.OsVersion = info.PlatformVersion
		resp.KernelVersion = info.KernelVersion
		resp.Hostname = info.Hostname
		resp.UptimeSeconds = info.Uptime
	}

	// Load averages
	if avg, err := load.AvgWithContext(ctx); err == nil {
		resp.LoadAvg_1 = avg.Load1
		resp.LoadAvg_5 = avg.Load5
		resp.LoadAvg_15 = avg.Load15
	}

	// CPU per-core usage
	if percents, err := cpu.PercentWithContext(ctx, 0, true); err == nil {
		var total float64
		for i, p := range percents {
			resp.CpuCores = append(resp.CpuCores, &agentv1.CpuCoreUsage{
				Core:    int32(i),
				Percent: p,
			})
			total += p
		}
		if len(percents) > 0 {
			resp.CpuTotalPercent = total / float64(len(percents))
		}
	}

	// Memory
	if vmem, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		resp.MemoryTotal = vmem.Total
		resp.MemoryUsed = vmem.Used
		resp.MemoryAvailable = vmem.Available
		resp.MemoryCached = vmem.Cached
	}

	// Swap
	if swap, err := mem.SwapMemoryWithContext(ctx); err == nil {
		resp.SwapTotal = swap.Total
		resp.SwapUsed = swap.Used
	}

	// Filesystems
	if parts, err := disk.PartitionsWithContext(ctx, false); err == nil {
		for _, p := range parts {
			usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
			if err != nil {
				continue
			}
			resp.Filesystems = append(resp.Filesystems, &agentv1.FilesystemInfo{
				Device:         p.Device,
				MountPoint:     p.Mountpoint,
				FsType:         p.Fstype,
				TotalBytes:     usage.Total,
				UsedBytes:      usage.Used,
				AvailableBytes: usage.Free,
				UsagePercent:   usage.UsedPercent,
				InodesTotal:    usage.InodesTotal,
				InodesUsed:     usage.InodesUsed,
			})
		}
	}

	// Network interfaces
	if counters, err := net.IOCountersWithContext(ctx, true); err == nil {
		for _, ioc := range counters {
			resp.NetworkInterfaces = append(resp.NetworkInterfaces, &agentv1.NetworkInterfaceInfo{
				Name:        ioc.Name,
				BytesRecv:   ioc.BytesRecv,
				BytesSent:   ioc.BytesSent,
				PacketsRecv: ioc.PacketsRecv,
				PacketsSent: ioc.PacketsSent,
				ErrorsIn:    ioc.Errin,
				ErrorsOut:   ioc.Errout,
				DropsIn:     ioc.Dropin,
				DropsOut:    ioc.Dropout,
			})
		}
	}

	// Disk I/O
	if ioCounters, err := disk.IOCountersWithContext(ctx); err == nil {
		for name, d := range ioCounters {
			resp.DiskIo = append(resp.DiskIo, &agentv1.DiskIOInfo{
				Device:           name,
				ReadBytes:        d.ReadBytes,
				WriteBytes:       d.WriteBytes,
				ReadCount:        d.ReadCount,
				WriteCount:       d.WriteCount,
				IoTimeMs:         d.IoTime,
				WeightedIoTimeMs: d.WeightedIO,
			})
		}
	}

	c.mu.Lock()
	c.cached = resp
	c.mu.Unlock()
}
