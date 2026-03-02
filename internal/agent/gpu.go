package agent

import (
	"os/exec"
	"strconv"
	"strings"
)

func collectGPU(report *NodeMetricsReport) {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, ", ")
		if len(fields) < 6 {
			continue
		}

		idx, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
		name := strings.TrimSpace(fields[1])
		util, _ := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
		memUsed, _ := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 64)
		memTotal, _ := strconv.ParseUint(strings.TrimSpace(fields[4]), 10, 64)
		temp, _ := strconv.ParseFloat(strings.TrimSpace(fields[5]), 64)

		report.GPUs = append(report.GPUs, GPUMetrics{
			Index:       idx,
			Name:        name,
			UtilPct:     util,
			MemUsed:     memUsed * 1024 * 1024, // MiB to bytes
			MemTotal:    memTotal * 1024 * 1024,
			TempCelsius: temp,
		})
	}
}
