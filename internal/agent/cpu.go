package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var hostProc = "/host/proc"

func init() {
	if _, err := os.Stat(hostProc); os.IsNotExist(err) {
		hostProc = "/proc"
	}
}

type cpuTimes struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

func (c cpuTimes) total() uint64 {
	return c.user + c.nice + c.system + c.idle + c.iowait + c.irq + c.softirq + c.steal
}

func (c cpuTimes) active() uint64 {
	return c.total() - c.idle - c.iowait
}

func parseProcStat() (total cpuTimes, perCore []cpuTimes, err error) {
	data, err := os.ReadFile(filepath.Join(hostProc, "stat"))
	if err != nil {
		return total, nil, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		if fields[0] == "cpu" {
			total = parseCPUFields(fields[1:])
		} else if strings.HasPrefix(fields[0], "cpu") {
			perCore = append(perCore, parseCPUFields(fields[1:]))
		}
	}
	return
}

func parseCPUFields(fields []string) cpuTimes {
	var ct cpuTimes
	if len(fields) >= 1 {
		ct.user, _ = strconv.ParseUint(fields[0], 10, 64)
	}
	if len(fields) >= 2 {
		ct.nice, _ = strconv.ParseUint(fields[1], 10, 64)
	}
	if len(fields) >= 3 {
		ct.system, _ = strconv.ParseUint(fields[2], 10, 64)
	}
	if len(fields) >= 4 {
		ct.idle, _ = strconv.ParseUint(fields[3], 10, 64)
	}
	if len(fields) >= 5 {
		ct.iowait, _ = strconv.ParseUint(fields[4], 10, 64)
	}
	if len(fields) >= 6 {
		ct.irq, _ = strconv.ParseUint(fields[5], 10, 64)
	}
	if len(fields) >= 7 {
		ct.softirq, _ = strconv.ParseUint(fields[6], 10, 64)
	}
	if len(fields) >= 8 {
		ct.steal, _ = strconv.ParseUint(fields[7], 10, 64)
	}
	return ct
}

func collectCPU(report *NodeMetricsReport) {
	report.CPUCores = runtime.NumCPU()

	prev, prevCores, err := parseProcStat()
	if err != nil {
		return
	}
	time.Sleep(500 * time.Millisecond)
	curr, currCores, err := parseProcStat()
	if err != nil {
		return
	}

	totalDelta := float64(curr.total() - prev.total())
	if totalDelta > 0 {
		report.CPUTotalPct = float64(curr.active()-prev.active()) / totalDelta * 100
	}

	for i := 0; i < len(currCores) && i < len(prevCores); i++ {
		delta := float64(currCores[i].total() - prevCores[i].total())
		if delta > 0 {
			pct := float64(currCores[i].active()-prevCores[i].active()) / delta * 100
			report.CPUPerCore = append(report.CPUPerCore, pct)
		} else {
			report.CPUPerCore = append(report.CPUPerCore, 0)
		}
	}

	collectLoadAvg(report)
	collectCPUTemp(report)
}

func collectLoadAvg(report *NodeMetricsReport) {
	data, err := os.ReadFile(filepath.Join(hostProc, "loadavg"))
	if err != nil {
		return
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		report.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
		report.LoadAvg5, _ = strconv.ParseFloat(fields[1], 64)
		report.LoadAvg15, _ = strconv.ParseFloat(fields[2], 64)
	}
}

func collectCPUTemp(report *NodeMetricsReport) {
	hostSys := "/host/sys"
	if _, err := os.Stat(hostSys); os.IsNotExist(err) {
		hostSys = "/sys"
	}

	thermalDir := filepath.Join(hostSys, "class", "thermal")
	entries, err := os.ReadDir(thermalDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "thermal_zone") {
			continue
		}

		typeData, err := os.ReadFile(filepath.Join(thermalDir, entry.Name(), "type"))
		if err != nil {
			continue
		}
		typeName := strings.TrimSpace(string(typeData))

		if strings.Contains(typeName, "cpu") || strings.Contains(typeName, "x86_pkg") ||
			strings.Contains(typeName, "coretemp") || strings.Contains(typeName, "k10temp") {
			tempData, err := os.ReadFile(filepath.Join(thermalDir, entry.Name(), "temp"))
			if err != nil {
				continue
			}
			milliC, err := strconv.ParseFloat(strings.TrimSpace(string(tempData)), 64)
			if err != nil {
				continue
			}
			report.CPUTempCelsius = milliC / 1000.0
			return
		}
	}

	// Fallback: use the first thermal zone
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "thermal_zone") {
			continue
		}
		tempData, err := os.ReadFile(filepath.Join(thermalDir, entry.Name(), "temp"))
		if err != nil {
			continue
		}
		milliC, _ := strconv.ParseFloat(strings.TrimSpace(string(tempData)), 64)
		if milliC > 0 {
			report.CPUTempCelsius = milliC / 1000.0
			return
		}
	}
	_ = fmt.Sprintf("no CPU temp sensor found")
}
