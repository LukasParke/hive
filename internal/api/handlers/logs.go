package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/store"
)

func QueryAppLogs(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)

	var since, until time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			since = t
		}
	}
	if v := r.URL.Query().Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			until = t
		}
	}
	search := r.URL.Query().Get("search")
	level := r.URL.Query().Get("level")
	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}

	entries, err := s.QueryLogEntries(r.Context(), appID, since, until, search, level, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []store.LogEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func GetSystemLogs(w http.ResponseWriter, r *http.Request) {
	s := storeFromRequest(r)

	var since, until time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			since = t
		}
	}
	if v := r.URL.Query().Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			until = t
		}
	}
	search := r.URL.Query().Get("search")
	level := r.URL.Query().Get("level")
	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}

	entries, err := s.QueryLogEntries(r.Context(), "system", since, until, search, level, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []store.LogEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func ListLogForwards(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no active organization"})
		return
	}
	s := storeFromRequest(r)
	configs, err := s.ListLogForwardConfigs(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if configs == nil {
		configs = []store.LogForwardConfig{}
	}
	writeJSON(w, http.StatusOK, configs)
}

func CreateLogForward(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no active organization"})
		return
	}
	var body struct {
		Name   string                 `json:"name"`
		Type   string                 `json:"type"`
		Config map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.Type == "" {
		body.Type = "webhook"
	}
	configBytes, _ := json.Marshal(body.Config)
	s := storeFromRequest(r)
	lfc := &store.LogForwardConfig{
		OrgID:           orgID,
		Name:            body.Name,
		Type:            body.Type,
		ConfigEncrypted: configBytes,
		Enabled:         true,
	}
	if err := s.CreateLogForwardConfig(r.Context(), lfc); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, lfc)
}

func DeleteLogForward(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "forwardId")
	s := storeFromRequest(r)
	if err := s.DeleteLogForwardConfig(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}
