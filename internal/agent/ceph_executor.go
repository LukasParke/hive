package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	cephadmBinary      = "/usr/local/bin/cephadm"
	cephConfDir        = "/etc/ceph"
	cephConfPath       = "/etc/ceph/ceph.conf"
	cephKeyringPath    = "/etc/ceph/ceph.client.admin.keyring"
	defaultCephRelease = "reef"

	bootstrapTimeout = 10 * time.Minute
	defaultTimeout   = 5 * time.Minute
)

type CephCommandRequest struct {
	Command   string            `json:"command"`
	Args      map[string]string `json:"args"`
	ClusterID string            `json:"cluster_id,omitempty"`
}

type CephCommandResponse struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

type CephExecutor struct {
	nc     *nats.Conn
	nodeID string
	log    *zap.SugaredLogger
}

func NewCephExecutor(nc *nats.Conn, nodeID string, log *zap.SugaredLogger) *CephExecutor {
	return &CephExecutor{nc: nc, nodeID: nodeID, log: log}
}

func (ce *CephExecutor) Start(ctx context.Context) {
	subject := fmt.Sprintf("hive.ceph.cmd.%s", ce.nodeID)
	ce.nc.Subscribe(subject, func(msg *nats.Msg) {
		var req CephCommandRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			ce.reply(msg, CephCommandResponse{Error: "invalid request: " + err.Error()})
			return
		}
		ce.log.Infof("ceph command received: %s", req.Command)
		resp := ce.execute(ctx, req)
		ce.reply(msg, resp)
	})
	ce.log.Infof("ceph executor listening on %s", subject)
}

func (ce *CephExecutor) reply(msg *nats.Msg, resp CephCommandResponse) {
	data, _ := json.Marshal(resp)
	if msg.Reply != "" {
		msg.Respond(data)
	}
}

func (ce *CephExecutor) publishProgress(clusterID, step, message string) {
	if clusterID == "" {
		return
	}
	ev := map[string]string{
		"cluster_id": clusterID,
		"node_id":    ce.nodeID,
		"step":       step,
		"message":    message,
		"timestamp":  fmt.Sprintf("%d", time.Now().Unix()),
	}
	data, _ := json.Marshal(ev)
	ce.nc.Publish(fmt.Sprintf("hive.ceph.progress.%s", clusterID), data)
}

func (ce *CephExecutor) execute(ctx context.Context, req CephCommandRequest) CephCommandResponse {
	switch req.Command {
	case "install_cephadm":
		return ce.installCephadm(ctx, req)
	case "bootstrap":
		return ce.bootstrap(ctx, req)
	case "add_host":
		return ce.addHost(ctx, req)
	case "add_osd":
		return ce.addOSD(ctx, req)
	case "remove_osd":
		return ce.removeOSD(ctx, req)
	case "create_pool":
		return ce.createPool(ctx, req)
	case "create_cephfs":
		return ce.createCephFS(ctx, req)
	case "status":
		return ce.cephStatus(ctx)
	case "osd_tree":
		return ce.osdTree(ctx)
	case "device_ls":
		return ce.deviceLs(ctx)
	case "set_config":
		return ce.setConfig(ctx, req)
	case "destroy":
		return ce.destroy(ctx, req)
	case "check_prerequisites":
		return ce.checkPrerequisites(ctx)
	default:
		return CephCommandResponse{Error: "unknown command: " + req.Command}
	}
}

func (ce *CephExecutor) checkPrerequisites(ctx context.Context) CephCommandResponse {
	var missing []string

	if _, err := exec.LookPath("python3"); err != nil {
		missing = append(missing, "python3")
	}
	if _, err := exec.LookPath("lvm"); err != nil {
		if _, err := exec.LookPath("lvs"); err != nil {
			missing = append(missing, "lvm2")
		}
	}
	if _, err := exec.LookPath("docker"); err != nil {
		missing = append(missing, "docker")
	}

	if len(missing) > 0 {
		return CephCommandResponse{
			Success: false,
			Output:  "missing prerequisites: " + strings.Join(missing, ", "),
		}
	}
	return CephCommandResponse{Success: true, Output: "all prerequisites met"}
}

