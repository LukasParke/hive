package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func collectSystem(report *NodeMetricsReport) {
	hostname, _ := os.Hostname()
	report.Hostname = hostname

	collectOSInfo(report)
	collectUptime(report)
	collectProcessCount(report)
	collectPendingUpdates(report)
}

func collectOSInfo(report *NodeMetricsReport) {
	osRelease := "/host/etc/os-release"
	if _, err := os.Stat(osRelease); os.IsNotExist(err) {
		osRelease = "/etc/os-release"
	}

	data, err := os.ReadFile(osRelease)
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			val = strings.Trim(val, `"`)
			report.OS = val
			break
		}
	}

	kernelData, err := os.ReadFile(filepath.Join(hostProc, "version"))
	if err != nil {
		return
	}
	fields := strings.Fields(string(kernelData))
	if len(fields) >= 3 {
		report.Kernel = fields[2]
	}
}

func collectUptime(report *NodeMetricsReport) {
	data, err := os.ReadFile(filepath.Join(hostProc, "uptime"))
	if err != nil {
		return
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 1 {
		secs, _ := strconv.ParseFloat(fields[0], 64)
		report.Uptime = uint64(secs)
	}
}

func collectProcessCount(report *NodeMetricsReport) {
	entries, err := os.ReadDir(hostProc)
	if err != nil {
		return
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			if _, err := strconv.Atoi(e.Name()); err == nil {
				count++
			}
		}
	}
	report.ProcessCount = count
}

func collectPendingUpdates(report *NodeMetricsReport) {
	// Best-effort: try common package managers
	if count := tryAptUpdates(); count >= 0 {
		report.PendingUpdates = count
		return
	}
	if count := tryPacmanUpdates(); count >= 0 {
		report.PendingUpdates = count
		return
	}
	if count := tryDnfUpdates(); count >= 0 {
		report.PendingUpdates = count
		return
	}
}

func tryAptUpdates() int {
	out, err := exec.Command("apt", "list", "--upgradable").Output()
	if err != nil {
		return -1
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	count := 0
	for _, l := range lines {
		if strings.Contains(l, "upgradable") {
			count++
		}
	}
	return count
}

func tryPacmanUpdates() int {
	out, err := exec.Command("checkupdates").Output()
	if err != nil {
		return -1
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

func tryDnfUpdates() int {
	out, err := exec.Command("dnf", "check-update", "--quiet").Output()
	if err != nil {
		// dnf returns exit code 100 when updates are available
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 100 {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			return len(lines)
		}
		return -1
	}
	return 0
}
