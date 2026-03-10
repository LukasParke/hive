package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/docker/docker/api/types/filters"
	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"

	swarmtypes "github.com/docker/docker/api/types/swarm"
)

func (s *Server) apiListAllStacks(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	stacks, err := s.store.ListAllStacksByOrg(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if stacks == nil {
		stacks = []store.Stack{}
	}

	projectNames := make(map[string]string)
	for _, st := range stacks {
		if _, ok := projectNames[st.ProjectID]; !ok {
			if p, err := s.store.GetProject(r.Context(), st.ProjectID); err == nil && p != nil {
				projectNames[st.ProjectID] = p.Name
			}
		}
	}

	type stackWithProject struct {
		store.Stack
		ProjectName string `json:"project_name"`
	}
	result := make([]stackWithProject, len(stacks))
	for i, st := range stacks {
		result[i] = stackWithProject{Stack: st, ProjectName: projectNames[st.ProjectID]}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) apiStackServices(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	stackId := chi.URLParam(r, "stackId")
	if _, err := s.requireStackAccess(r.Context(), stackId, user.OrgID); errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	} else if handleErr(w, err) {
		return
	}

	ctx := r.Context()
	svcs, err := s.stackServices(ctx, stackId)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, svcs)
}

type stackServiceInfo struct {
	Name     string `json:"name"`
	Replicas uint64 `json:"replicas"`
	Running  int    `json:"running"`
	Healthy  bool   `json:"healthy"`
	Image    string `json:"image"`
}

func (s *Server) stackServices(ctx context.Context, stackID string) ([]stackServiceInfo, error) {
	if s.sc == nil {
		return []stackServiceInfo{}, nil
	}
	f := filters.NewArgs()
	f.Add("label", "hive.stack_id="+stackID)
	svcs, err := s.sc.Docker().ServiceList(ctx, swarmtypes.ServiceListOptions{Filters: f})
	if err != nil {
		return nil, err
	}

	tasks, _ := s.sc.Docker().TaskList(ctx, swarmtypes.TaskListOptions{})

	var result []stackServiceInfo
	for _, svc := range svcs {
		desired := uint64(0)
		if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
			desired = *svc.Spec.Mode.Replicated.Replicas
		}
		running := 0
		for _, t := range tasks {
			if t.ServiceID == svc.ID && t.Status.State == swarmtypes.TaskStateRunning {
				running++
			}
		}
		result = append(result, stackServiceInfo{
			Name:     svc.Spec.Name,
			Replicas: desired,
			Running:  running,
			Healthy:  running >= int(desired) && desired > 0,
			Image:    svc.Spec.TaskTemplate.ContainerSpec.Image,
		})
	}
	return result, nil
}

func (s *Server) apiListStacks(w http.ResponseWriter, r *http.Request) {
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
	stacks, err := s.store.ListStacks(r.Context(), projectID)
	if handleErr(w, err) {
		return
	}
	if stacks == nil {
		stacks = []store.Stack{}
	}
	writeJSON(w, http.StatusOK, stacks)
}

func (s *Server) apiCreateStack(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Name           string `json:"name"`
		Domain         string `json:"domain"`
		ComposeContent string `json:"compose_content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if req.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name is required", nil)
		return
	}
	projectID := chi.URLParam(r, "projectId")
	if _, err := s.requireProjectAccess(r.Context(), projectID, user.OrgID); errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	} else if handleErr(w, err) {
		return
	}

	st := &store.Stack{
		ProjectID:      projectID,
		Name:           req.Name,
		Domain:         req.Domain,
		ComposeContent: req.ComposeContent,
		Status:         "pending",
	}
	if err := s.store.CreateStack(r.Context(), st); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "stack", st.ID, "")

	// Trigger stack deploy via NATS
	if s.nc != nil {
		job, _ := json.Marshal(map[string]string{
			"action":   "stack_deploy",
			"stack_id": st.ID,
			"name":     st.Name,
			"domain":   st.Domain,
		})
		_ = s.nc.Publish("hive.deploy", job)
	}

	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) apiGetStack(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	st, err := s.requireStackAccess(r.Context(), chi.URLParam(r, "stackId"), user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) apiUpdateStack(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	stackId := chi.URLParam(r, "stackId")
	st, err := s.requireStackAccess(r.Context(), stackId, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	var req struct {
		Name           string  `json:"name"`
		Domain         *string `json:"domain"`
		ComposeContent string  `json:"compose_content"`
		Status         string  `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if req.Name != "" {
		st.Name = req.Name
	}
	if req.Domain != nil {
		st.Domain = *req.Domain
	}
	if req.ComposeContent != "" {
		st.ComposeContent = req.ComposeContent
	}
	if req.Status != "" {
		st.Status = req.Status
	}
	st.ID = stackId
	if err := s.store.UpdateStack(r.Context(), st); handleErr(w, err) {
		return
	}

	// Re-deploy via NATS if content changed
	if (req.ComposeContent != "" || req.Domain != nil) && s.nc != nil {
		job, _ := json.Marshal(map[string]string{
			"action":   "stack_deploy",
			"stack_id": stackId,
			"name":     st.Name,
			"domain":   st.Domain,
		})
		_ = s.nc.Publish("hive.deploy", job)
	}

	writeJSON(w, http.StatusOK, st)
}

func (s *Server) apiDeleteStack(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	stackId := chi.URLParam(r, "stackId")
	st, err := s.requireStackAccess(r.Context(), stackId, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}

	// Trigger stack remove via NATS before deleting from store
	if s.nc != nil {
		job, _ := json.Marshal(map[string]string{
			"action":   "stack_remove",
			"stack_id": stackId,
			"name":     st.Name,
		})
		_ = s.nc.Publish("hive.deploy", job)
	}

	if err := s.store.DeleteStack(r.Context(), stackId); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "stack", stackId, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
