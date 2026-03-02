package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/store"
)

func CreateAlertThreshold(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Metric          string  `json:"metric"`
		Operator        string  `json:"operator"`
		Value           float64 `json:"value"`
		CooldownMinutes int     `json:"cooldown_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Metric == "" || body.Value == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "metric and value required"})
		return
	}
	if body.Operator == "" {
		body.Operator = ">"
	}
	if body.CooldownMinutes == 0 {
		body.CooldownMinutes = 5
	}

	s := storeFromRequest(r)
	at := &store.AlertThreshold{
		OrgID:           orgIDFromRequest(r),
		Metric:          body.Metric,
		Operator:        body.Operator,
		Value:           body.Value,
		CooldownMinutes: body.CooldownMinutes,
		Enabled:         true,
	}
	if err := s.CreateAlertThreshold(r.Context(), at); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, at)
}

func ListAlertThresholds(w http.ResponseWriter, r *http.Request) {
	s := storeFromRequest(r)
	thresholds, err := s.ListAlertThresholds(r.Context(), orgIDFromRequest(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if thresholds == nil {
		thresholds = []store.AlertThreshold{}
	}
	writeJSON(w, http.StatusOK, thresholds)
}

func DeleteAlertThreshold(w http.ResponseWriter, r *http.Request) {
	alertID := chi.URLParam(r, "alertId")
	s := storeFromRequest(r)
	if err := s.DeleteAlertThreshold(r.Context(), alertID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": alertID})
}

func orgIDFromRequest(r *http.Request) string {
	if orgID := middleware.GetOrgID(r.Context()); orgID != "" {
		return orgID
	}
	if orgID := r.Header.Get("X-Org-ID"); orgID != "" {
		return orgID
	}
	return "default"
}
