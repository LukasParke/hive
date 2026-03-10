package engine

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiListOrgRoles(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		orgID = r.URL.Query().Get("org_id")
	}
	if orgID == "" {
		orgID = user.OrgID
	}
	if orgID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "org_id is required", nil)
		return
	}
	roles, err := s.store.ListOrgRoles(r.Context(), orgID)
	if handleErr(w, err) {
		return
	}
	if roles == nil {
		roles = []store.OrgRole{}
	}
	writeJSON(w, http.StatusOK, roles)
}

func (s *Server) apiCreateOrgRole(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	orgID := chi.URLParam(r, "orgId")
	if orgID == "" {
		orgID = r.URL.Query().Get("org_id")
	}
	if orgID == "" {
		orgID = user.OrgID
	}
	var or store.OrgRole
	if err := json.NewDecoder(r.Body).Decode(&or); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if orgID != "" {
		or.OrgID = orgID
	}
	if or.OrgID == "" || or.UserID == "" || or.Role == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "org_id, user_id and role are required", nil)
		return
	}
	if err := s.store.CreateOrgRole(r.Context(), &or); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "org_role", or.ID, "")
	writeJSON(w, http.StatusCreated, or)
}

func (s *Server) apiUpdateOrgRole(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	orgID := chi.URLParam(r, "orgId")
	userID := chi.URLParam(r, "userId")
	if orgID == "" {
		orgID = r.URL.Query().Get("org_id")
	}
	if orgID == "" {
		orgID = user.OrgID
	}
	if userID == "" {
		userID = r.URL.Query().Get("user_id")
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if orgID == "" || userID == "" || req.Role == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "org_id, user_id and role are required", nil)
		return
	}
	if err := s.store.UpdateOrgRole(r.Context(), orgID, userID, req.Role); handleErr(w, err) {
		return
	}
	s.auditLog(r, "update", "org_role", userID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) apiDeleteOrgRole(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	orgID := chi.URLParam(r, "orgId")
	userID := chi.URLParam(r, "userId")
	if orgID == "" {
		orgID = r.URL.Query().Get("org_id")
	}
	if orgID == "" {
		orgID = user.OrgID
	}
	if userID == "" {
		userID = r.URL.Query().Get("user_id")
	}
	if orgID == "" || userID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "org_id and user_id are required", nil)
		return
	}
	if err := s.store.DeleteOrgRole(r.Context(), orgID, userID); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "org_role", userID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
