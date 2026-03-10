package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiListAllApps(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	apps, err := s.store.ListAllAppsByOrg(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if apps == nil {
		apps = []store.App{}
	}

	projectNames := make(map[string]string)
	for _, a := range apps {
		if _, ok := projectNames[a.ProjectID]; !ok {
			if p, err := s.store.GetProject(r.Context(), a.ProjectID); err == nil && p != nil {
				projectNames[a.ProjectID] = p.Name
			}
		}
	}

	type appWithProject struct {
		store.App
		ProjectName string `json:"project_name"`
	}
	result := make([]appWithProject, len(apps))
	for i, a := range apps {
		result[i] = appWithProject{App: a, ProjectName: projectNames[a.ProjectID]}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) apiListApps(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	if _, err := s.requireProjectAccess(r.Context(), projectID, user.OrgID); errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	} else if handleErr(w, err) {
		return
	}
	apps, err := s.store.ListApps(r.Context(), projectID)
	if handleErr(w, err) {
		return
	}
	if apps == nil {
		apps = []store.App{}
	}
	writeJSON(w, http.StatusOK, apps)
}

func (s *Server) apiCreateApp(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Name           string `json:"name"`
		DeployType     string `json:"deploy_type"`
		Image          string `json:"image"`
		GitRepo        string `json:"git_repo"`
		GitBranch      string `json:"git_branch"`
		DockerfilePath string `json:"dockerfile_path"`
		Domain         string `json:"domain"`
		Port           int    `json:"port"`
		Replicas       int    `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if req.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name is required", nil)
		return
	}
	if req.Port == 0 {
		req.Port = 3000
	}
	if req.Replicas == 0 {
		req.Replicas = 1
	}
	if req.DeployType == "" {
		req.DeployType = "image"
	}
	projectID := chi.URLParam(r, "projectId")
	if _, err := s.requireProjectAccess(r.Context(), projectID, user.OrgID); errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	} else if handleErr(w, err) {
		return
	}

	a := &store.App{
		ProjectID:      projectID,
		Name:           req.Name,
		DeployType:     req.DeployType,
		Image:          req.Image,
		GitRepo:        req.GitRepo,
		GitBranch:      req.GitBranch,
		DockerfilePath: req.DockerfilePath,
		Domain:         req.Domain,
		Port:           req.Port,
		Replicas:       req.Replicas,
	}
	if err := s.store.CreateApp(r.Context(), a); handleErr(w, err) {
		return
	}

	// Auto-deploy for image-based apps
	if a.DeployType == "image" && a.Image != "" {
		s.publishDeploy(a, "")
	}

	s.auditLog(r, "create", "app", a.ID, "")
	_ = user
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) apiGetApp(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	a, err := s.requireAppAccess(r.Context(), chi.URLParam(r, "appId"), user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}

	type appWithDNS struct {
		*store.App
		DNSStatus      string `json:"dns_status"`
		HasDNSProvider bool   `json:"has_dns_provider"`
	}
	resp := appWithDNS{App: a, DNSStatus: "none", HasDNSProvider: false}
	if a.Domain != "" {
		resp.HasDNSProvider = s.HasDefaultDNSProvider(r.Context(), user.OrgID)
		if s.HasManagedDNSRecord(r.Context(), a.ID) {
			resp.DNSStatus = "configured"
		} else if resp.HasDNSProvider {
			resp.DNSStatus = "missing"
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) apiUpdateApp(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	a, err := s.requireAppAccess(r.Context(), appId, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}

	oldDomain := a.Domain

	var req struct {
		Domain   *string `json:"domain"`
		Image    *string `json:"image"`
		Port     *int    `json:"port"`
		Replicas *int    `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if req.Domain != nil {
		a.Domain = *req.Domain
	}
	if req.Image != nil {
		a.Image = *req.Image
	}
	if req.Port != nil {
		a.Port = *req.Port
	}
	if req.Replicas != nil {
		a.Replicas = *req.Replicas
	}
	if err := s.store.UpdateApp(r.Context(), a); handleErr(w, err) {
		return
	}

	domainChanged := oldDomain != a.Domain
	hasDNS := s.HasManagedDNSRecord(r.Context(), a.ID)
	if domainChanged || (a.Domain != "" && !hasDNS) {
		orgID := user.OrgID
		s.ensureAppDNSRecord(r.Context(), a, oldDomain, orgID)
	}

	if domainChanged {
		s.publishDeploy(a, "")
	}

	s.auditLog(r, "update", "app", a.ID, "")
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) apiDeleteApp(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	a, err := s.requireAppAccess(r.Context(), appId, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}

	s.cleanupAppDNSRecords(r.Context(), appId, user.OrgID)

	if s.sc != nil {
		svc, _ := s.sc.GetService(r.Context(), "hive-app-"+a.Name)
		if svc == nil {
			svc, _ = s.sc.GetService(r.Context(), a.Name)
		}
		if svc != nil {
			_ = s.sc.RemoveService(r.Context(), svc.ID)
		}
	}

	if err := s.store.DeleteApp(r.Context(), appId); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "app", appId, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) apiDeployApp(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	a, err := s.requireAppAccess(r.Context(), appId, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}

	deployStatus := "deploying"
	appStatus := "deploying"
	if a.DeployType == "git" && a.GitRepo != "" {
		deployStatus = "building"
		appStatus = "building"
	}
	d := &store.Deployment{AppID: a.ID, Status: deployStatus}
	if err := s.store.CreateDeployment(r.Context(), d); handleErr(w, err) {
		return
	}
	_ = s.store.UpdateAppStatus(r.Context(), a.ID, appStatus)

	if a.DeployType == "git" && a.GitRepo != "" {
		s.publishBuild(a, d.ID)
	} else {
		s.publishDeploy(a, d.ID)
	}

	s.auditLog(r, "deploy", "app", a.ID, "")
	writeJSON(w, http.StatusAccepted, d)
}

func (s *Server) apiRestartApp(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	a, err := s.requireAppAccess(r.Context(), appId, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	d := &store.Deployment{AppID: a.ID, Status: "deploying"}
	if err := s.store.CreateDeployment(r.Context(), d); handleErr(w, err) {
		return
	}
	_ = s.store.UpdateAppStatus(r.Context(), a.ID, "deploying")
	s.publishDeploy(a, d.ID)
	s.auditLog(r, "restart", "app", a.ID, "")
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
}

func (s *Server) apiStopApp(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	a, err := s.requireAppAccess(r.Context(), appId, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	if s.sc != nil {
		svc, _ := s.sc.GetService(r.Context(), "hive-app-"+a.Name)
		if svc == nil {
			svc, _ = s.sc.GetService(r.Context(), a.Name)
		}
		if svc != nil {
			_ = s.sc.ScaleService(r.Context(), svc.ID, 0)
		}
	}
	_ = s.store.UpdateAppStatus(r.Context(), a.ID, "stopped")
	s.auditLog(r, "stop", "app", a.ID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Server) apiStartApp(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	a, err := s.requireAppAccess(r.Context(), appId, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	replicas := a.Replicas
	if replicas == 0 {
		replicas = 1
	}
	if s.sc != nil {
		svc, _ := s.sc.GetService(r.Context(), "hive-app-"+a.Name)
		if svc == nil {
			svc, _ = s.sc.GetService(r.Context(), a.Name)
		}
		if svc != nil {
			_ = s.sc.ScaleService(r.Context(), svc.ID, uint64(replicas))
		}
	}
	_ = s.store.UpdateAppStatus(r.Context(), a.ID, "running")
	s.auditLog(r, "start", "app", a.ID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func (s *Server) apiScaleApp(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	var req struct {
		Replicas int `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	a, err := s.requireAppAccess(r.Context(), appId, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	a.Replicas = req.Replicas
	_ = s.store.UpdateApp(r.Context(), a)
	if s.sc != nil {
		svc, _ := s.sc.GetService(r.Context(), "hive-app-"+a.Name)
		if svc == nil {
			svc, _ = s.sc.GetService(r.Context(), a.Name)
		}
		if svc != nil {
			_ = s.sc.ScaleService(r.Context(), svc.ID, uint64(req.Replicas))
		}
	}
	s.auditLog(r, "scale", "app", a.ID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "scaled"})
}

func (s *Server) apiRollbackApp(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	a, err := s.requireAppAccess(r.Context(), appId, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}
	svc, _ := s.sc.GetService(r.Context(), "hive-app-"+a.Name)
	if svc == nil {
		svc, _ = s.sc.GetService(r.Context(), a.Name)
	}
	if svc == nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "service not found", nil)
		return
	}
	if err := s.sc.RollbackService(r.Context(), svc.ID); handleErr(w, err) {
		return
	}
	s.auditLog(r, "rollback", "app", a.ID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "rolled back"})
}

func (s *Server) apiAppTasks(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	a, err := s.requireAppAccess(r.Context(), chi.URLParam(r, "appId"), user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}
	svc, _ := s.sc.GetService(r.Context(), "hive-app-"+a.Name)
	if svc == nil {
		svc, _ = s.sc.GetService(r.Context(), a.Name)
	}
	if svc == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	tasks, err := s.sc.Docker().TaskList(r.Context(), types.TaskListOptions{
		Filters: filters.NewArgs(filters.Arg("service", svc.ID)),
	})
	if handleErr(w, err) {
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

func (s *Server) apiAppEvents(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	a, err := s.requireAppAccess(r.Context(), chi.URLParam(r, "appId"), user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}
	svc, _ := s.sc.GetService(r.Context(), "hive-app-"+a.Name)
	if svc == nil {
		svc, _ = s.sc.GetService(r.Context(), a.Name)
	}
	if svc == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
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
		case _, ok := <-errCh:
			if ok {
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
	}
}

func (s *Server) apiAppPorts(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	a, err := s.requireAppAccess(r.Context(), chi.URLParam(r, "appId"), user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	if s.sc == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "swarm client not available", nil)
		return
	}
	svc, _ := s.sc.GetService(r.Context(), "hive-app-"+a.Name)
	if svc == nil {
		svc, _ = s.sc.GetService(r.Context(), a.Name)
	}
	if svc == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
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

func (s *Server) apiListDeployments(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	deployments, err := s.store.ListDeployments(r.Context(), chi.URLParam(r, "appId"))
	if handleErr(w, err) {
		return
	}
	if deployments == nil {
		deployments = []store.Deployment{}
	}
	writeJSON(w, http.StatusOK, deployments)
}

func (s *Server) apiUpdateAppResources(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	var req struct {
		CPULimit    float64 `json:"cpu_limit"`
		MemoryLimit int64   `json:"memory_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.store.UpdateAppResources(r.Context(), appId, req.CPULimit, req.MemoryLimit); handleErr(w, err) {
		return
	}
	s.auditLog(r, "update", "app", appId, "resources")
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) apiUpdateAppHealthCheck(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	var req struct {
		Path     string `json:"path"`
		Interval int    `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.store.UpdateAppHealthCheck(r.Context(), appId, req.Path, req.Interval); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) apiUpdateAppPlacement(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	var req struct {
		Constraints json.RawMessage `json:"constraints"`
		Preferences json.RawMessage `json:"preferences"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.store.UpdateAppPlacement(r.Context(), appId, []byte(req.Constraints), []byte(req.Preferences)); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) apiUpdateAppStrategy(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	var req struct {
		Strategy      string `json:"strategy"`
		Parallelism   int    `json:"parallelism"`
		Delay         string `json:"delay"`
		FailureAction string `json:"failure_action"`
		Order         string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.store.UpdateAppUpdateStrategy(r.Context(), appId, req.Strategy, req.Parallelism, req.Delay, req.FailureAction, req.Order); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) apiUpdateAppLabels(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	var req struct {
		HomepageLabels json.RawMessage `json:"homepage_labels"`
		ExtraLabels    json.RawMessage `json:"extra_labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if err := s.store.UpdateAppLabels(r.Context(), appId, []byte(req.HomepageLabels), []byte(req.ExtraLabels)); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// publishDeploy publishes a deploy job to NATS for an app.
func (s *Server) publishDeploy(a *store.App, deploymentID string) {
	payload, _ := json.Marshal(map[string]any{
		"job_id":        uuid.New().String(),
		"action":        "deploy",
		"app_id":        a.ID,
		"deployment_id": deploymentID,
		"name":          a.Name,
		"image":         a.Image,
		"domain":        a.Domain,
		"port":          a.Port,
		"replicas":      a.Replicas,
	})
	_ = s.nc.Publish("hive.deploy", payload)
}

// publishBuild publishes a build job to NATS.
func (s *Server) publishBuild(a *store.App, deploymentID string) {
	payload, _ := json.Marshal(map[string]any{
		"job_id":        uuid.New().String(),
		"app_id":        a.ID,
		"deployment_id": deploymentID,
		"name":          a.Name,
		"domain":        a.Domain,
		"deploy_type":   a.DeployType,
		"git_repo":      a.GitRepo,
		"git_branch":    a.GitBranch,
		"dockerfile":    a.DockerfilePath,
	})
	_ = s.nc.Publish("hive.build", payload)
}
