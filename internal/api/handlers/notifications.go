package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/notify"
	"github.com/lholliger/hive/internal/store"
)

func CreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string            `json:"name"`
		Type   string            `json:"type"`
		Config map[string]string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Type == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type is required"})
		return
	}

	validTypes := map[string]bool{"discord": true, "slack": true, "webhook": true, "email": true, "gotify": true, "resend": true}
	if !validTypes[body.Type] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid type, must be one of: discord, slack, webhook, email, gotify, resend"})
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	configJSON, _ := json.Marshal(body.Config)

	s := storeFromRequest(r)
	ch := &store.NotificationChannel{
		OrgID:  orgID,
		Name:   body.Name,
		Type:   body.Type,
		Config: configJSON,
	}
	if err := s.CreateNotificationChannel(r.Context(), ch); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func ListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetOrgID(r.Context())
	s := storeFromRequest(r)
	channels, err := s.ListNotificationChannels(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if channels == nil {
		channels = []store.NotificationChannel{}
	}
	writeJSON(w, http.StatusOK, channels)
}

func DeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "channelId")
	s := storeFromRequest(r)
	if err := s.DeleteNotificationChannel(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func TestNotificationChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "channelId")
	s := storeFromRequest(r)

	ch, err := s.GetNotificationChannel(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "channel not found"})
		return
	}

	dispatcher := notify.NewDispatcher(s, nil)
	if err := dispatcher.SendTest(r.Context(), *ch); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
