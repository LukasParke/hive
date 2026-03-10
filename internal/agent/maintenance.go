package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

type UpdateProgress struct {
	NodeID    string `json:"node_id"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	Output    string `json:"output"`
	Progress  int    `json:"progress"`
	Timestamp int64  `json:"timestamp"`
}

type MaintenanceHandler struct {
	nc     *nats.Conn
	nodeID string
	log    *zap.SugaredLogger
}

func NewMaintenanceHandler(nc *nats.Conn, nodeID string, log *zap.SugaredLogger) *MaintenanceHandler {
	return &MaintenanceHandler{nc: nc, nodeID: nodeID, log: log}
}

func (m *MaintenanceHandler) Start(ctx context.Context) {
	subject := "hive.node.maintenance." + m.nodeID

	sub, err := m.nc.Subscribe(subject, func(msg *nats.Msg) {
		var req struct {
			Action   string   `json:"action"`
			NodeID   string   `json:"node_id"`
			Packages []string `json:"packages,omitempty"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			m.log.Warnf("invalid maintenance request: %v", err)
			return
		}
		m.log.Infof("maintenance action received: %s", req.Action)
		m.handleAction(req.Action, req.Packages)
	})
	if err != nil {
		m.log.Errorf("failed to subscribe to %s: %v", subject, err)
		return
	}

	m.log.Infof("listening for maintenance commands on %s", subject)
	<-ctx.Done()
	_ = sub.Unsubscribe()
}

func (m *MaintenanceHandler) handleAction(action string, packages []string) {
	m.publishProgress(action, "running", "Starting action: "+action, 0)

	switch action {
	case "apt_update":
		m.streamCmd(action, "apt-get", "update", "-y")
	case "apt_upgrade":
		m.streamCmd(action, "apt-get", "update", "-y")
		m.streamCmd(action, "apt-get", "upgrade", "-y")
	case "apt_dist_upgrade":
		m.streamCmd(action, "apt-get", "update", "-y")
		m.streamCmd(action, "apt-get", "dist-upgrade", "-y")
	case "apt_security_upgrade":
		m.streamCmd(action, "apt-get", "update", "-y")
		m.streamCmd(action, "unattended-upgrades", "--minimal_upgrade_steps")
	case "apt_install":
		if len(packages) > 0 {
			args := append([]string{"install", "-y"}, packages...)
			m.streamCmd(action, "apt-get", args...)
		}
	case "dnf_upgrade":
		m.streamCmd(action, "dnf", "upgrade", "-y")
	case "yum_upgrade":
		m.streamCmd(action, "yum", "update", "-y")
	case "pacman_upgrade":
		m.streamCmd(action, "pacman", "-Syu", "--noconfirm")
	case "reboot":
		m.publishProgress(action, "running", "Rebooting node...", 90)
		m.runCmd("reboot")
		m.publishProgress(action, "completed", "Reboot initiated", 100)
		return
	default:
		m.log.Warnf("unknown maintenance action: %s", action)
		m.publishProgress(action, "failed", "Unknown action: "+action, 0)
		return
	}

	m.publishProgress(action, "completed", fmt.Sprintf("Action %s completed successfully", action), 100)

	// Trigger a fresh update check after applying updates
	_ = m.nc.Publish("hive.node.updates.check."+m.nodeID, []byte("{}"))
}

func (m *MaintenanceHandler) publishProgress(action, status, output string, progress int) {
	p := UpdateProgress{
		NodeID:    m.nodeID,
		Action:    action,
		Status:    status,
		Output:    output,
		Progress:  progress,
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(p)
	_ = m.nc.Publish("hive.node.updates.progress."+m.nodeID, data)
}

func (m *MaintenanceHandler) streamCmd(action, name string, args ...string) {
	m.log.Infof("running: %s %v", name, args)

	nsenterArgs := append([]string{"-t", "1", "-m", "-u", "-i", "-n", "--", name}, args...)
	cmd := exec.Command("nsenter", nsenterArgs...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.publishProgress(action, "failed", "Failed to create stdout pipe: "+err.Error(), 0)
		return
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		m.publishProgress(action, "failed", "Failed to start command: "+err.Error(), 0)
		return
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		m.publishProgress(action, "running", line, -1)
	}

	if err := cmd.Wait(); err != nil {
		errMsg := fmt.Sprintf("Command %s failed: %v", name, err)
		m.log.Error(errMsg)
		m.publishProgress(action, "failed", errMsg, 0)
		return
	}
	m.log.Infof("command %s completed successfully (%d lines)", name, lineCount)
}

func (m *MaintenanceHandler) runCmd(name string, args ...string) {
	m.log.Infof("running: %s %v", name, args)

	nsenterArgs := append([]string{"-t", "1", "-m", "-u", "-i", "-n", "--", name}, args...)
	cmd := exec.Command("nsenter", nsenterArgs...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	output, err := cmd.CombinedOutput()
	if err != nil {
		m.log.Errorf("command %s failed: %v\n%s", name, err, string(output))
		return
	}
	m.log.Infof("command %s completed: %s", name, strings.TrimSpace(string(output)))
}
