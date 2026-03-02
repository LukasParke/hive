package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/lholliger/hive/internal/agent"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/encryption"
)

const (
	natsTimeout     = 5 * time.Minute
	bootstrapNATSTO = 12 * time.Minute
)

type DeployRequest struct {
	Name            string          `json:"name"`
	BootstrapNodeID string          `json:"bootstrap_node_id"`
	MonNodes        []NodeSelection `json:"mon_nodes"`
	OSDSelections   []OSDSelection  `json:"osd_selections"`
	ReplicationSize int             `json:"replication_size"`
	CreateCephFS    bool            `json:"create_cephfs"`
	CephFSName      string          `json:"cephfs_name"`
	PublicNetwork   string          `json:"public_network"`
}

type NodeSelection struct {
	NodeID   string `json:"node_id"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
}

type OSDSelection struct {
	NodeID     string `json:"node_id"`
	Hostname   string `json:"hostname"`
	DevicePath string `json:"device_path"`
	DeviceSize int64  `json:"device_size"`
	DeviceType string `json:"device_type"`
}

type Orchestrator struct {
	nc    *nats.Conn
	store *store.Store
	log   *zap.SugaredLogger
}

func NewOrchestrator(nc *nats.Conn, s *store.Store, log *zap.SugaredLogger) *Orchestrator {
	return &Orchestrator{nc: nc, store: s, log: log}
}

func (o *Orchestrator) Deploy(ctx context.Context, req DeployRequest) (*store.CephCluster, error) {
	if req.ReplicationSize == 0 {
		req.ReplicationSize = 3
	}
	if req.CephFSName == "" {
		req.CephFSName = "hive-fs"
	}

	monHosts := make([]string, len(req.MonNodes))
	for i, n := range req.MonNodes {
		monHosts[i] = n.IP
	}

	cluster := &store.CephCluster{
		Name:            req.Name,
		Status:          "bootstrapping",
		BootstrapNodeID: req.BootstrapNodeID,
		MonHosts:        monHosts,
		PublicNetwork:   req.PublicNetwork,
		ReplicationSize: req.ReplicationSize,
	}

	if err := o.store.CreateCephCluster(ctx, cluster); err != nil {
		return nil, fmt.Errorf("create cluster record: %w", err)
	}

	go o.runDeployment(ctx, cluster, req)

	return cluster, nil
}

func (o *Orchestrator) runDeployment(ctx context.Context, cluster *store.CephCluster, req DeployRequest) {
	o.publishProgress(cluster.ID, "start", "Starting Ceph cluster deployment")

	// Step 1: Check prerequisites on bootstrap node
	o.publishProgress(cluster.ID, "prerequisites", "Checking prerequisites on bootstrap node")
	resp, err := o.sendCommand(ctx, req.BootstrapNodeID, agent.CephCommandRequest{
		Command:   "check_prerequisites",
		ClusterID: cluster.ID,
	}, natsTimeout)
	if err != nil || !resp.Success {
		o.failCluster(ctx, cluster.ID, "prerequisite check failed: "+errorMsg(err, resp))
		return
	}

	// Step 2: Install cephadm on bootstrap node
	o.publishProgress(cluster.ID, "install", "Installing cephadm on bootstrap node")
	resp, err = o.sendCommand(ctx, req.BootstrapNodeID, agent.CephCommandRequest{
		Command:   "install_cephadm",
		ClusterID: cluster.ID,
	}, natsTimeout)
	if err != nil || !resp.Success {
		o.failCluster(ctx, cluster.ID, "cephadm install failed: "+errorMsg(err, resp))
		return
	}

	// Step 3: Bootstrap Ceph cluster
	bootstrapNode := findNode(req.MonNodes, req.BootstrapNodeID)
	if bootstrapNode == nil {
		o.failCluster(ctx, cluster.ID, "bootstrap node not found in monitor node list")
		return
	}

	singleHost := "false"
	if len(req.MonNodes) == 1 {
		singleHost = "true"
	}

	o.publishProgress(cluster.ID, "bootstrap", fmt.Sprintf("Bootstrapping Ceph on %s (%s)", bootstrapNode.Hostname, bootstrapNode.IP))
	resp, err = o.sendCommand(ctx, req.BootstrapNodeID, agent.CephCommandRequest{
		Command:   "bootstrap",
		ClusterID: cluster.ID,
		Args: map[string]string{
			"mon_ip":         bootstrapNode.IP,
			"single_host":    singleHost,
			"public_network": req.PublicNetwork,
		},
	}, bootstrapNATSTO)
	if err != nil || !resp.Success {
		o.failCluster(ctx, cluster.ID, "bootstrap failed: "+errorMsg(err, resp))
		return
	}

	// Step 4: Parse bootstrap output and store encrypted config
	var bootstrapResult map[string]string
	if err := json.Unmarshal([]byte(resp.Output), &bootstrapResult); err != nil {
		o.failCluster(ctx, cluster.ID, "failed to parse bootstrap output")
		return
	}

	if conf, ok := bootstrapResult["ceph_conf"]; ok {
		if enc, err := encryption.Encrypt([]byte(conf)); err == nil {
			cluster.CephConfEncrypted = enc
		}
		cluster.FSID = extractFSIDFromConf(conf)
	}
	if keyring, ok := bootstrapResult["admin_keyring"]; ok {
		if enc, err := encryption.Encrypt([]byte(keyring)); err == nil {
			cluster.AdminKeyringEncrypted = enc
		}
	}

	cluster.Status = "expanding"
	if err := o.store.UpdateCephCluster(ctx, cluster); err != nil {
		o.log.Warnf("update cluster after bootstrap: %v", err)
	}

	// Step 5: Add additional monitor/host nodes
	for _, node := range req.MonNodes {
		if node.NodeID == req.BootstrapNodeID {
			continue
		}

		o.publishProgress(cluster.ID, "install", fmt.Sprintf("Installing cephadm on %s", node.Hostname))
		o.sendCommand(ctx, node.NodeID, agent.CephCommandRequest{
			Command:   "install_cephadm",
			ClusterID: cluster.ID,
		}, natsTimeout)

		o.publishProgress(cluster.ID, "add_host", fmt.Sprintf("Adding host %s to cluster", node.Hostname))
		resp, err = o.sendCommand(ctx, req.BootstrapNodeID, agent.CephCommandRequest{
			Command:   "add_host",
			ClusterID: cluster.ID,
			Args: map[string]string{
				"hostname": node.Hostname,
				"ip":       node.IP,
			},
		}, natsTimeout)
		if err != nil || !resp.Success {
			o.log.Warnf("add host %s failed: %s", node.Hostname, errorMsg(err, resp))
		}
	}

	// Step 6: Add OSDs
	for i, osd := range req.OSDSelections {
		o.publishProgress(cluster.ID, "add_osd", fmt.Sprintf("Adding OSD %d/%d: %s:%s", i+1, len(req.OSDSelections), osd.Hostname, osd.DevicePath))

		osdRecord := &store.CephOSD{
			ClusterID:  cluster.ID,
			NodeID:     osd.NodeID,
			Hostname:   osd.Hostname,
			DevicePath: osd.DevicePath,
			DeviceSize: osd.DeviceSize,
			DeviceType: osd.DeviceType,
			Status:     "provisioning",
		}
		if osdRecord.DeviceType == "" {
			osdRecord.DeviceType = "hdd"
		}
		o.store.CreateCephOSD(ctx, osdRecord)

		resp, err = o.sendCommand(ctx, req.BootstrapNodeID, agent.CephCommandRequest{
			Command:   "add_osd",
			ClusterID: cluster.ID,
			Args: map[string]string{
				"hostname": osd.Hostname,
				"device":   osd.DevicePath,
			},
		}, natsTimeout)
		if err != nil || !resp.Success {
			o.log.Warnf("add OSD %s:%s failed: %s", osd.Hostname, osd.DevicePath, errorMsg(err, resp))
			o.store.UpdateCephOSDStatus(ctx, osdRecord.ID, "failed", nil)
			continue
		}
		o.store.UpdateCephOSDStatus(ctx, osdRecord.ID, "active", nil)
	}

	// Step 7: Set converged memory tuning
	o.publishProgress(cluster.ID, "configure", "Applying converged cluster tuning")
	o.sendCommand(ctx, req.BootstrapNodeID, agent.CephCommandRequest{
		Command:   "set_config",
		ClusterID: cluster.ID,
		Args: map[string]string{
			"section": "mgr",
			"key":     "mgr/cephadm/autotune_memory_target_ratio",
			"value":   "0.2",
		},
	}, 30*time.Second)

	// Step 8: Set replication size
	if req.ReplicationSize > 0 && req.ReplicationSize < 3 {
		o.sendCommand(ctx, req.BootstrapNodeID, agent.CephCommandRequest{
			Command:   "set_config",
			ClusterID: cluster.ID,
			Args: map[string]string{
				"section": "global",
				"key":     "osd_pool_default_size",
				"value":   fmt.Sprintf("%d", req.ReplicationSize),
			},
		}, 30*time.Second)
	}

	// Step 9: Create CephFS if requested
	if req.CreateCephFS {
		o.publishProgress(cluster.ID, "create_cephfs", fmt.Sprintf("Creating CephFS filesystem: %s", req.CephFSName))
		resp, err = o.sendCommand(ctx, req.BootstrapNodeID, agent.CephCommandRequest{
			Command:   "create_cephfs",
			ClusterID: cluster.ID,
			Args:      map[string]string{"name": req.CephFSName},
		}, natsTimeout)
		if err != nil || !resp.Success {
			o.log.Warnf("create CephFS failed: %s", errorMsg(err, resp))
		}
	}

	// Step 10: Create default RBD pool
	o.publishProgress(cluster.ID, "create_pool", "Creating default RBD pool")
	o.sendCommand(ctx, req.BootstrapNodeID, agent.CephCommandRequest{
		Command:   "create_pool",
		ClusterID: cluster.ID,
		Args: map[string]string{
			"name":        "hive-rbd",
			"application": "rbd",
			"size":        fmt.Sprintf("%d", req.ReplicationSize),
		},
	}, natsTimeout)

	poolRecord := &store.CephPool{
		ClusterID:   cluster.ID,
		Name:        "hive-rbd",
		PGNum:       32,
		Size:        req.ReplicationSize,
		Type:        "replicated",
		Application: "rbd",
	}
	o.store.CreateCephPool(ctx, poolRecord)

	// Step 11: Auto-register StorageHost
	o.publishProgress(cluster.ID, "register", "Registering Ceph as a storage host")
	storageHost := &store.StorageHost{
		Name:             "ceph-" + cluster.Name,
		Address:          strings.Join(cluster.MonHosts, ","),
		Type:             "ceph",
		DefaultMountType: "cephfs",
		Capabilities:     json.RawMessage(`{"cephfs":true,"rbd":true}`),
		NodeLabel:        "hive.storage.ceph-" + cluster.Name + "=true",
		Status:           "active",
	}
	if err := o.store.CreateStorageHost(ctx, storageHost); err != nil {
		o.log.Warnf("auto-register StorageHost: %v", err)
	} else {
		cluster.StorageHostID = storageHost.ID
	}

	// Step 12: Final status update
	cluster.Status = "healthy"
	if err := o.store.UpdateCephCluster(ctx, cluster); err != nil {
		o.log.Warnf("final cluster update: %v", err)
	}

	o.publishProgress(cluster.ID, "complete", "Ceph cluster deployment complete")
	o.log.Infof("Ceph cluster %s deployed successfully (FSID: %s)", cluster.Name, cluster.FSID)
}

func (o *Orchestrator) DestroyCluster(ctx context.Context, clusterID string) error {
	cluster, err := o.store.GetCephCluster(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	o.store.UpdateCephClusterStatus(ctx, clusterID, "destroying")

	osds, _ := o.store.ListCephOSDs(ctx, clusterID)
	nodeIDs := map[string]bool{cluster.BootstrapNodeID: true}
	for _, osd := range osds {
		nodeIDs[osd.NodeID] = true
	}

	for nodeID := range nodeIDs {
		o.sendCommand(ctx, nodeID, agent.CephCommandRequest{
			Command:   "destroy",
			ClusterID: clusterID,
			Args:      map[string]string{"fsid": cluster.FSID},
		}, natsTimeout)
	}

	if cluster.StorageHostID != "" {
		o.store.DeleteStorageHost(ctx, cluster.StorageHostID)
	}

	return o.store.DeleteCephCluster(ctx, clusterID)
}

func (o *Orchestrator) AddOSD(ctx context.Context, clusterID string, sel OSDSelection) (*store.CephOSD, error) {
	cluster, err := o.store.GetCephCluster(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("get cluster: %w", err)
	}

	osdRecord := &store.CephOSD{
		ClusterID:  clusterID,
		NodeID:     sel.NodeID,
		Hostname:   sel.Hostname,
		DevicePath: sel.DevicePath,
		DeviceSize: sel.DeviceSize,
		DeviceType: sel.DeviceType,
		Status:     "provisioning",
	}
	if osdRecord.DeviceType == "" {
		osdRecord.DeviceType = "hdd"
	}
	if err := o.store.CreateCephOSD(ctx, osdRecord); err != nil {
		return nil, fmt.Errorf("create OSD record: %w", err)
	}

	resp, err := o.sendCommand(ctx, cluster.BootstrapNodeID, agent.CephCommandRequest{
		Command:   "add_osd",
		ClusterID: clusterID,
		Args: map[string]string{
			"hostname": sel.Hostname,
			"device":   sel.DevicePath,
		},
	}, natsTimeout)
	if err != nil || !resp.Success {
		o.store.UpdateCephOSDStatus(ctx, osdRecord.ID, "failed", nil)
		return osdRecord, fmt.Errorf("add OSD failed: %s", errorMsg(err, resp))
	}

	o.store.UpdateCephOSDStatus(ctx, osdRecord.ID, "active", nil)
	return osdRecord, nil
}

func (o *Orchestrator) RemoveOSD(ctx context.Context, clusterID, osdRecordID string) error {
	cluster, err := o.store.GetCephCluster(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	osds, err := o.store.ListCephOSDs(ctx, clusterID)
	if err != nil {
		return err
	}

	var target *store.CephOSD
	for _, osd := range osds {
		if osd.ID == osdRecordID {
			target = &osd
			break
		}
	}
	if target == nil {
		return fmt.Errorf("OSD not found")
	}

	if target.OsdID != nil {
		o.sendCommand(ctx, cluster.BootstrapNodeID, agent.CephCommandRequest{
			Command:   "remove_osd",
			ClusterID: clusterID,
			Args:      map[string]string{"osd_id": fmt.Sprintf("%d", *target.OsdID)},
		}, natsTimeout)
	}

	return o.store.DeleteCephOSD(ctx, osdRecordID)
}

func (o *Orchestrator) CreatePool(ctx context.Context, clusterID string, pool *store.CephPool) error {
	cluster, err := o.store.GetCephCluster(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	resp, err := o.sendCommand(ctx, cluster.BootstrapNodeID, agent.CephCommandRequest{
		Command:   "create_pool",
		ClusterID: clusterID,
		Args: map[string]string{
			"name":        pool.Name,
			"pg_num":      fmt.Sprintf("%d", pool.PGNum),
			"application": pool.Application,
			"size":        fmt.Sprintf("%d", pool.Size),
		},
	}, natsTimeout)
	if err != nil || !resp.Success {
		return fmt.Errorf("create pool failed: %s", errorMsg(err, resp))
	}

	pool.ClusterID = clusterID
	return o.store.CreateCephPool(ctx, pool)
}

// DiscoverDisks sends a NATS request to an agent to get block devices via metrics cache.
func (o *Orchestrator) DiscoverDisks(ctx context.Context, nodeID string) ([]agent.BlockDevice, error) {
	resp, err := o.sendCommand(ctx, nodeID, agent.CephCommandRequest{
		Command: "check_prerequisites",
	}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	_ = resp
	return nil, nil
}

func (o *Orchestrator) sendCommand(ctx context.Context, nodeID string, req agent.CephCommandRequest, timeout time.Duration) (*agent.CephCommandResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	subject := fmt.Sprintf("hive.ceph.cmd.%s", nodeID)
	msg, err := o.nc.Request(subject, data, timeout)
	if err != nil {
		return nil, fmt.Errorf("NATS request to %s: %w", nodeID, err)
	}

	var resp agent.CephCommandResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

func (o *Orchestrator) publishProgress(clusterID, step, message string) {
	ev := map[string]string{
		"cluster_id": clusterID,
		"step":       step,
		"message":    message,
		"timestamp":  fmt.Sprintf("%d", time.Now().Unix()),
	}
	data, _ := json.Marshal(ev)
	o.nc.Publish(fmt.Sprintf("hive.ceph.progress.%s", clusterID), data)
}

func (o *Orchestrator) failCluster(ctx context.Context, clusterID, message string) {
	o.log.Errorf("ceph deploy failed: %s", message)
	o.publishProgress(clusterID, "error", message)
	o.store.UpdateCephClusterStatus(ctx, clusterID, "error")
}

func findNode(nodes []NodeSelection, nodeID string) *NodeSelection {
	for i, n := range nodes {
		if n.NodeID == nodeID {
			return &nodes[i]
		}
	}
	return nil
}

func errorMsg(err error, resp *agent.CephCommandResponse) string {
	if err != nil {
		return err.Error()
	}
	if resp != nil && resp.Error != "" {
		return resp.Error
	}
	if resp != nil {
		return resp.Output
	}
	return "unknown error"
}

func extractFSIDFromConf(conf string) string {
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "fsid") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
