package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/store"
)

// --- Summary ---

func (s *Server) apiUpdatesSummary(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	nodeStatuses := s.updateCache.GetAll()
	totalPending := 0
	totalSecurity := 0
	rebootRequired := 0
	for _, n := range nodeStatuses {
		totalPending += n.PendingCount
		totalSecurity += n.SecurityCount
		if n.RebootRequired {
			rebootRequired++
		}
	}

	serviceUpdates := 0
	if s.store != nil {
		statuses, err := s.store.ListServiceUpdateStatusesWithUpdates(r.Context())
		if err == nil {
			serviceUpdates = len(statuses)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"nodes_total":      len(nodeStatuses),
		"pending_updates":  totalPending,
		"security_updates": totalSecurity,
		"reboot_required":  rebootRequired,
		"service_updates":  serviceUpdates,
	})
}

// --- Node Updates ---

func (s *Server) apiUpdatesNodes(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	if s.store != nil {
		statuses, err := s.store.ListNodeUpdateStatuses(r.Context())
		if err == nil && statuses != nil {
			writeJSON(w, http.StatusOK, statuses)
			return
		}
	}

	entries := s.updateCache.GetAll()
	result := make([]any, 0, len(entries))
	for _, e := range entries {
		result = append(result, e)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) apiUpdatesNodeDetail(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	nodeID := chi.URLParam(r, "nodeId")

	if s.store != nil {
		status, err := s.store.GetNodeUpdateStatus(r.Context(), nodeID)
		if err == nil {
			writeJSON(w, http.StatusOK, status)
			return
		}
	}

	entry := s.updateCache.Get(nodeID)
	if entry == nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "no update status for node", nil)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) apiCheckNodeUpdates(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	nodeID := chi.URLParam(r, "nodeId")

	msg, err := s.nc.Request(
		fmt.Sprintf("hive.node.updates.check.%s", nodeID),
		[]byte("{}"),
		30*time.Second,
	)
	if err != nil {
		writeAPIError(w, http.StatusGatewayTimeout, "gateway_timeout", "node did not respond: "+err.Error(), nil)
		return
	}

	s.auditLog(r, "check_updates", "node", nodeID, "")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(msg.Data)
}

func (s *Server) apiApplyNodeUpdates(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	nodeID := chi.URLParam(r, "nodeId")

	var req struct {
		SecurityOnly bool     `json:"security_only"`
		Packages     []string `json:"packages,omitempty"`
		Action       string   `json:"action,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	action := req.Action
	if action == "" {
		if req.SecurityOnly {
			action = "apt_security_upgrade"
		} else {
			action = "apt_upgrade"
		}
	}

	payload, _ := json.Marshal(map[string]any{
		"node_id":  nodeID,
		"action":   action,
		"packages": req.Packages,
	})
	subject := fmt.Sprintf("hive.node.maintenance.%s", nodeID)
	if err := s.nc.Publish(subject, payload); handleErr(w, err) {
		return
	}

	if s.store != nil {
		event := &store.UpdateEvent{
			EventType:   "node_os",
			TargetType:  "node",
			TargetID:    nodeID,
			TargetName:  nodeID,
			Status:      "running",
			Details:     action,
			TriggeredBy: "manual",
		}
		_ = s.store.CreateUpdateEvent(r.Context(), event)
	}

	s.auditLog(r, "apply_updates", "node", nodeID, action)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued", "action": action})
}

func (s *Server) apiCheckAllNodeUpdates(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}

	nodes, err := s.sc.ListNodes(r.Context())
	if handleErr(w, err) {
		return
	}

	checked := 0
	for _, node := range nodes {
		hostname := node.Description.Hostname
		_ = s.nc.Publish(
			fmt.Sprintf("hive.node.updates.check.%s", hostname),
			[]byte("{}"),
		)
		checked++
	}

	s.auditLog(r, "check_updates", "cluster", "all", fmt.Sprintf("%d nodes", checked))
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "queued",
		"checked": checked,
	})
}

func (s *Server) apiApplyAllNodeUpdates(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}

	var req struct {
		SecurityOnly bool `json:"security_only"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	action := "apt_upgrade"
	if req.SecurityOnly {
		action = "apt_security_upgrade"
	}

	nodes, err := s.sc.ListNodes(r.Context())
	if handleErr(w, err) {
		return
	}

	queued := 0
	for _, node := range nodes {
		hostname := node.Description.Hostname
		payload, _ := json.Marshal(map[string]any{
			"node_id": hostname,
			"action":  action,
		})
		_ = s.nc.Publish(fmt.Sprintf("hive.node.maintenance.%s", hostname), payload)
		queued++
	}

	s.auditLog(r, "apply_updates", "cluster", "all", fmt.Sprintf("%s on %d nodes", action, queued))
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "queued",
		"queued": queued,
		"action": action,
	})
}

// --- Service Updates ---

func (s *Server) apiUpdatesServices(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	if s.store == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	statuses, err := s.store.ListServiceUpdateStatuses(r.Context())
	if handleErr(w, err) {
		return
	}
	if statuses == nil {
		statuses = []store.ServiceUpdateStatus{}
	}
	writeJSON(w, http.StatusOK, statuses)
}

