package server

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
)

// Metrics holds all Prometheus metrics for the agent.
type Metrics struct {
	// RPC metrics
	ExecStreamTotal      prometheus.Counter
	ExecStreamActive     prometheus.Gauge
	LogStreamTotal       prometheus.Counter
	LogStreamActive      prometheus.Gauge
	StatsRequestTotal    prometheus.Counter
	StatsRequestDuration prometheus.Histogram
	HealthCheckTotal     prometheus.Counter

	// Cert metrics
	CertExpiryTimestamp prometheus.Gauge
	CertRenewalTotal    *prometheus.CounterVec

	// Docker API errors
	DockerAPIErrors *prometheus.CounterVec

	// Host exec metrics
	HostExecTotal *prometheus.CounterVec

	// Host node metrics (updated by collector)
	NodeCPUUsage       *prometheus.GaugeVec
	NodeLoadAverage    *prometheus.GaugeVec
	NodeMemoryTotal    prometheus.Gauge
	NodeMemoryUsed     prometheus.Gauge
	NodeMemoryAvail    prometheus.Gauge
	NodeSwapTotal      prometheus.Gauge
	NodeSwapUsed       prometheus.Gauge
	NodeFSTotal        *prometheus.GaugeVec
	NodeFSUsed         *prometheus.GaugeVec
	NodeFSUsagePercent *prometheus.GaugeVec
	NodeDiskReadBytes  *prometheus.CounterVec
	NodeDiskWriteBytes *prometheus.CounterVec
	NodeNetRxBytes     *prometheus.CounterVec
	NodeNetTxBytes     *prometheus.CounterVec
	NodeNetRxErrors    *prometheus.CounterVec
	NodeNetTxErrors    *prometheus.CounterVec
	NodeUptime         prometheus.Gauge
	NodePkgsUpgradable prometheus.Gauge
	NodePkgsSecurity   prometheus.Gauge
	NodeRebootRequired prometheus.Gauge
}

// NewMetrics creates and registers all Prometheus metrics with the default registry.
func NewMetrics() *Metrics {
	return NewMetricsWithRegistry(prometheus.DefaultRegisterer)
}

// NewMetricsWithRegistry creates and registers all Prometheus metrics with the given registry.
func NewMetricsWithRegistry(reg prometheus.Registerer) *Metrics {
	factory := promauto.With(reg)
	return &Metrics{
		ExecStreamTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "hive_agent_exec_stream_total",
			Help: "Total number of exec streams opened",
		}),
		ExecStreamActive: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_agent_exec_stream_active",
			Help: "Number of currently active exec streams",
		}),
		LogStreamTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "hive_agent_log_stream_total",
			Help: "Total number of log streams opened",
		}),
		LogStreamActive: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_agent_log_stream_active",
			Help: "Number of currently active log streams",
		}),
		StatsRequestTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "hive_agent_stats_request_total",
			Help: "Total number of stats requests",
		}),
		StatsRequestDuration: factory.NewHistogram(prometheus.HistogramOpts{
			Name:    "hive_agent_stats_request_duration_seconds",
			Help:    "Duration of stats requests",
			Buckets: prometheus.DefBuckets,
		}),
		HealthCheckTotal: factory.NewCounter(prometheus.CounterOpts{
			Name: "hive_agent_health_check_total",
			Help: "Total number of health checks",
		}),
		CertExpiryTimestamp: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_agent_cert_expiry_timestamp_seconds",
			Help: "Unix timestamp when the agent certificate expires",
		}),
		CertRenewalTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "hive_agent_cert_renewal_total",
			Help: "Total certificate renewal attempts",
		}, []string{"result"}),
		DockerAPIErrors: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "hive_agent_docker_api_errors_total",
			Help: "Total Docker API errors",
		}, []string{"operation"}),
		HostExecTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "hive_agent_host_exec_total",
			Help: "Total host exec operations",
		}, []string{"operation", "result"}),
		NodeCPUUsage: factory.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hive_node_cpu_usage_percent",
			Help: "CPU usage percent per core",
		}, []string{"core"}),
		NodeLoadAverage: factory.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hive_node_load_average",
			Help: "System load average",
		}, []string{"period"}),
		NodeMemoryTotal: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_node_memory_total_bytes",
			Help: "Total memory in bytes",
		}),
		NodeMemoryUsed: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_node_memory_used_bytes",
			Help: "Used memory in bytes",
		}),
		NodeMemoryAvail: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_node_memory_available_bytes",
			Help: "Available memory in bytes",
		}),
		NodeSwapTotal: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_node_swap_total_bytes",
			Help: "Total swap in bytes",
		}),
		NodeSwapUsed: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_node_swap_used_bytes",
			Help: "Used swap in bytes",
		}),
		NodeFSTotal: factory.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hive_node_filesystem_total_bytes",
			Help: "Total filesystem size in bytes",
		}, []string{"mount", "device"}),
		NodeFSUsed: factory.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hive_node_filesystem_used_bytes",
			Help: "Used filesystem size in bytes",
		}, []string{"mount", "device"}),
		NodeFSUsagePercent: factory.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hive_node_filesystem_usage_percent",
			Help: "Filesystem usage percent",
		}, []string{"mount", "device"}),
		NodeDiskReadBytes: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "hive_node_disk_read_bytes_total",
			Help: "Total disk read bytes",
		}, []string{"device"}),
		NodeDiskWriteBytes: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "hive_node_disk_write_bytes_total",
			Help: "Total disk write bytes",
		}, []string{"device"}),
		NodeNetRxBytes: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "hive_node_network_rx_bytes_total",
			Help: "Total network receive bytes",
		}, []string{"interface"}),
		NodeNetTxBytes: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "hive_node_network_tx_bytes_total",
			Help: "Total network transmit bytes",
		}, []string{"interface"}),
		NodeNetRxErrors: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "hive_node_network_rx_errors_total",
			Help: "Total network receive errors",
		}, []string{"interface"}),
		NodeNetTxErrors: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "hive_node_network_tx_errors_total",
			Help: "Total network transmit errors",
		}, []string{"interface"}),
		NodeUptime: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_node_uptime_seconds",
			Help: "Node uptime in seconds",
		}),
		NodePkgsUpgradable: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_node_packages_upgradable",
			Help: "Number of upgradable packages",
		}),
		NodePkgsSecurity: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_node_security_updates",
			Help: "Number of security updates",
		}),
		NodeRebootRequired: factory.NewGauge(prometheus.GaugeOpts{
			Name: "hive_node_reboot_required",
			Help: "Whether a reboot is required (0 or 1)",
		}),
	}
}