func (ce *CephExecutor) installCephadm(ctx context.Context, req CephCommandRequest) CephCommandResponse {
	ce.publishProgress(req.ClusterID, "install_cephadm", "checking for existing cephadm installation")

	if _, err := os.Stat(cephadmBinary); err == nil {
		out, err := runCmd(ctx, defaultTimeout, cephadmBinary, "version")
		if err == nil {
			ce.publishProgress(req.ClusterID, "install_cephadm", "cephadm already installed: "+strings.TrimSpace(out))
			return CephCommandResponse{Success: true, Output: "cephadm already installed: " + strings.TrimSpace(out)}
		}
	}

	ce.publishProgress(req.ClusterID, "install_cephadm", "downloading cephadm")

	release := req.Args["release"]
	if release == "" {
		release = defaultCephRelease
	}

	url := fmt.Sprintf("https://download.ceph.com/rpm-%s/el9/noarch/cephadm", release)
	if err := downloadFile(ctx, cephadmBinary, url); err != nil {
		distroCmd := detectDistroInstallCmd()
		if distroCmd != "" {
			ce.publishProgress(req.ClusterID, "install_cephadm", "binary download failed, trying package manager")
			out, err := runCmd(ctx, defaultTimeout, "sh", "-c", distroCmd)
			if err == nil {
				return CephCommandResponse{Success: true, Output: "cephadm installed via package manager: " + out}
			}
		}
		return CephCommandResponse{Error: "failed to install cephadm: " + err.Error()}
	}

	os.Chmod(cephadmBinary, 0755)
	ce.publishProgress(req.ClusterID, "install_cephadm", "cephadm installed successfully")

	out, _ := runCmd(ctx, defaultTimeout, cephadmBinary, "version")
	return CephCommandResponse{Success: true, Output: "cephadm installed: " + strings.TrimSpace(out)}
}

func (ce *CephExecutor) bootstrap(ctx context.Context, req CephCommandRequest) CephCommandResponse {
	monIP := req.Args["mon_ip"]
	if monIP == "" {
		return CephCommandResponse{Error: "mon_ip is required"}
	}

	ce.publishProgress(req.ClusterID, "bootstrap", fmt.Sprintf("bootstrapping Ceph cluster on %s with mon-ip %s", ce.nodeID, monIP))

	os.MkdirAll(cephConfDir, 0755)

	args := []string{
		"--docker", "bootstrap",
		"--mon-ip", monIP,
		"--skip-dashboard",
		"--skip-firewalld",
		"--output-dir", cephConfDir,
		"--output-config", cephConfPath,
		"--output-keyring", cephKeyringPath,
	}

	if singleHost := req.Args["single_host"]; singleHost == "true" {
		args = append(args, "--single-host-defaults")
	}

	if publicNetwork := req.Args["public_network"]; publicNetwork != "" {
		args = append(args, "--cluster-network", publicNetwork)
	}

	out, err := runCmd(ctx, bootstrapTimeout, cephadmBinary, args...)
	if err != nil {
		ce.publishProgress(req.ClusterID, "bootstrap", "bootstrap failed: "+err.Error())
		return CephCommandResponse{Error: "bootstrap failed: " + err.Error(), Output: out}
	}

	ce.publishProgress(req.ClusterID, "bootstrap", "reading cluster configuration")

	result := map[string]string{"bootstrap_output": out}
	if confData, err := os.ReadFile(cephConfPath); err == nil {
		result["ceph_conf"] = string(confData)
	}
	if keyData, err := os.ReadFile(cephKeyringPath); err == nil {
		result["admin_keyring"] = string(keyData)
	}
	if pubKey, err := os.ReadFile(filepath.Join(cephConfDir, "ceph.pub")); err == nil {
		result["ssh_public_key"] = string(pubKey)
	}

	resultJSON, _ := json.Marshal(result)

	ce.publishProgress(req.ClusterID, "bootstrap", "Ceph cluster bootstrapped successfully")
	return CephCommandResponse{Success: true, Output: string(resultJSON)}
}

