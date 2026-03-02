package handlers

import (
	"net/http"
	"strconv"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/store"
)

func ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	s := storeFromRequest(r)
	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	userID := r.URL.Query().Get("user_id")
	action := r.URL.Query().Get("action")
	resource := r.URL.Query().Get("resource")
	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	logs, err := s.ListAuditLogsFiltered(r.Context(), orgID, userID, action, resource, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if logs == nil {
		logs = []store.AuditLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

func GetAuditLogStats(w http.ResponseWriter, r *http.Request) {
	s := storeFromRequest(r)
	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	stats, err := s.GetAuditLogStats(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if stats == nil {
		stats = map[string]int{}
	}
	writeJSON(w, http.StatusOK, stats)
}
