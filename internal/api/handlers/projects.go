package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/store"
)

func CreateProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no active organization"})
		return
	}

	s := storeFromRequest(r)
	project := &store.Project{
		Name:        body.Name,
		OrgID:       orgID,
		Description: body.Description,
	}
	if err := s.CreateProject(r.Context(), project); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func ListProjects(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetOrgID(r.Context())
	s := storeFromRequest(r)
	projects, err := s.ListProjects(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "projectId")
	s := storeFromRequest(r)
	project, err := s.GetProject(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "projectId")
	s := storeFromRequest(r)
	if err := s.DeleteProject(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func storeFromRequest(r *http.Request) *store.Store {
	return middleware.GetStore(r.Context())
}
