package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerswarm "github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// Service tasks, events, ports, rollback
// ---------------------------------------------------------------------------

func (s *Server) serviceTasks(w http.ResponseWriter, r *http.Request) {
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

	tasks, err := s.sc.Docker().TaskList(r.Context(), types.TaskListOptions{
		Filters: filters.NewArgs(filters.Arg("service", svc.ID)),
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	type TaskInfo struct {
		ID        string `json:"id"`
		NodeID    string `json:"node_id"`
		Status    string `json:"status"`
		Message   string `json:"message"`
		Image     string `json:"image"`
		Slot      int    `json:"slot"`
		CreatedAt string `json:"created_at"`
	}

	result := make([]TaskInfo, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, TaskInfo{
			ID:        t.ID,
			NodeID:    t.NodeID,
			Status:    string(t.Status.State),
			Message:   t.Status.Message,
			Image:     t.Spec.ContainerSpec.Image,
			Slot:      t.Slot,
			CreatedAt: t.CreatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) serviceEvents(w http.ResponseWriter, r *http.Request) {
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

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	since := time.Now().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	evtCh, errCh := s.sc.Docker().Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("type", "service"),
			filters.Arg("service", svc.ID),
		),
		Since: since,
	})

	type EventInfo struct {
		Action  string `json:"action"`
		Message string `json:"message"`
		Time    string `json:"time"`
	}

	var result []EventInfo
	for {
		select {
		case evt, ok := <-evtCh:
			if !ok {
				writeJSON(w, http.StatusOK, result)
				return
			}
			result = append(result, EventInfo{
				Action:  string(evt.Action),
				Message: evt.Actor.ID,
				Time:    time.Unix(evt.Time, evt.TimeNano).Format(time.RFC3339),
			})
		case err, ok := <-errCh:
			if ok && err != nil {
				// Context deadline will trigger this; that's normal
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
	}
}

func (s *Server) servicePorts(w http.ResponseWriter, r *http.Request) {
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

	type PortInfo struct {
		Protocol      string `json:"protocol"`
		TargetPort    uint32 `json:"target_port"`
		PublishedPort uint32 `json:"published_port"`
		PublishMode   string `json:"publish_mode"`
	}

	var ports []PortInfo
	if svc.Endpoint.Ports != nil {
		for _, p := range svc.Endpoint.Ports {
			ports = append(ports, PortInfo{
				Protocol:      string(p.Protocol),
				TargetPort:    p.TargetPort,
				PublishedPort: p.PublishedPort,
				PublishMode:   string(p.PublishMode),
			})
		}
	}

	if ports == nil {
		ports = []PortInfo{}
	}

	writeJSON(w, http.StatusOK, ports)
}

func (s *Server) serviceRollback(w http.ResponseWriter, r *http.Request) {
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

	if err := s.sc.RollbackService(r.Context(), svc.ID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "rolled_back"})
}

// ---------------------------------------------------------------------------
// Node label updates
// ---------------------------------------------------------------------------

func (s *Server) updateNodeLabels(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	var body struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	node, _, err := s.sc.Docker().NodeInspectWithRaw(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "node not found", nil)
		return
	}

	node.Spec.Labels = body.Labels
	if err := s.sc.Docker().NodeUpdate(r.Context(), id, node.Version, node.Spec); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"updated": id})
}

// ---------------------------------------------------------------------------
// Node availability & role
// ---------------------------------------------------------------------------