func (ce *CephExecutor) addHost(ctx context.Context, req CephCommandRequest) CephCommandResponse {
	hostname := req.Args["hostname"]
	ip := req.Args["ip"]
	if hostname == "" || ip == "" {
		return CephCommandResponse{Error: "hostname and ip are required"}
	}

	ce.publishProgress(req.ClusterID, "add_host", fmt.Sprintf("adding host %s (%s) to cluster", hostname, ip))

	args := []string{"shell", "--", "ceph", "orch", "host", "add", hostname, ip}

	labels := req.Args["labels"]
	if labels != "" {
		for _, l := range strings.Split(labels, ",") {
			args = append(args, "--labels", l)
		}
	}

	out, err := runCephadmShell(ctx, defaultTimeout, args...)
	if err != nil {
		return CephCommandResponse{Error: "add host failed: " + err.Error(), Output: out}
	}

	ce.publishProgress(req.ClusterID, "add_host", fmt.Sprintf("host %s added successfully", hostname))
	return CephCommandResponse{Success: true, Output: out}
}

func (ce *CephExecutor) addOSD(ctx context.Context, req CephCommandRequest) CephCommandResponse {
	hostname := req.Args["hostname"]
	device := req.Args["device"]
	if hostname == "" || device == "" {
		return CephCommandResponse{Error: "hostname and device are required"}
	}

	ce.publishProgress(req.ClusterID, "add_osd", fmt.Sprintf("adding OSD %s:%s", hostname, device))

	out, err := runCephadmShell(ctx, defaultTimeout,
		"shell", "--", "ceph", "orch", "daemon", "add", "osd",
		fmt.Sprintf("%s:%s", hostname, device),
	)
	if err != nil {
		return CephCommandResponse{Error: "add OSD failed: " + err.Error(), Output: out}
	}

	ce.publishProgress(req.ClusterID, "add_osd", fmt.Sprintf("OSD %s:%s added successfully", hostname, device))
	return CephCommandResponse{Success: true, Output: out}
}

func (ce *CephExecutor) removeOSD(ctx context.Context, req CephCommandRequest) CephCommandResponse {
	osdID := req.Args["osd_id"]
	if osdID == "" {
		return CephCommandResponse{Error: "osd_id is required"}
	}

	out, err := runCephadmShell(ctx, defaultTimeout,
		"shell", "--", "ceph", "orch", "osd", "rm", osdID,
	)
	if err != nil {
		return CephCommandResponse{Error: "remove OSD failed: " + err.Error(), Output: out}
	}
	return CephCommandResponse{Success: true, Output: out}
}

func (ce *CephExecutor) createPool(ctx context.Context, req CephCommandRequest) CephCommandResponse {
	name := req.Args["name"]
	if name == "" {
		return CephCommandResponse{Error: "name is required"}
	}

	pgNum := req.Args["pg_num"]
	if pgNum == "" {
		pgNum = "32"
	}

	ce.publishProgress(req.ClusterID, "create_pool", fmt.Sprintf("creating pool %s", name))

	out, err := runCephadmShell(ctx, defaultTimeout,
		"shell", "--", "ceph", "osd", "pool", "create", name, pgNum,
	)
	if err != nil {
		return CephCommandResponse{Error: "create pool failed: " + err.Error(), Output: out}
	}

	appName := req.Args["application"]
	if appName == "" {
		appName = "rbd"
	}
	runCephadmShell(ctx, defaultTimeout,
		"shell", "--", "ceph", "osd", "pool", "application", "enable", name, appName,
	)

	size := req.Args["size"]
	if size != "" {
		runCephadmShell(ctx, defaultTimeout,
			"shell", "--", "ceph", "osd", "pool", "set", name, "size", size,
		)
	}

	return CephCommandResponse{Success: true, Output: out}
}

