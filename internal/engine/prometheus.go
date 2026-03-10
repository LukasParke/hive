package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// promResult is a single instant-query result from Prometheus.
type promResult struct {
	Metric map[string]string  `json:"metric"`
	Value  [2]json.RawMessage `json:"value"` // [unix_ts, "value"]
}

// promRangeResult is a single range-query result from Prometheus.
type promRangeResult struct {
	Metric map[string]string    `json:"metric"`
	Values [][2]json.RawMessage `json:"values"`
}

type promResponse[T any] struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []T    `json:"result"`
	} `json:"data"`
}

var promHTTP = &http.Client{Timeout: 10 * time.Second}

func promQuery(ctx context.Context, baseURL, query string) ([]promResult, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := promHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("prometheus returned %d", resp.StatusCode)
	}
	var body promResponse[promResult]
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed")
	}
	return body.Data.Result, nil
}

func promQueryRange(ctx context.Context, baseURL, query string, start, end time.Time, step string) ([]promRangeResult, error) {
	u := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=%s",
		baseURL, url.QueryEscape(query), start.Unix(), end.Unix(), url.QueryEscape(step))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := promHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("prometheus returned %d", resp.StatusCode)
	}
	var body promResponse[promRangeResult]
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Status != "success" {
		return nil, fmt.Errorf("prometheus range query failed")
	}
	return body.Data.Result, nil
}

func safePromQuery(ctx context.Context, baseURL, query string) []promResult {
	r, err := promQuery(ctx, baseURL, query)
	if err != nil {
		return nil
	}
	return r
}

func promVal(r []promResult) float64 {
	if len(r) == 0 {
		return 0
	}
	return parsePromValue(r[0].Value[1])
}

