package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	dockerswarm "github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	hiveceph "github.com/lholliger/hive/internal/ceph"
	"github.com/lholliger/hive/internal/monitor"
)

// ---------------------------------------------------------------------------
// Swarm services
// ---------------------------------------------------------------------------

type CreateServiceRequest struct {
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Domain      string            `json:"domain"`
	Port        int               `json:"port"`
	Replicas    int               `json:"replicas"`
	Env         map[string]string `json:"env"`
	Labels      map[string]string `json:"labels"`
	Constraints []string          `json:"constraints"`
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	var req CreateServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	jobID := uuid.New().String()
	payload, _ := json.Marshal(map[string]any{
		"job_id":      jobID,
		"action":      "deploy",
		"name":        req.Name,
		"image":       req.Image,
		"domain":      req.Domain,
		"port":        req.Port,
		"replicas":    req.Replicas,
		"env":         req.Env,
		"labels":      req.Labels,
		"constraints": req.Constraints,
	})

	if err := s.nc.Publish("hive.deploy", payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID, "status": "queued"})
}

func (s *Server) updateService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	body["action"] = "deploy"
	body["name"] = name

	payload, _ := json.Marshal(body)
	if err := s.nc.Publish("hive.deploy", payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) removeService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	payload, _ := json.Marshal(map[string]string{
		"action": "remove",
		"name":   name,
	})
	if err := s.nc.Publish("hive.deploy", payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) serviceLogs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	svc, err := s.sc.GetService(r.Context(), "hive-app-"+name)
	if err != nil || svc == nil {
		svc, err = s.sc.GetService(r.Context(), name)
		if err != nil || svc == nil {
			writeAPIError(w, http.StatusNotFound, "not_found", "service not found", nil)
			return
		}
	}

	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "200"
	}

	reader, err := s.sc.ServiceLogs(r.Context(), svc.ID, tail, false)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	defer func() { _ = reader.Close() }()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "streaming not supported", nil)
		return
	}

	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			fmt.Fprintf(w, "data: %s\n\n", string(buf[:n]))
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
	}
}

// ---------------------------------------------------------------------------
// Build & Deploy
// ---------------------------------------------------------------------------

type BuildRequest struct {
	AppID        string `json:"app_id"`
	DeploymentID string `json:"deployment_id"`
	GitRepo      string `json:"git_repo"`
	GitBranch    string `json:"git_branch"`
	Image        string `json:"image"`
	Dockerfile   string `json:"dockerfile"`
	RegistryURL  string `json:"registry_url"`
	GitSourceID  string `json:"git_source_id"`
}

func (s *Server) triggerBuild(w http.ResponseWriter, r *http.Request) {
	var req BuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	jobID := uuid.New().String()
	payload, _ := json.Marshal(map[string]any{
		"job_id":        jobID,
		"app_id":        req.AppID,
		"deployment_id": req.DeploymentID,
		"git_repo":      req.GitRepo,
		"git_branch":    req.GitBranch,
		"image":         req.Image,
		"dockerfile":    req.Dockerfile,
		"registry_url":  req.RegistryURL,
		"git_source_id": req.GitSourceID,
	})

	if err := s.nc.Publish("hive.build", payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID, "status": "queued"})
}

type DeployRequest struct {
	AppID        string            `json:"app_id"`
	DeploymentID string            `json:"deployment_id"`
	Name         string            `json:"name"`
	Image        string            `json:"image"`
	Domain       string            `json:"domain"`
	Port         int               `json:"port"`
	Replicas     int               `json:"replicas"`
	Env          map[string]string `json:"env"`
}

func (s *Server) triggerDeploy(w http.ResponseWriter, r *http.Request) {
	var req DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	jobID := uuid.New().String()
	payload, _ := json.Marshal(map[string]any{
		"job_id":        jobID,
		"action":        "deploy",
		"app_id":        req.AppID,
		"deployment_id": req.DeploymentID,
		"name":          req.Name,
		"image":         req.Image,
		"domain":        req.Domain,
		"port":          req.Port,
		"replicas":      req.Replicas,
		"env":           req.Env,
	})

	if err := s.nc.Publish("hive.deploy", payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID, "status": "queued"})
}

// ---------------------------------------------------------------------------
// Backup & Restore
// ---------------------------------------------------------------------------

func (s *Server) triggerBackup(w http.ResponseWriter, r *http.Request) {
	var body json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.nc.Publish("hive.backup", body); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) triggerRestore(w http.ResponseWriter, r *http.Request) {
	var body json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.nc.Publish("hive.backup", body); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

// ---------------------------------------------------------------------------
// Nodes & Metrics
// ---------------------------------------------------------------------------

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	nodes, err := s.sc.Docker().NodeList(r.Context(), dockerswarm.NodeListOptions{})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
	})
}