func (s *Server) updateNodeAvailability(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	var body struct {
		Availability string `json:"availability"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	var avail dockerswarm.NodeAvailability
	switch body.Availability {
	case "active":
		avail = dockerswarm.NodeAvailabilityActive
	case "drain":
		avail = dockerswarm.NodeAvailabilityDrain
	case "pause":
		avail = dockerswarm.NodeAvailabilityPause
	default:
		writeAPIError(w, http.StatusBadRequest, "bad_request", "availability must be active, drain, or pause", nil)
		return
	}

	if err := s.sc.SetNodeAvailability(r.Context(), id, avail); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"updated": id, "availability": body.Availability})
}

func (s *Server) updateNodeRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	var role dockerswarm.NodeRole
	switch body.Role {
	case "manager":
		role = dockerswarm.NodeRoleManager
	case "worker":
		role = dockerswarm.NodeRoleWorker
	default:
		writeAPIError(w, http.StatusBadRequest, "bad_request", "role must be manager or worker", nil)
		return
	}

	if err := s.sc.SetNodeRole(r.Context(), id, role); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"updated": id, "role": body.Role})
}

func (s *Server) removeNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	force := r.URL.Query().Get("force") == "true"
	if err := s.sc.RemoveNode(r.Context(), id, force); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"removed": id})
}

func (s *Server) nodeMaintenanceAction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	switch body.Action {
	case "apt_update", "apt_upgrade", "reboot":
	default:
		writeAPIError(w, http.StatusBadRequest, "bad_request", "action must be apt_update, apt_upgrade, or reboot", nil)
		return
	}

	if s.nc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "NATS not available", nil)
		return
	}

	payload, _ := json.Marshal(map[string]string{"action": body.Action, "node_id": id})
	if err := s.nc.Publish("hive.node.maintenance."+id, payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "triggered", "action": body.Action, "node_id": id})
}

// ---------------------------------------------------------------------------
// Service label updates (for Traefik routing)
// ---------------------------------------------------------------------------

func (s *Server) updateServiceLabels(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	var body struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	svc, err := s.sc.GetService(r.Context(), name)
	if err != nil || svc == nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "service not found", nil)
		return
	}

	if svc.Spec.Labels == nil {
		svc.Spec.Labels = make(map[string]string)
	}
	for k, v := range body.Labels {
		if v == "" {
			delete(svc.Spec.Labels, k)
		} else {
			svc.Spec.Labels[k] = v
		}
	}

	if err := s.sc.UpdateServiceSpec(r.Context(), svc.ID, svc.Version, svc.Spec); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"updated": name})
}

// ---------------------------------------------------------------------------
// Docker secrets management
// ---------------------------------------------------------------------------

func (s *Server) createDockerSecret(w http.ResponseWriter, r *http.Request) {
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	var body struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	resp, err := s.sc.Docker().SecretCreate(r.Context(), dockerswarm.SecretSpec{
		Annotations: dockerswarm.Annotations{Name: body.Name},
		Data:        []byte(body.Data),
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": resp.ID, "name": body.Name})
}

func (s *Server) deleteDockerSecret(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	if err := s.sc.Docker().SecretRemove(r.Context(), id); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// ---------------------------------------------------------------------------
// Docker volumes management
// ---------------------------------------------------------------------------

func (s *Server) createDockerVolume(w http.ResponseWriter, r *http.Request) {
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	var body struct {
		Name         string            `json:"name"`
		Driver       string            `json:"driver"`
		DriverOpts   map[string]string `json:"driver_opts"`
		MountType    string            `json:"mount_type"`
		RemoteHost   string            `json:"remote_host"`
		RemotePath   string            `json:"remote_path"`
		MountOptions string            `json:"mount_options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if body.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name is required", nil)
		return
	}

	switch body.MountType {
	case "nfs":
		vol, err := s.sc.CreateNFSVolume(r.Context(), body.Name, body.RemoteHost, body.RemotePath, body.MountOptions, nil)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"name": vol.Name, "driver": vol.Driver})
	default:
		vol, err := s.sc.CreateVolume(r.Context(), body.Name, body.Driver, body.DriverOpts, nil)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"name": vol.Name, "driver": vol.Driver})
	}
}

func (s *Server) deleteDockerVolume(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	if err := s.sc.RemoveVolume(r.Context(), name, false); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}

