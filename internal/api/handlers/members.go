package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/store"
)

func ListOrgMembers(w http.ResponseWriter, r *http.Request) {
	s := storeFromRequest(r)
	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	members, err := s.ListOrgRoles(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if members == nil {
		members = []store.OrgRole{}
	}
	writeJSON(w, http.StatusOK, members)
}

func InviteMember(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id is required"})
		return
	}
	if body.Role == "" {
		body.Role = "viewer"
	}

	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	validRoles := map[string]bool{"owner": true, "admin": true, "deployer": true, "viewer": true}
	if !validRoles[body.Role] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role, must be one of: owner, admin, deployer, viewer"})
		return
	}

	s := storeFromRequest(r)
	or := &store.OrgRole{
		OrgID:  orgID,
		UserID: body.UserID,
		Role:   body.Role,
	}
	if err := s.CreateOrgRole(r.Context(), or); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, or)
}

func UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Role == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role is required"})
		return
	}

	validRoles := map[string]bool{"owner": true, "admin": true, "deployer": true, "viewer": true}
	if !validRoles[body.Role] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	s := storeFromRequest(r)
	if err := s.UpdateOrgRole(r.Context(), orgID, userID, body.Role); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	role, _ := s.GetOrgRole(r.Context(), orgID, userID)
	if role != nil {
		writeJSON(w, http.StatusOK, role)
	} else {
		writeJSON(w, http.StatusOK, map[string]string{"updated": userID})
	}
}

func RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	s := storeFromRequest(r)
	if err := s.DeleteOrgRole(r.Context(), orgID, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": userID})
}
