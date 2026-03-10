package engine

import (
	"net/http"
	"strconv"

	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiListAuditLogs(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	logs, err := s.store.ListAuditLogs(r.Context(), user.OrgID, limit)
	if handleErr(w, err) {
		return
	}
	if logs == nil {
		logs = []store.AuditLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) apiListAuditLogsFiltered(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	userID := r.URL.Query().Get("user_id")
	action := r.URL.Query().Get("action")
	resource := r.URL.Query().Get("resource")
	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	logs, err := s.store.ListAuditLogsFiltered(r.Context(), user.OrgID, userID, action, resource, limit, offset)
	if handleErr(w, err) {
		return
	}
	if logs == nil {
		logs = []store.AuditLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) apiGetAuditLogStats(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	stats, err := s.store.GetAuditLogStats(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if stats == nil {
		stats = map[string]int{}
	}
	writeJSON(w, http.StatusOK, stats)
}