func parsePromValue(raw json.RawMessage) float64 {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func promValByInstance(results []promResult) map[string]float64 {
	m := make(map[string]float64, len(results))
	for _, r := range results {
		inst := r.Metric["instance"]
		if inst == "" {
			inst = r.Metric["node_hostname"]
		}
		m[inst] = parsePromValue(r.Value[1])
	}
	return m
}

// ── Prometheus-backed metrics types (match UI expectations) ──

type clusterSummary struct {
	Nodes      int     `json:"nodes"`
	NodesUp    int     `json:"nodesUp"`
	TotalCores int     `json:"totalCores"`
	TotalRAM   float64 `json:"totalRAM"`
	TotalDisk  float64 `json:"totalDisk"`
	UsedDisk   float64 `json:"usedDisk"`
	AvgCPU     float64 `json:"avgCPU"`
	Containers int     `json:"containers"`
}

type nodeCurrent struct {
	Hostname          string  `json:"hostname"`
	NodeID            string  `json:"nodeId"`
	Up                bool    `json:"up"`
	CPUPct            float64 `json:"cpuPct"`
	Cores             int     `json:"cores"`
	MemUsed           float64 `json:"memUsed"`
	MemTotal          float64 `json:"memTotal"`
	DiskUsed          float64 `json:"diskUsed"`
	DiskTotal         float64 `json:"diskTotal"`
	UptimeSeconds     float64 `json:"uptimeSeconds"`
	TempCelsius       float64 `json:"tempCelsius"`
	ContainersRunning int     `json:"containersRunning"`
	LoadAvg1          float64 `json:"loadAvg1"`
}

type timeSeriesPoint struct {
	Ts    int64   `json:"ts"`
	Value float64 `json:"value"`
}

type nodeHistory struct {
	Hostname string            `json:"hostname"`
	CPU      []timeSeriesPoint `json:"cpu"`
	Mem      []timeSeriesPoint `json:"mem"`
}

type containerMetric struct {
	Name     string  `json:"name"`
	Image    string  `json:"image"`
	Instance string  `json:"instance"`
	CPUPct   float64 `json:"cpuPct"`
	MemBytes float64 `json:"memBytes"`
}

// ── High-level query functions ──

func (s *Server) fetchClusterSummary(ctx context.Context) *clusterSummary {
	purl := s.cfg.PrometheusURL
	if purl == "" {
		return nil
	}

	type queryResult struct {
		idx    int
		result []promResult
	}

	queries := []string{
		`up{job="node-exporter"}`,
		`count by (instance)(node_cpu_seconds_total{mode="idle"})`,
		`node_memory_MemTotal_bytes`,
		`sum by (instance)(node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs"})`,
		`sum by (instance)(node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs"} - node_filesystem_avail_bytes{mountpoint="/",fstype!="tmpfs"})`,
		`100 - (avg by (instance)(rate(node_cpu_seconds_total{mode="idle"}[1m])) * 100)`,
		`sum(count by (instance)(container_last_seen{image!=""}))`,
	}

	results := make([][]promResult, len(queries))
	for i, q := range queries {
		results[i] = safePromQuery(ctx, purl, q)
	}

	upResult := results[0]
	coresResult := results[1]
	ramResult := results[2]
	diskTotalResult := results[3]
	diskUsedResult := results[4]
	cpuResult := results[5]
	containersResult := results[6]

	nodesUp := 0
	for _, r := range upResult {
		if parsePromValue(r.Value[1]) == 1 {
			nodesUp++
		}
	}

	totalCores := 0
	for _, v := range promValByInstance(coresResult) {
		totalCores += int(v)
	}

	totalRAM := 0.0
	for _, v := range promValByInstance(ramResult) {
		totalRAM += v
	}

	totalDisk := 0.0
	for _, v := range promValByInstance(diskTotalResult) {
		totalDisk += v
	}

	usedDisk := 0.0
	for _, v := range promValByInstance(diskUsedResult) {
		usedDisk += v
	}

	cpuVals := promValByInstance(cpuResult)
	avgCPU := 0.0
	if len(cpuVals) > 0 {
		sum := 0.0
		for _, v := range cpuVals {
			sum += v
		}
		avgCPU = sum / float64(len(cpuVals))
	}

	return &clusterSummary{
		Nodes:      len(upResult),
		NodesUp:    nodesUp,
		TotalCores: totalCores,
		TotalRAM:   totalRAM,
		TotalDisk:  totalDisk,
		UsedDisk:   usedDisk,
		AvgCPU:     avgCPU,
		Containers: int(promVal(containersResult)),
	}
}

func (s *Server) fetchNodeMetrics(ctx context.Context) []nodeCurrent {
	purl := s.cfg.PrometheusURL
	if purl == "" {
		return []nodeCurrent{}
	}

	queries := []string{
		`up{job="node-exporter"}`,
		`100 - (avg by (instance)(rate(node_cpu_seconds_total{mode="idle"}[1m])) * 100)`,
		`count by (instance)(node_cpu_seconds_total{mode="idle"})`,
		`node_memory_MemTotal_bytes`,
		`node_memory_MemAvailable_bytes`,
		`sum by (instance)(node_filesystem_size_bytes{mountpoint="/",fstype!="tmpfs"})`,
		`sum by (instance)(node_filesystem_avail_bytes{mountpoint="/",fstype!="tmpfs"})`,
		`node_time_seconds - node_boot_time_seconds`,
		`node_hwmon_temp_celsius`,
		`count by (instance)(container_last_seen{image!=""})`,
		`node_load1`,
	}

	results := make([][]promResult, len(queries))
	for i, q := range queries {
		results[i] = safePromQuery(ctx, purl, q)
	}

	upResult := results[0]
	cpuMap := promValByInstance(results[1])
	coresMap := promValByInstance(results[2])
	memTotalMap := promValByInstance(results[3])
	memAvailMap := promValByInstance(results[4])
	diskTotalMap := promValByInstance(results[5])
	diskAvailMap := promValByInstance(results[6])
	uptimeMap := promValByInstance(results[7])
	containerMap := promValByInstance(results[9])
	loadMap := promValByInstance(results[10])

	tempMap := make(map[string]float64)
	for _, r := range results[8] {
		inst := r.Metric["instance"]
		if inst == "" {
			inst = r.Metric["node_hostname"]
		}
		temp := parsePromValue(r.Value[1])
		if temp > tempMap[inst] {
			tempMap[inst] = temp
		}
	}

	upMap := make(map[string]bool)
	hostnames := make([]string, 0, len(upResult))
	for _, r := range upResult {
		inst := r.Metric["instance"]
		if inst == "" {
			inst = r.Metric["node_hostname"]
		}
		upMap[inst] = parsePromValue(r.Value[1]) == 1
		hostnames = append(hostnames, inst)
	}

	nodes := make([]nodeCurrent, 0, len(hostnames))
	for _, h := range hostnames {
		memTotal := memTotalMap[h]
		memAvail := memAvailMap[h]
		dTotal := diskTotalMap[h]
		dAvail := diskAvailMap[h]

		nodeID := h
		for _, r := range upResult {
			inst := r.Metric["instance"]
			if inst == "" {
				inst = r.Metric["node_hostname"]
			}
			if inst == h && r.Metric["node_id"] != "" {
				nodeID = r.Metric["node_id"]
				break
			}
		}

		nodes = append(nodes, nodeCurrent{
			Hostname:          h,
			NodeID:            nodeID,
			Up:                upMap[h],
			CPUPct:            cpuMap[h],
			Cores:             int(coresMap[h]),
			MemUsed:           memTotal - memAvail,
			MemTotal:          memTotal,
			DiskUsed:          dTotal - dAvail,
			DiskTotal:         dTotal,
			UptimeSeconds:     uptimeMap[h],
			TempCelsius:       tempMap[h],
			ContainersRunning: int(containerMap[h]),
			LoadAvg1:          loadMap[h],
		})
	}
	return nodes
}

func (s *Server) fetchNodeHistory(ctx context.Context, hostname string, rangeSec int) *nodeHistory {
	purl := s.cfg.PrometheusURL
	if purl == "" {
		return &nodeHistory{Hostname: hostname}
	}

	now := time.Now()
	start := now.Add(-time.Duration(rangeSec) * time.Second)
	step := "15s"
	if rangeSec > 3600*6 {
		step = "60s"
	}

	instFilter := fmt.Sprintf(`instance="%s"`, hostname)

	cpuQ := fmt.Sprintf(`100 - (avg by (instance)(rate(node_cpu_seconds_total{mode="idle",%s}[30s])) * 100)`, instFilter)
	memQ := fmt.Sprintf(`100 * (1 - node_memory_MemAvailable_bytes{%s} / node_memory_MemTotal_bytes{%s})`, instFilter, instFilter)

	cpuResult, _ := promQueryRange(ctx, purl, cpuQ, start, now, step)
	memResult, _ := promQueryRange(ctx, purl, memQ, start, now, step)

	toPoints := func(results []promRangeResult) []timeSeriesPoint {
		if len(results) == 0 {
			return []timeSeriesPoint{}
		}
		pts := make([]timeSeriesPoint, 0, len(results[0].Values))
		for _, v := range results[0].Values {
			var tsRaw float64
			_ = json.Unmarshal(v[0], &tsRaw)
			pts = append(pts, timeSeriesPoint{
				Ts:    int64(tsRaw),
				Value: parsePromValue(v[1]),
			})
		}
		return pts
	}

	return &nodeHistory{
		Hostname: hostname,
		CPU:      toPoints(cpuResult),
		Mem:      toPoints(memResult),
	}
}

func (s *Server) fetchTopContainers(ctx context.Context, limit int) []containerMetric {
	purl := s.cfg.PrometheusURL
	if purl == "" {
		return []containerMetric{}
	}

	groupBy := "name, image, instance, container_label_com_docker_swarm_service_name"
	cpuResult := safePromQuery(ctx, purl, fmt.Sprintf(`sum by (%s)(rate(container_cpu_usage_seconds_total{image!=""}[1m])) * 100`, groupBy))
	memResult := safePromQuery(ctx, purl, fmt.Sprintf(`sum by (%s)(container_memory_usage_bytes{image!=""})`, groupBy))

	type cpuInfo struct {
		cpuPct      float64
		image       string
		instance    string
		serviceName string
	}

	cpuMap := make(map[string]cpuInfo)
	for _, r := range cpuResult {
		name := r.Metric["name"]
		svcName := r.Metric["container_label_com_docker_swarm_service_name"]
		cpuMap[name] = cpuInfo{
			cpuPct:      parsePromValue(r.Value[1]),
			image:       r.Metric["image"],
			instance:    r.Metric["instance"],
			serviceName: svcName,
		}
	}

	memMap := make(map[string]float64)
	for _, r := range memResult {
		memMap[r.Metric["name"]] = parsePromValue(r.Value[1])
	}

	containers := make([]containerMetric, 0, len(cpuMap))
	for rawName, cpu := range cpuMap {
		displayName := cpu.serviceName
		if displayName == "" {
			displayName = rawName
		}
		containers = append(containers, containerMetric{
			Name:     displayName,
			Image:    cleanImageName(cpu.image),
			Instance: cpu.instance,
			CPUPct:   cpu.cpuPct,
			MemBytes: memMap[rawName],
		})
	}

	// Sort by CPU descending
	for i := 0; i < len(containers); i++ {
		for j := i + 1; j < len(containers); j++ {
			if containers[j].CPUPct > containers[i].CPUPct {
				containers[i], containers[j] = containers[j], containers[i]
			}
		}
	}

	if limit > 0 && len(containers) > limit {
		containers = containers[:limit]
	}
	return containers
}

func cleanImageName(raw string) string {
	img := raw
	if idx := strings.Index(img, "@"); idx > 0 {
		img = img[:idx]
	}
	// Remove local registry prefix
	for _, prefix := range []string{"127.0.0.1:5000/", "localhost:5000/"} {
		img = strings.TrimPrefix(img, prefix)
	}
	if idx := strings.LastIndex(img, ":"); idx > 0 {
		img = img[:idx]
	}
	if img == "" {
		return raw
	}
	return img
}
