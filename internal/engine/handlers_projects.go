package engine

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiListProjects(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	projects, err := s.store.ListProjects(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if projects == nil {
		projects = []store.Project{}
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) apiCreateProject(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if req.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name is required", nil)
		return
	}
	p := &store.Project{Name: req.Name, OrgID: user.OrgID, Description: req.Description}
	if err := s.store.CreateProject(r.Context(), p); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "project", p.ID, "")
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) apiGetProject(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	p, err := s.requireProjectAccess(r.Context(), chi.URLParam(r, "projectId"), user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) apiDeleteProject(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "projectId")
	if _, err := s.requireProjectAccess(r.Context(), id, user.OrgID); errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	} else if handleErr(w, err) {
		return
	}
	if err := s.store.DeleteProject(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "project", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