// ---------------------------------------------------------------------------
// Swarm info
// ---------------------------------------------------------------------------

func (s *Server) swarmInfo(w http.ResponseWriter, r *http.Request) {
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	info, err := s.sc.Docker().Info(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"node_count": info.Swarm.Nodes,
		"managers":   info.Swarm.Managers,
		"cluster_id": info.Swarm.Cluster.ID,
	})
}

// ---------------------------------------------------------------------------
// Service Health
// ---------------------------------------------------------------------------

func (s *Server) serviceHealth(w http.ResponseWriter, _ *http.Request) {
	entries, _ := monitor.ServiceHealthCache.GetAll()
	writeJSON(w, http.StatusOK, entries)
}

// ---------------------------------------------------------------------------
// Ceph
// ---------------------------------------------------------------------------

func (s *Server) cephDeploy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name            string                   `json:"name"`
		BootstrapNodeID string                   `json:"bootstrap_node_id"`
		MonNodes        []hiveceph.NodeSelection `json:"mon_nodes"`
		OSDSelections   []hiveceph.OSDSelection  `json:"osd_selections"`
		ReplicationSize int                      `json:"replication_size"`
		CreateCephFS    bool                     `json:"create_cephfs"`
		CephFSName      string                   `json:"cephfs_name"`
		PublicNetwork   string                   `json:"public_network"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if body.Name == "" || body.BootstrapNodeID == "" || len(body.MonNodes) == 0 {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name, bootstrap_node_id, and mon_nodes are required", nil)
		return
	}
	if len(body.OSDSelections) == 0 {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "at least one OSD selection is required", nil)
		return
	}

	orch := hiveceph.NewOrchestrator(s.nc, s.store, s.log)
	cluster, err := orch.Deploy(r.Context(), hiveceph.DeployRequest{
		Name:            body.Name,
		BootstrapNodeID: body.BootstrapNodeID,
		MonNodes:        body.MonNodes,
		OSDSelections:   body.OSDSelections,
		ReplicationSize: body.ReplicationSize,
		CreateCephFS:    body.CreateCephFS,
		CephFSName:      body.CephFSName,
		PublicNetwork:   body.PublicNetwork,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "ceph deploy failed: "+err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusAccepted, cluster)
}

func (s *Server) cephHealth(w http.ResponseWriter, r *http.Request) {
	reports := hiveceph.HealthCache.GetAll()
	if len(reports) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no health data available"})
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) cephCommand(w http.ResponseWriter, r *http.Request) {
	var envelope struct {
		NodeID  string          `json:"node_id"`
		Command json.RawMessage `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if envelope.NodeID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "node_id is required", nil)
		return
	}

	subject := "hive.ceph.cmd." + envelope.NodeID
	msg, err := s.nc.Request(subject, envelope.Command, 30*time.Second)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "ceph command failed: "+err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(msg.Data)
}

// ---------------------------------------------------------------------------
// Database provisioning
// ---------------------------------------------------------------------------

func (s *Server) provisionDatabase(w http.ResponseWriter, r *http.Request) {
	var body json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.nc.Publish("hive.deploy", body); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

// ---------------------------------------------------------------------------
// Disk discovery
// ---------------------------------------------------------------------------

func (s *Server) discoverDisks(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("node_id")

	type blockDevice struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		Size       uint64 `json:"size"`
		Type       string `json:"type"`
		MountPoint string `json:"mount_point,omitempty"`
		FSType     string `json:"fs_type,omitempty"`
		Model      string `json:"model,omitempty"`
		Serial     string `json:"serial,omitempty"`
		Rotational bool   `json:"rotational"`
		Transport  string `json:"transport,omitempty"`
		Available  bool   `json:"available"`
	}

	type nodeDisks struct {
		NodeID       string        `json:"node_id"`
		Hostname     string        `json:"hostname"`
		BlockDevices []blockDevice `json:"block_devices"`
	}

	subject := "hive.discover.disks"
	if nodeID != "" {
		subject = "hive.discover.disks." + nodeID
	}

	msg, err := s.nc.Request(subject, nil, 10*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, []nodeDisks{})
		return
	}

	var result []nodeDisks
	if err := json.Unmarshal(msg.Data, &result); err != nil {
		writeJSON(w, http.StatusOK, []nodeDisks{})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// Maintenance
// ---------------------------------------------------------------------------

func (s *Server) triggerMaintenance(w http.ResponseWriter, r *http.Request) {
	var body json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.nc.Publish("hive.maintenance", body); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}
