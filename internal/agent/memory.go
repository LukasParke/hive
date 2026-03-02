package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func collectMemory(report *NodeMetricsReport) {
	data, err := os.ReadFile(filepath.Join(hostProc, "meminfo"))
	if err != nil {
		return
	}

	info := make(map[string]uint64)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(parts[1])
		valStr = strings.TrimSuffix(valStr, " kB")
		valStr = strings.TrimSpace(valStr)
		val, err := strconv.ParseUint(valStr, 10, 64)
		if err != nil {
			continue
		}
		info[key] = val * 1024 // convert kB to bytes
	}

	report.MemTotal = info["MemTotal"]
	report.MemAvailable = info["MemAvailable"]
	report.MemBuffers = info["Buffers"]
	report.MemCached = info["Cached"]
	report.MemUsed = report.MemTotal - report.MemAvailable
	report.SwapTotal = info["SwapTotal"]
	report.SwapUsed = info["SwapTotal"] - info["SwapFree"]
}
