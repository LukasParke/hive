package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/store"
)

func CreateServiceLink(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	projectID := chi.URLParam(r, "projectId")

	s := storeFromRequest(r)
	_, err := s.GetApp(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	var body struct {
		TargetAppID      string `json:"target_app_id"`
		TargetDatabaseID string `json:"target_database_id"`
		EnvPrefix        string `json:"env_prefix"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.EnvPrefix == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "env_prefix is required"})
		return
	}
	if body.TargetAppID == "" && body.TargetDatabaseID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target_app_id or target_database_id is required"})
		return
	}
	if body.TargetAppID != "" && body.TargetDatabaseID != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot specify both target_app_id and target_database_id"})
		return
	}

	sl := &store.ServiceLink{
		SourceAppID:      appID,
		TargetAppID:      body.TargetAppID,
		TargetDatabaseID: body.TargetDatabaseID,
		EnvPrefix:        body.EnvPrefix,
	}
	if err := s.CreateServiceLink(r.Context(), sl); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = projectID
	writeJSON(w, http.StatusCreated, sl)
}

func ListServiceLinks(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)

	links, err := s.ListServiceLinks(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if links == nil {
		links = []store.ServiceLink{}
	}
	writeJSON(w, http.StatusOK, links)
}

func DeleteServiceLink(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	linkID := chi.URLParam(r, "linkId")
	s := storeFromRequest(r)

	links, err := s.ListServiceLinks(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, sl := range links {
		if sl.ID == linkID {
			if err := s.DeleteServiceLink(r.Context(), linkID); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"deleted": linkID})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "link not found"})
}
