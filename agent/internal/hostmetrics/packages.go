package hostmetrics

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
)

// collectPackageStatus detects the distro and returns package status.
func collectPackageStatus(hostRoot string) *agentv1.PackageStatusResponse {
	distro := detectDistro(hostRoot)

	switch distro {
	case "ubuntu", "debian":
		return collectAptStatus(hostRoot)
	case "centos", "rhel", "fedora", "rocky", "alma":
		return collectYumStatus(hostRoot)
	case "alpine":
		return collectApkStatus(hostRoot)
	default:
		return &agentv1.PackageStatusResponse{
			PackageManager: "unknown",
			LastCheckedAt:  time.Now().Unix(),
		}
	}
}

// detectDistro reads /etc/os-release (or host-mounted equivalent) and returns the distro ID.
func detectDistro(hostRoot string) string {
	path := filepath.Join(hostRoot, "etc", "os-release")
	f, err := os.Open(path) //nolint:gosec // path is derived from the trusted hostRoot config
	if err != nil {
		return "unknown"
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ID=") {
			id := strings.TrimPrefix(line, "ID=")
			id = strings.Trim(id, `"`)
			return strings.ToLower(id)
		}
	}
	return "unknown"
}

func collectAptStatus(hostRoot string) *agentv1.PackageStatusResponse {
	resp := &agentv1.PackageStatusResponse{
		PackageManager: "apt",
		LastCheckedAt:  time.Now().Unix(),
	}

	// Count installed packages from dpkg status
	statusPath := filepath.Join(hostRoot, "var", "lib", "dpkg", "status")
	if f, err := os.Open(statusPath); err == nil { //nolint:gosec // statusPath is derived from the trusted hostRoot config
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "Status: install ok installed") {
				resp.TotalInstalled++
			}
		}
	}

	// Check for available upgrades by parsing apt lists
	upgradesPath := filepath.Join(hostRoot, "var", "lib", "apt", "lists")
	entries, err := os.ReadDir(upgradesPath)
	if err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), "Packages") {
				// Parse package list for upgrade candidates
				parsePath := filepath.Join(upgradesPath, e.Name())
				updates := parseAptPackageList(parsePath)
				resp.AvailableUpdates = append(resp.AvailableUpdates, updates...)
			}
		}
	}

	resp.UpgradableCount = int32(min(len(resp.AvailableUpdates), math.MaxInt32))

	// Count security updates
	for _, u := range resp.AvailableUpdates {
		if u.IsSecurity {
			resp.SecurityCount++
		}
	}

	// Check reboot required
	rebootPath := filepath.Join(hostRoot, "var", "run", "reboot-required")
	if _, err := os.Stat(rebootPath); err == nil {
		resp.RebootRequired = true
	}

	return resp
}

func parseAptPackageList(path string) []*agentv1.PackageUpdate {
	// This is a simplified parser - in production would compare installed vs available versions
	// For now, return empty since proper diff requires dpkg status comparison
	return nil
}

func collectYumStatus(hostRoot string) *agentv1.PackageStatusResponse {
	resp := &agentv1.PackageStatusResponse{
		PackageManager: "yum",
		LastCheckedAt:  time.Now().Unix(),
	}

	// Count installed packages from rpm database
	rpmDBPath := filepath.Join(hostRoot, "var", "lib", "rpm")
	if _, err := os.Stat(rpmDBPath); err == nil {
		// RPM database exists - count packages
		// In production, would use rpm -qa equivalent or parse the BDB/SQLite db
		resp.TotalInstalled = -1 // unknown without exec
	}

	// Check for dnf preference
	if _, err := os.Stat(filepath.Join(hostRoot, "usr", "bin", "dnf")); err == nil {
		resp.PackageManager = "dnf"
	}

	return resp
}

func collectApkStatus(hostRoot string) *agentv1.PackageStatusResponse {
	resp := &agentv1.PackageStatusResponse{
		PackageManager: "apk",
		LastCheckedAt:  time.Now().Unix(),
	}

	// Count installed packages
	installedPath := filepath.Join(hostRoot, "lib", "apk", "db", "installed")
	if f, err := os.Open(installedPath); err == nil { //nolint:gosec // installedPath is derived from the trusted hostRoot config
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "P:") {
				resp.TotalInstalled++
			}
		}
	}

	return resp
}