func (s *Server) apiApplyServiceUpdate(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	serviceName := chi.URLParam(r, "serviceName")

	if isInfraService(serviceName) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "infrastructure services cannot be updated through this API", nil)
		return
	}

	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "store unavailable", nil)
		return
	}

	sus, err := s.store.GetServiceUpdateStatus(r.Context(), serviceName)
	if handleErr(w, err) {
		return
	}
	if !sus.UpdateAvailable {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "no update available", nil)
		return
	}

	newImage := sus.CurrentImage
	if sus.LatestVersion != "" {
		parts := splitImageTag(sus.CurrentImage)
		newImage = parts[0] + ":" + sus.LatestVersion
	}

	svc, err := s.sc.GetService(r.Context(), serviceName)
	if handleErr(w, err) {
		return
	}

	svc.Spec.TaskTemplate.ContainerSpec.Image = newImage
	if err := s.sc.UpdateService(r.Context(), svc.ID, svc.Version, svc.Spec); handleErr(w, err) {
		return
	}

	event := &store.UpdateEvent{
		EventType:       "service_image",
		TargetType:      "app",
		TargetID:        sus.AppID,
		TargetName:      serviceName,
		PreviousVersion: sus.CurrentImage,
		NewVersion:      newImage,
		Status:          "success",
		TriggeredBy:     "manual",
	}
	_ = s.store.CreateUpdateEvent(r.Context(), event)

	sus.UpdateAvailable = false
	sus.CurrentImage = newImage
	sus.CurrentDigest = sus.LatestDigest
	_ = s.store.UpsertServiceUpdateStatus(r.Context(), sus)

	s.auditLog(r, "apply_update", "service", serviceName, newImage)
	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "updated",
		"new_image": newImage,
	})
}

func (s *Server) apiApplyAllServiceUpdates(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "store unavailable", nil)
		return
	}

	statuses, err := s.store.ListServiceUpdateStatusesWithUpdates(r.Context())
	if handleErr(w, err) {
		return
	}

	updated := 0
	failed := 0
	for _, sus := range statuses {
		if isInfraService(sus.ServiceName) {
			continue
		}

		newImage := sus.CurrentImage
		if sus.LatestVersion != "" {
			parts := splitImageTag(sus.CurrentImage)
			newImage = parts[0] + ":" + sus.LatestVersion
		}

		svc, err := s.sc.GetService(r.Context(), sus.ServiceName)
		if err != nil {
			failed++
			continue
		}

		svc.Spec.TaskTemplate.ContainerSpec.Image = newImage
		if err := s.sc.UpdateService(r.Context(), svc.ID, svc.Version, svc.Spec); err != nil {
			failed++
			continue
		}

		sus.UpdateAvailable = false
		sus.CurrentImage = newImage
		_ = s.store.UpsertServiceUpdateStatus(r.Context(), &sus)
		updated++
	}

	s.auditLog(r, "apply_updates", "services", "all", fmt.Sprintf("updated=%d failed=%d", updated, failed))
	writeJSON(w, http.StatusOK, map[string]any{
		"updated": updated,
		"failed":  failed,
	})
}

// --- Update History ---

func (s *Server) apiUpdatesHistory(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	if s.store == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}

	eventType := r.URL.Query().Get("type")
	var events []store.UpdateEvent

	if eventType != "" {
		events, err = s.store.ListUpdateEventsByType(r.Context(), eventType, limit)
	} else {
		events, err = s.store.ListUpdateEvents(r.Context(), limit)
	}
	if handleErr(w, err) {
		return
	}
	if events == nil {
		events = []store.UpdateEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

// --- Update Policies ---

func (s *Server) apiListUpdatePolicies(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	if s.store == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	policies, err := s.store.ListUpdatePolicies(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if policies == nil {
		policies = []store.UpdatePolicy{}
	}
	writeJSON(w, http.StatusOK, policies)
}

func (s *Server) apiCreateUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "store unavailable", nil)
		return
	}

	var p store.UpdatePolicy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	p.OrgID = user.OrgID

	if p.TargetType == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "target_type required", nil)
		return
	}

	if err := s.store.CreateUpdatePolicy(r.Context(), &p); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "update_policy", p.ID, p.TargetType+":"+p.TargetID)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) apiUpdateUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "store unavailable", nil)
		return
	}

	policyID := chi.URLParam(r, "policyId")
	existing, err := s.store.GetUpdatePolicy(r.Context(), policyID)
	if handleErr(w, err) {
		return
	}

	var updates store.UpdatePolicy
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	existing.AutoUpdate = updates.AutoUpdate
	existing.AutoRestart = updates.AutoRestart
	existing.MaintenanceWindowStart = updates.MaintenanceWindowStart
	existing.MaintenanceWindowEnd = updates.MaintenanceWindowEnd
	existing.MaintenanceWindowDays = updates.MaintenanceWindowDays
	existing.SecurityOnly = updates.SecurityOnly
	existing.PreUpdateBackup = updates.PreUpdateBackup
	existing.NotifyOnUpdate = updates.NotifyOnUpdate

	if err := s.store.UpdateUpdatePolicy(r.Context(), existing); handleErr(w, err) {
		return
	}
	s.auditLog(r, "update", "update_policy", policyID, "")
	writeJSON(w, http.StatusOK, existing)
}

func (s *Server) apiDeleteUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "store unavailable", nil)
		return
	}

	policyID := chi.URLParam(r, "policyId")
	if err := s.store.DeleteUpdatePolicy(r.Context(), policyID); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "update_policy", policyID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Helpers ---

func splitImageTag(image string) [2]string {
	// Handle registry URLs with ports (e.g., registry:5000/image:tag)
	lastColon := -1
	for i := len(image) - 1; i >= 0; i-- {
		if image[i] == ':' {
			lastColon = i
			break
		}
		if image[i] == '/' {
			break
		}
	}
	if lastColon > 0 {
		return [2]string{image[:lastColon], image[lastColon+1:]}
	}
	return [2]string{image, "latest"}
}