// UpdateNodeMetrics updates the Prometheus node metrics from the collected data.
func (m *Metrics) UpdateNodeMetrics(resp *agentv1.HostMetricsResponse) {
	if resp == nil {
		return
	}

	for _, core := range resp.CpuCores {
		m.NodeCPUUsage.WithLabelValues(fmt.Sprintf("%d", core.Core)).Set(core.Percent)
	}

	m.NodeLoadAverage.WithLabelValues("1m").Set(resp.LoadAvg_1)
	m.NodeLoadAverage.WithLabelValues("5m").Set(resp.LoadAvg_5)
	m.NodeLoadAverage.WithLabelValues("15m").Set(resp.LoadAvg_15)

	m.NodeMemoryTotal.Set(float64(resp.MemoryTotal))
	m.NodeMemoryUsed.Set(float64(resp.MemoryUsed))
	m.NodeMemoryAvail.Set(float64(resp.MemoryAvailable))
	m.NodeSwapTotal.Set(float64(resp.SwapTotal))
	m.NodeSwapUsed.Set(float64(resp.SwapUsed))

	for _, fs := range resp.Filesystems {
		m.NodeFSTotal.WithLabelValues(fs.MountPoint, fs.Device).Set(float64(fs.TotalBytes))
		m.NodeFSUsed.WithLabelValues(fs.MountPoint, fs.Device).Set(float64(fs.UsedBytes))
		m.NodeFSUsagePercent.WithLabelValues(fs.MountPoint, fs.Device).Set(fs.UsagePercent)
	}

	for _, d := range resp.DiskIo {
		m.NodeDiskReadBytes.WithLabelValues(d.Device).Add(float64(d.ReadBytes))
		m.NodeDiskWriteBytes.WithLabelValues(d.Device).Add(float64(d.WriteBytes))
	}

	for _, n := range resp.NetworkInterfaces {
		m.NodeNetRxBytes.WithLabelValues(n.Name).Add(float64(n.BytesRecv))
		m.NodeNetTxBytes.WithLabelValues(n.Name).Add(float64(n.BytesSent))
		m.NodeNetRxErrors.WithLabelValues(n.Name).Add(float64(n.ErrorsIn))
		m.NodeNetTxErrors.WithLabelValues(n.Name).Add(float64(n.ErrorsOut))
	}

	m.NodeUptime.Set(float64(resp.UptimeSeconds))
}

// UpdatePackageMetrics updates Prometheus metrics from package status.
func (m *Metrics) UpdatePackageMetrics(status *agentv1.PackageStatusResponse) {
	if status == nil {
		return
	}
	m.NodePkgsUpgradable.Set(float64(status.UpgradableCount))
	m.NodePkgsSecurity.Set(float64(status.SecurityCount))
	if status.RebootRequired {
		m.NodeRebootRequired.Set(1)
	} else {
		m.NodeRebootRequired.Set(0)
	}
}
