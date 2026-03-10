package engine

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiCreateAlertThreshold(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var at store.AlertThreshold
	if err := json.NewDecoder(r.Body).Decode(&at); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	at.OrgID = user.OrgID
	if at.Metric == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "metric is required", nil)
		return
	}
	if err := s.store.CreateAlertThreshold(r.Context(), &at); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "alert_threshold", at.ID, "")
	writeJSON(w, http.StatusCreated, at)
}

func (s *Server) apiListAlertThresholds(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	thresholds, err := s.store.ListAlertThresholds(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if thresholds == nil {
		thresholds = []store.AlertThreshold{}
	}
	writeJSON(w, http.StatusOK, thresholds)
}

func (s *Server) apiDeleteAlertThreshold(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteAlertThreshold(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "alert_threshold", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
