package engine

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiCreatePreviewDeployment(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	a, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}

	var pd store.PreviewDeployment
	if err := json.NewDecoder(r.Body).Decode(&pd); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	pd.AppID = appID
	pd.Status = "pending"
	if pd.Branch == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "branch is required", nil)
		return
	}

	previewName := fmt.Sprintf("hive-preview-%s-%s", a.Name, pd.Branch)
	pd.ServiceName = previewName

	if err := s.store.CreatePreviewDeployment(r.Context(), &pd); handleErr(w, err) {
		return
	}

	if s.nc != nil && a.GitRepo != "" {
		payload, _ := json.Marshal(map[string]any{
			"job_id":        uuid.New().String(),
			"app_id":        a.ID,
			"deployment_id": pd.ID,
			"name":          previewName,
			"git_repo":      a.GitRepo,
			"git_branch":    pd.Branch,
			"dockerfile":    a.DockerfilePath,
			"is_preview":    true,
		})
		_ = s.nc.Publish("hive.build", payload)
	}

	s.auditLog(r, "create", "preview_deployment", pd.ID, pd.Branch)
	writeJSON(w, http.StatusCreated, pd)
}

func (s *Server) apiListPreviewDeployments(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	previews, err := s.store.ListPreviewDeployments(r.Context(), appID)
	if handleErr(w, err) {
		return
	}
	if previews == nil {
		previews = []store.PreviewDeployment{}
	}
	writeJSON(w, http.StatusOK, previews)
}

func (s *Server) apiDeletePreviewDeployment(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "previewId")

	pd, _ := s.store.GetPreviewDeployment(r.Context(), id)
	if pd != nil && pd.ServiceName != "" && s.nc != nil {
		payload, _ := json.Marshal(map[string]any{
			"job_id": uuid.New().String(),
			"action": "remove",
			"name":   pd.ServiceName,
		})
		_ = s.nc.Publish("hive.deploy", payload)
	}

	if err := s.store.DeletePreviewDeployment(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "preview_deployment", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
