package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func collectNetwork(report *NodeMetricsReport) {
	data, err := os.ReadFile(filepath.Join(hostProc, "net", "dev"))
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i < 2 { // skip header lines
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		ifName := strings.TrimSpace(line[:colonIdx])
		rest := strings.Fields(line[colonIdx+1:])
		if len(rest) < 16 {
			continue
		}

		// Skip virtual interfaces
		if isVirtualInterface(ifName) {
			continue
		}

		rxBytes, _ := strconv.ParseUint(rest[0], 10, 64)
		rxPackets, _ := strconv.ParseUint(rest[1], 10, 64)
		rxErrors, _ := strconv.ParseUint(rest[2], 10, 64)

		txBytes, _ := strconv.ParseUint(rest[8], 10, 64)
		txPackets, _ := strconv.ParseUint(rest[9], 10, 64)
		txErrors, _ := strconv.ParseUint(rest[10], 10, 64)

		iface := NetInterface{
			Name:      ifName,
			RxBytes:   rxBytes,
			TxBytes:   txBytes,
			RxPackets: rxPackets,
			TxPackets: txPackets,
			RxErrors:  rxErrors,
			TxErrors:  txErrors,
		}

		iface.LinkSpeedMbps = readLinkSpeed(ifName)
		report.Interfaces = append(report.Interfaces, iface)
	}
}

func isVirtualInterface(name string) bool {
	prefixes := []string{"lo", "docker", "veth", "br-", "virbr", "vxlan", "flannel", "cni", "cali"}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func readLinkSpeed(ifName string) int {
	hostSys := "/host/sys"
	if _, err := os.Stat(hostSys); os.IsNotExist(err) {
		hostSys = "/sys"
	}
	speedFile := filepath.Join(hostSys, "class", "net", ifName, "speed")
	data, err := os.ReadFile(speedFile)
	if err != nil {
		return 0
	}
	speed, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if speed < 0 {
		return 0
	}
	return speed
}