func (s *Server) listDockerVolumes(w http.ResponseWriter, r *http.Request) {
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	vols, err := s.sc.ListVolumes(r.Context(), "hive.managed=true")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	type VolumeInfo struct {
		Name       string            `json:"name"`
		Driver     string            `json:"driver"`
		Mountpoint string            `json:"mountpoint"`
		Labels     map[string]string `json:"labels"`
	}

	result := make([]VolumeInfo, 0, len(vols))
	for _, v := range vols {
		result = append(result, VolumeInfo{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Labels:     v.Labels,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// Swarm join tokens
// ---------------------------------------------------------------------------

func (s *Server) swarmJoinTokens(w http.ResponseWriter, r *http.Request) {
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	workerToken, managerToken, err := s.sc.GetSwarmJoinTokens(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	info, err := s.sc.Docker().Info(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	advertiseAddr := ""
	if info.Swarm.NodeAddr != "" {
		advertiseAddr = info.Swarm.NodeAddr + ":2377"
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"worker":         workerToken,
		"manager":        managerToken,
		"advertise_addr": advertiseAddr,
	})
}

// ---------------------------------------------------------------------------
// Direct service scale (bypasses NATS deploy worker)
// ---------------------------------------------------------------------------

func (s *Server) directScale(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}

	var body struct {
		Replicas uint64 `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
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

	if err := s.sc.ScaleService(r.Context(), svc.ID, body.Replicas); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "scaled", "service": svc.Spec.Name})
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

func (s *Server) registryStatus(w http.ResponseWriter, r *http.Request) {
	if s.sc == nil {
		writeJSON(w, http.StatusOK, map[string]any{"running": false})
		return
	}

	_, err := s.sc.GetService(r.Context(), "hive-registry")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"running": false})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"running": true})
}

func (s *Server) registryImages(w http.ResponseWriter, r *http.Request) {
	registryURL := "http://hive-registry:5000"

	type catalogResp struct {
		Repositories []string `json:"repositories"`
	}
	type tagsResp struct {
		Tags []string `json:"tags"`
	}
	type ImageInfo struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}

	catalogRes, err := http.Get(registryURL + "/v2/_catalog")
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	defer catalogRes.Body.Close()

	var catalog catalogResp
	if err := json.NewDecoder(catalogRes.Body).Decode(&catalog); err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	result := make([]ImageInfo, 0, len(catalog.Repositories))
	for _, repo := range catalog.Repositories {
		tagsRes, err := http.Get(registryURL + "/v2/" + repo + "/tags/list")
		if err != nil {
			result = append(result, ImageInfo{Name: repo, Tags: []string{}})
			continue
		}
		var tags tagsResp
		if err := json.NewDecoder(tagsRes.Body).Decode(&tags); err != nil {
			tags.Tags = []string{}
		}
		tagsRes.Body.Close()
		if tags.Tags == nil {
			tags.Tags = []string{}
		}
		result = append(result, ImageInfo{Name: repo, Tags: tags.Tags})
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) apiDeleteRegistryImage(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	name := chi.URLParam(r, "name")
	tag := chi.URLParam(r, "tag")
	registryURL := "http://hive-registry:5000"

	digestReq, err := http.NewRequest("HEAD", fmt.Sprintf("%s/v2/%s/manifests/%s", registryURL, name, tag), nil)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	digestReq.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	resp, err := http.DefaultClient.Do(digestReq)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", err.Error(), nil)
		return
	}
	_ = resp.Body.Close()
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "manifest not found", nil)
		return
	}

	delReq, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/v2/%s/manifests/%s", registryURL, name, digest), nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", err.Error(), nil)
		return
	}
	_ = delResp.Body.Close()
	if delResp.StatusCode >= 400 {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", fmt.Sprintf("registry returned %d", delResp.StatusCode), nil)
		return
	}
	s.auditLog(r, "delete", "registry_image", name+":"+tag, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------------------------------------------------------------------------
// Active Traefik routes (read from Swarm service labels)
// ---------------------------------------------------------------------------

func (s *Server) activeRoutes(w http.ResponseWriter, r *http.Request) {
	if s.sc == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	services, err := s.sc.ListServices(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	type ActiveRoute struct {
		Service      string `json:"service"`
		Domain       string `json:"domain"`
		Entrypoint   string `json:"entrypoint"`
		CertResolver string `json:"cert_resolver"`
		Port         string `json:"port"`
		Enabled      bool   `json:"enabled"`
	}

	var routes []ActiveRoute
	for _, svc := range services {
		labels := svc.Spec.Labels
		if labels == nil {
			continue
		}
		if labels["traefik.enable"] != "true" {
			continue
		}

		name := svc.Spec.Name
		route := ActiveRoute{
			Service: name,
			Enabled: true,
		}

		for k, v := range labels {
			if len(k) > len("traefik.http.routers.") && k[:len("traefik.http.routers.")] == "traefik.http.routers." {
				rest := k[len("traefik.http.routers."):]
				if idx := len(rest) - len(".rule"); idx > 0 && rest[idx:] == ".rule" {
					route.Domain = v
				}
				if idx := len(rest) - len(".entrypoints"); idx > 0 && rest[idx:] == ".entrypoints" {
					route.Entrypoint = v
				}
				if idx := len(rest) - len(".tls.certresolver"); idx > 0 && rest[idx:] == ".tls.certresolver" {
					route.CertResolver = v
				}
			}
			if len(k) > len("traefik.http.services.") && k[:len("traefik.http.services.")] == "traefik.http.services." {
				rest := k[len("traefik.http.services."):]
				if idx := len(rest) - len(".loadbalancer.server.port"); idx > 0 && rest[idx:] == ".loadbalancer.server.port" {
					route.Port = v
				}
			}
		}

		if route.Domain != "" || route.Port != "" {
			routes = append(routes, route)
		}
	}

	if routes == nil {
		routes = []ActiveRoute{}
	}
	writeJSON(w, http.StatusOK, routes)
}

// ---------------------------------------------------------------------------
// Connectivity check
// ---------------------------------------------------------------------------

func (s *Server) connectivityCheck(w http.ResponseWriter, r *http.Request) {
	port80 := checkPort(80)
	port443 := checkPort(443)

	msg := "ok"
	if !port80 || !port443 {
		msg = "some ports are unreachable"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"port_80":  port80,
		"port_443": port443,
		"message":  msg,
	})
}

func checkPort(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
