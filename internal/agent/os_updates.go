package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const rootfsPrefix = "/rootfs"

type OSUpdateStatus struct {
	NodeID          string           `json:"node_id"`
	Hostname        string           `json:"hostname"`
	OS              string           `json:"os"`
	KernelVersion   string           `json:"kernel_version"`
	PackageManager  string           `json:"package_manager"`
	PendingCount    int              `json:"pending_count"`
	PendingPackages []PendingPackage `json:"pending_packages"`
	SecurityCount   int              `json:"security_count"`
	RebootRequired  bool             `json:"reboot_required"`
	LastChecked     time.Time        `json:"last_checked"`
}

type PendingPackage struct {
	Name           string `json:"name"`
	CurrentVersion string `json:"current_version"`
	NewVersion     string `json:"new_version"`
	IsSecurity     bool   `json:"is_security"`
}

type OSUpdateChecker struct {
	nc       *nats.Conn
	docker   *client.Client
	nodeID   string
	log      *zap.SugaredLogger
	interval time.Duration
	hasRootfs bool
}

func NewOSUpdateChecker(nc *nats.Conn, nodeID string, log *zap.SugaredLogger) *OSUpdateChecker {
	_, err := os.Stat(rootfsPrefix + "/etc/os-release")
	hasRootfs := err == nil

	var dc *client.Client
	dc, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Warnf("os-updates: docker client unavailable: %v", err)
	}

	return &OSUpdateChecker{
		nc:        nc,
		docker:    dc,
		nodeID:    nodeID,
		log:       log,
		interval:  6 * time.Hour,
		hasRootfs: hasRootfs,
	}
}

func (c *OSUpdateChecker) Start(ctx context.Context) {
	checkSubject := "hive.node.updates.check." + c.nodeID
	sub, err := c.nc.Subscribe(checkSubject, func(msg *nats.Msg) {
		c.log.Info("on-demand update check requested")
		status := c.check(ctx)
		data, _ := json.Marshal(status)
		if msg.Reply != "" {
			_ = c.nc.Publish(msg.Reply, data)
		}
		_ = c.nc.Publish("hive.node.updates.status."+c.nodeID, data)
	})
	if err != nil {
		c.log.Errorf("subscribe %s: %v", checkSubject, err)
		return
	}
	c.log.Infof("listening for update checks on %s", checkSubject)

	status := c.check(ctx)
	data, _ := json.Marshal(status)
	_ = c.nc.Publish("hive.node.updates.status."+c.nodeID, data)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				st := c.check(ctx)
				d, _ := json.Marshal(st)
				_ = c.nc.Publish("hive.node.updates.status."+c.nodeID, d)
			}
		}
	}()

	<-ctx.Done()
	_ = sub.Unsubscribe()
}

func (c *OSUpdateChecker) check(ctx context.Context) OSUpdateStatus {
	status := OSUpdateStatus{
		NodeID:      c.nodeID,
		Hostname:    c.nodeID,
		LastChecked: time.Now(),
	}

	status.OS = c.readOSInfo(ctx)
	status.KernelVersion = c.readKernelVersion(ctx)
	status.PackageManager = c.detectPackageManager(ctx)
	status.RebootRequired = c.checkRebootRequired()

	switch status.PackageManager {
	case "apt":
		c.runHostCmd("apt-get", "update", "-qq")
		status.PendingPackages = c.listAptUpgrades()
	case "dnf":
		status.PendingPackages = c.listDnfUpgrades()
	case "yum":
		status.PendingPackages = c.listYumUpgrades()
	case "pacman":
		c.runHostCmd("pacman", "-Sy")
		status.PendingPackages = c.listPacmanUpgrades()
	}

	status.PendingCount = len(status.PendingPackages)
	for _, p := range status.PendingPackages {
		if p.IsSecurity {
			status.SecurityCount++
		}
	}

	c.log.Infof("update check complete: %d pending (%d security), reboot=%v",
		status.PendingCount, status.SecurityCount, status.RebootRequired)

	return status
}

func (c *OSUpdateChecker) readOSInfo(ctx context.Context) string {
	if c.hasRootfs {
		data, err := os.ReadFile(rootfsPrefix + "/etc/os-release")
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
				}
			}
		}
	}

	output := c.runHostCmdOutput("cat", "/etc/os-release")
	if output != "" {
		for _, line := range strings.Split(output, "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
	}

	if c.docker != nil {
		info, err := c.docker.Info(ctx)
		if err == nil && info.OperatingSystem != "" {
			return info.OperatingSystem
		}
	}
	return "Linux"
}

