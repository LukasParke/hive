package engine

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiCreateServiceLink(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	if appID == "" {
		appID = r.URL.Query().Get("app_id")
	}
	var sl store.ServiceLink
	if err := json.NewDecoder(r.Body).Decode(&sl); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	sl.SourceAppID = appID
	if sl.SourceAppID == "" {
		sl.SourceAppID = sl.TargetAppID
	}
	if (sl.TargetAppID == "" && sl.TargetDatabaseID == "") || sl.SourceAppID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "source_app_id and (target_app_id or target_database_id) are required", nil)
		return
	}
	if err := s.store.CreateServiceLink(r.Context(), &sl); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "service_link", sl.ID, "")
	writeJSON(w, http.StatusCreated, sl)
}

func (s *Server) apiListServiceLinks(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	if appID == "" {
		appID = r.URL.Query().Get("app_id")
	}
	if appID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "app_id is required", nil)
		return
	}
	links, err := s.store.ListServiceLinks(r.Context(), appID)
	if handleErr(w, err) {
		return
	}
	if links == nil {
		links = []store.ServiceLink{}
	}
	writeJSON(w, http.StatusOK, links)
}

func (s *Server) apiDeleteServiceLink(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteServiceLink(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "service_link", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
