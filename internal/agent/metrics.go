package agent

type NodeMetricsReport struct {
	NodeID    string `json:"node_id"`
	Hostname  string `json:"hostname"`
	Timestamp int64  `json:"timestamp"`

	CPUCores       int       `json:"cpu_cores"`
	CPUPerCore     []float64 `json:"cpu_per_core"`
	CPUTotalPct    float64   `json:"cpu_total_pct"`
	LoadAvg1       float64   `json:"load_avg_1"`
	LoadAvg5       float64   `json:"load_avg_5"`
	LoadAvg15      float64   `json:"load_avg_15"`
	CPUTempCelsius float64   `json:"cpu_temp_celsius"`

	MemTotal     uint64 `json:"mem_total"`
	MemUsed      uint64 `json:"mem_used"`
	MemAvailable uint64 `json:"mem_available"`
	MemBuffers   uint64 `json:"mem_buffers"`
	MemCached    uint64 `json:"mem_cached"`
	SwapTotal    uint64 `json:"swap_total"`
	SwapUsed     uint64 `json:"swap_used"`

	Disks      []DiskMetrics  `json:"disks"`
	Interfaces []NetInterface `json:"interfaces"`

	OS             string `json:"os"`
	Kernel         string `json:"kernel"`
	Uptime         uint64 `json:"uptime_seconds"`
	ProcessCount   int    `json:"process_count"`
	PendingUpdates int    `json:"pending_updates"`

	ContainersRunning int `json:"containers_running"`
	ContainersStopped int `json:"containers_stopped"`
	ImagesCount       int `json:"images_count"`
	VolumesCount      int `json:"volumes_count"`

	GPUs []GPUMetrics `json:"gpus,omitempty"`

	BlockDevices []BlockDevice `json:"block_devices,omitempty"`
}

type BlockDevice struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       uint64 `json:"size"`
	Type       string `json:"type"`
	MountPoint string `json:"mount_point,omitempty"`
	FSType     string `json:"fs_type,omitempty"`
	Model      string `json:"model,omitempty"`
	Serial     string `json:"serial,omitempty"`
	Rotational bool   `json:"rotational"`
	Transport  string `json:"transport,omitempty"`
	Available  bool   `json:"available"`
}

type DiskMetrics struct {
	MountPoint string `json:"mount_point"`
	Device     string `json:"device"`
	FSType     string `json:"fs_type"`
	Total      uint64 `json:"total"`
	Used       uint64 `json:"used"`
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
	IOps       uint64 `json:"iops"`
	SmartOK    *bool  `json:"smart_ok,omitempty"`
}

type NetInterface struct {
	Name          string `json:"name"`
	RxBytes       uint64 `json:"rx_bytes"`
	TxBytes       uint64 `json:"tx_bytes"`
	RxPackets     uint64 `json:"rx_packets"`
	TxPackets     uint64 `json:"tx_packets"`
	RxErrors      uint64 `json:"rx_errors"`
	TxErrors      uint64 `json:"tx_errors"`
	LinkSpeedMbps int    `json:"link_speed_mbps"`
}

type GPUMetrics struct {
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	UtilPct     float64 `json:"util_pct"`
	MemUsed     uint64  `json:"mem_used"`
	MemTotal    uint64  `json:"mem_total"`
	TempCelsius float64 `json:"temp_celsius"`
}
