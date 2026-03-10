package engine

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiQueryLogEntries(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	if appID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "app_id is required", nil)
		return
	}
	var since, until time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		since, _ = time.Parse(time.RFC3339, v)
	}
	if v := r.URL.Query().Get("until"); v != "" {
		until, _ = time.Parse(time.RFC3339, v)
	}
	search := r.URL.Query().Get("search")
	level := r.URL.Query().Get("level")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 500
	}
	entries, err := s.store.QueryLogEntries(r.Context(), appID, since, until, search, level, limit)
	if handleErr(w, err) {
		return
	}
	if entries == nil {
		entries = []store.LogEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) apiCreateLogForwardConfig(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var lfc store.LogForwardConfig
	if err := json.NewDecoder(r.Body).Decode(&lfc); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	lfc.OrgID = user.OrgID
	if lfc.Name == "" || lfc.Type == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name and type are required", nil)
		return
	}
	if err := s.store.CreateLogForwardConfig(r.Context(), &lfc); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "log_forward_config", lfc.ID, "")
	writeJSON(w, http.StatusCreated, lfc)
}

func (s *Server) apiListLogForwardConfigs(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	configs, err := s.store.ListLogForwardConfigs(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if configs == nil {
		configs = []store.LogForwardConfig{}
	}
	writeJSON(w, http.StatusOK, configs)
}

func (s *Server) apiDeleteLogForwardConfig(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteLogForwardConfig(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "log_forward_config", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