func (ce *CephExecutor) createCephFS(ctx context.Context, req CephCommandRequest) CephCommandResponse {
	name := req.Args["name"]
	if name == "" {
		name = "hive-fs"
	}

	ce.publishProgress(req.ClusterID, "create_cephfs", fmt.Sprintf("creating CephFS %s", name))

	out, err := runCephadmShell(ctx, defaultTimeout,
		"shell", "--", "ceph", "fs", "volume", "create", name,
	)
	if err != nil {
		return CephCommandResponse{Error: "create CephFS failed: " + err.Error(), Output: out}
	}

	ce.publishProgress(req.ClusterID, "create_cephfs", fmt.Sprintf("CephFS %s created successfully", name))
	return CephCommandResponse{Success: true, Output: out}
}

func (ce *CephExecutor) cephStatus(ctx context.Context) CephCommandResponse {
	out, err := runCephadmShell(ctx, 30*time.Second,
		"shell", "--", "ceph", "status", "-f", "json",
	)
	if err != nil {
		return CephCommandResponse{Error: "ceph status failed: " + err.Error(), Output: out}
	}
	return CephCommandResponse{Success: true, Output: out}
}

func (ce *CephExecutor) osdTree(ctx context.Context) CephCommandResponse {
	out, err := runCephadmShell(ctx, 30*time.Second,
		"shell", "--", "ceph", "osd", "tree", "-f", "json",
	)
	if err != nil {
		return CephCommandResponse{Error: "osd tree failed: " + err.Error(), Output: out}
	}
	return CephCommandResponse{Success: true, Output: out}
}

func (ce *CephExecutor) deviceLs(ctx context.Context) CephCommandResponse {
	out, err := runCephadmShell(ctx, 30*time.Second,
		"shell", "--", "ceph", "orch", "device", "ls", "--format", "json",
	)
	if err != nil {
		return CephCommandResponse{Error: "device ls failed: " + err.Error(), Output: out}
	}
	return CephCommandResponse{Success: true, Output: out}
}

func (ce *CephExecutor) setConfig(ctx context.Context, req CephCommandRequest) CephCommandResponse {
	key := req.Args["key"]
	value := req.Args["value"]
	section := req.Args["section"]
	if key == "" || value == "" {
		return CephCommandResponse{Error: "key and value are required"}
	}
	if section == "" {
		section = "global"
	}

	out, err := runCephadmShell(ctx, 30*time.Second,
		"shell", "--", "ceph", "config", "set", section, key, value,
	)
	if err != nil {
		return CephCommandResponse{Error: "set config failed: " + err.Error(), Output: out}
	}
	return CephCommandResponse{Success: true, Output: out}
}

func (ce *CephExecutor) destroy(ctx context.Context, req CephCommandRequest) CephCommandResponse {
	ce.publishProgress(req.ClusterID, "destroy", "removing Ceph from this node")

	fsid := req.Args["fsid"]
	if fsid == "" {
		if confData, err := os.ReadFile(cephConfPath); err == nil {
			for _, line := range strings.Split(string(confData), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "fsid") {
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						fsid = strings.TrimSpace(parts[1])
					}
				}
			}
		}
	}

	if fsid != "" {
		runCmd(ctx, defaultTimeout, cephadmBinary, "--docker", "rm-cluster", "--fsid", fsid, "--force")
	}

	os.RemoveAll(cephConfDir)

	ce.publishProgress(req.ClusterID, "destroy", "Ceph removed from this node")
	return CephCommandResponse{Success: true, Output: "ceph removed from node"}
}

func runCephadmShell(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	fullArgs := append([]string{"--docker"}, args...)
	return runCmd(ctx, timeout, cephadmBinary, fullArgs...)
}

func runCmd(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if output == "" {
		output = stderr.String()
	}
	if err != nil {
		combined := output
		if errStr := stderr.String(); errStr != "" && errStr != output {
			combined = output + "\n" + errStr
		}
		return combined, fmt.Errorf("%s: %w", strings.TrimSpace(combined), err)
	}
	return output, nil
}

func downloadFile(ctx context.Context, dest, url string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	os.MkdirAll(filepath.Dir(dest), 0755)
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func detectDistroInstallCmd() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if _, err := exec.LookPath("apt"); err == nil {
		return "apt-get update && apt-get install -y cephadm"
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		return "dnf install -y cephadm"
	}
	if _, err := exec.LookPath("zypper"); err == nil {
		return "zypper install -y cephadm"
	}
	return ""
}