func (c *OSUpdateChecker) readKernelVersion(ctx context.Context) string {
	// Containers share the host kernel, so uname -r works directly
	cmd := exec.Command("uname", "-r")
	out, err := cmd.Output()
	if err == nil {
		v := strings.TrimSpace(string(out))
		if v != "" {
			return v
		}
	}

	if c.docker != nil {
		info, err := c.docker.Info(ctx)
		if err == nil && info.KernelVersion != "" {
			return info.KernelVersion
		}
	}
	return ""
}

func (c *OSUpdateChecker) detectPackageManager(ctx context.Context) string {
	managers := []struct {
		binary string
		name   string
	}{
		{"apt-get", "apt"},
		{"dnf", "dnf"},
		{"yum", "yum"},
		{"pacman", "pacman"},
	}

	if c.hasRootfs {
		for _, m := range managers {
			for _, dir := range []string{"/usr/bin/", "/usr/sbin/", "/bin/", "/sbin/"} {
				if _, err := os.Stat(rootfsPrefix + dir + m.binary); err == nil {
					return m.name
				}
			}
		}
	}

	for _, m := range managers {
		if c.runHostCmdOutput("which", m.binary) != "" {
			return m.name
		}
	}

	if c.docker != nil {
		info, err := c.docker.Info(ctx)
		if err == nil {
			os := strings.ToLower(info.OperatingSystem)
			switch {
			case strings.Contains(os, "ubuntu") || strings.Contains(os, "debian"):
				return "apt"
			case strings.Contains(os, "fedora") || strings.Contains(os, "centos stream"):
				return "dnf"
			case strings.Contains(os, "centos") || strings.Contains(os, "red hat") || strings.Contains(os, "amazon"):
				return "yum"
			case strings.Contains(os, "arch"):
				return "pacman"
			}
		}
	}
	return "unknown"
}

func (c *OSUpdateChecker) checkRebootRequired() bool {
	if c.hasRootfs {
		if _, err := os.Stat(rootfsPrefix + "/var/run/reboot-required"); err == nil {
			return true
		}
	}
	output := c.runHostCmdOutput("cat", "/var/run/reboot-required")
	return strings.TrimSpace(output) != ""
}

var aptListRegex = regexp.MustCompile(`^(\S+?)(?:/\S+)?\s+(\S+)\s+\S+\s+\[upgradable from:\s+(\S+)\]`)

func (c *OSUpdateChecker) listAptUpgrades() []PendingPackage {
	output := c.runHostCmdOutput("apt", "list", "--upgradable")
	var packages []PendingPackage
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Listing...") || line == "" {
			continue
		}
		matches := aptListRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		pkg := PendingPackage{
			Name:           matches[1],
			NewVersion:     matches[2],
			CurrentVersion: matches[3],
			IsSecurity:     strings.Contains(line, "-security"),
		}
		packages = append(packages, pkg)
	}
	return packages
}

func (c *OSUpdateChecker) listDnfUpgrades() []PendingPackage {
	output := c.runHostCmdOutput("dnf", "check-update", "-q")
	return c.parseRpmUpdates(output)
}

func (c *OSUpdateChecker) listYumUpgrades() []PendingPackage {
	output := c.runHostCmdOutput("yum", "check-update", "-q")
	return c.parseRpmUpdates(output)
}

func (c *OSUpdateChecker) parseRpmUpdates(output string) []PendingPackage {
	var packages []PendingPackage
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Last metadata") || strings.HasPrefix(line, "Security:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			packages = append(packages, PendingPackage{
				Name:       fields[0],
				NewVersion: fields[1],
				IsSecurity: len(fields) >= 3 && strings.Contains(fields[2], "security"),
			})
		}
	}
	return packages
}

func (c *OSUpdateChecker) listPacmanUpgrades() []PendingPackage {
	output := c.runHostCmdOutput("pacman", "-Qu")
	var packages []PendingPackage
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 {
			packages = append(packages, PendingPackage{
				Name:           fields[0],
				CurrentVersion: fields[1],
				NewVersion:     fields[3],
			})
		}
	}
	return packages
}

func (c *OSUpdateChecker) runHostCmd(name string, args ...string) {
	nsArgs := append([]string{"-t", "1", "-m", "-u", "-i", "-n", "--", name}, args...)
	cmd := exec.Command("nsenter", nsArgs...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	_ = cmd.Run()
}

func (c *OSUpdateChecker) runHostCmdOutput(name string, args ...string) string {
	nsArgs := append([]string{"-t", "1", "-m", "-u", "-i", "-n", "--", name}, args...)
	cmd := exec.Command("nsenter", nsArgs...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return out.String()
}
