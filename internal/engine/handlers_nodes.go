package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	dockerswarm "github.com/docker/docker/api/types/swarm"

	"github.com/go-chi/chi/v5"
)

func (s *Server) apiListNodes(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	nodes, err := s.sc.ListNodes(r.Context())
	if handleErr(w, err) {
		return
	}
	worker, manager, _ := s.sc.GetSwarmJoinTokens(r.Context())
	var tokens map[string]string
	if worker != "" || manager != "" {
		tokens = map[string]string{"worker": worker, "manager": manager}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":       nodes,
		"join_tokens": tokens,
	})
}

func (s *Server) apiGetNodeLabels(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	node, err := s.sc.GetNode(r.Context(), chi.URLParam(r, "nodeId"))
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, node.Spec.Labels)
}

func (s *Server) apiUpdateNodeLabelsV1(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.sc.UpdateNodeLabels(r.Context(), chi.URLParam(r, "nodeId"), req.Labels); handleErr(w, err) {
		return
	}
	s.auditLog(r, "update", "node", chi.URLParam(r, "nodeId"), "labels")
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) apiUpdateNodeAvailabilityV1(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Availability string `json:"availability"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.sc.SetNodeAvailability(r.Context(), chi.URLParam(r, "nodeId"), dockerswarm.NodeAvailability(req.Availability)); handleErr(w, err) {
		return
	}
	s.auditLog(r, "update", "node", chi.URLParam(r, "nodeId"), "availability:"+req.Availability)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) apiUpdateNodeRoleV1(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.sc.SetNodeRole(r.Context(), chi.URLParam(r, "nodeId"), dockerswarm.NodeRole(req.Role)); handleErr(w, err) {
		return
	}
	s.auditLog(r, "update", "node", chi.URLParam(r, "nodeId"), "role:"+req.Role)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) apiNodeMaintenance(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	nodeID := chi.URLParam(r, "nodeId")
	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"node_id": nodeID,
		"action":  req.Action,
	})
	subject := fmt.Sprintf("hive.node.maintenance.%s", nodeID)
	if err := s.nc.Publish(subject, payload); handleErr(w, err) {
		return
	}
	s.auditLog(r, "maintenance", "node", nodeID, req.Action)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) apiSystemStatus(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	info, err := s.sc.Docker().Info(r.Context())
	if handleErr(w, err) {
		return
	}

	role := "worker"
	if info.Swarm.ControlAvailable {
		role = "manager"
	}

	natsStatus := "disconnected"
	if s.nc != nil && s.nc.IsConnected() {
		natsStatus = "connected"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"role":       role,
		"node_count": info.Swarm.Nodes,
		"multi_node": info.Swarm.Nodes > 1,
		"nats":       natsStatus,
	})
}

func (s *Server) apiNodeContainers(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	nodeID := chi.URLParam(r, "nodeId")
	all := s.fetchTopContainers(r.Context(), 0)
	var result []containerMetric
	for _, c := range all {
		if c.Instance == nodeID || strings.Contains(c.Instance, nodeID) {
			result = append(result, c)
		}
	}
	if result == nil {
		result = []containerMetric{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) apiMetricsCluster(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	summary := s.fetchClusterSummary(r.Context())
	if summary == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) apiMetricsNodes(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	nodes := s.fetchNodeMetrics(r.Context())
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) apiNodeMetricsHistory(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	hostname := r.URL.Query().Get("hostname")
	if hostname == "" {
		hostname = chi.URLParam(r, "nodeId")
	}
	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "1h"
	}
	d, err := time.ParseDuration(rangeStr)
	if err != nil {
		d = time.Hour
	}
	history := s.fetchNodeHistory(r.Context(), hostname, int(d.Seconds()))
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) apiMetricsServices(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	ctx := r.Context()
	docker := s.sc.Docker()

	services, err := docker.ServiceList(ctx, dockerswarm.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "hive.managed=true")),
	})
	if handleErr(w, err) {
		return
	}

	nodes, _ := docker.NodeList(ctx, dockertypes.NodeListOptions{})
	nodeHostname := make(map[string]string, len(nodes))
	var activeNodes uint64
	for _, n := range nodes {
		nodeHostname[n.ID] = n.Description.Hostname
		if n.Status.State == dockerswarm.NodeStateReady && n.Spec.Availability == dockerswarm.NodeAvailabilityActive {
			activeNodes++
		}
	}

	type serviceHealth struct {
		ServiceName string   `json:"service_name"`
		Replicas    uint64   `json:"replicas"`
		Running     uint64   `json:"running"`
		Healthy     bool     `json:"healthy"`
		IsGlobal    bool     `json:"is_global"`
		Nodes       []string `json:"nodes"`
	}

	results := make([]serviceHealth, 0, len(services))
	for _, svc := range services {
		isGlobal := svc.Spec.Mode.Global != nil
		desired := uint64(0)
		if isGlobal {
			desired = activeNodes
		} else if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
			desired = *svc.Spec.Mode.Replicated.Replicas
		}

		tasks, err := docker.TaskList(ctx, dockerswarm.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("service", svc.ID),
				filters.Arg("desired-state", "running"),
			),
		})
		if err != nil {
			continue
		}

		running := uint64(0)
		var taskNodes []string
		for _, t := range tasks {
			if t.Status.State == "running" {
				running++
				if hostname, ok := nodeHostname[t.NodeID]; ok {
					taskNodes = append(taskNodes, hostname)
				}
			}
		}
		if taskNodes == nil {
			taskNodes = []string{}
		}

		results = append(results, serviceHealth{
			ServiceName: svc.Spec.Name,
			Replicas:    desired,
			Running:     running,
			Healthy:     running >= desired && desired > 0,
			IsGlobal:    isGlobal,
			Nodes:       taskNodes,
		})
	}
	writeJSON(w, http.StatusOK, results)
}
