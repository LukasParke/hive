package engine

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/notify"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiCreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var nc store.NotificationChannel
	if err := json.NewDecoder(r.Body).Decode(&nc); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	nc.OrgID = user.OrgID
	if nc.Name == "" || nc.Type == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name and type are required", nil)
		return
	}
	if err := s.store.CreateNotificationChannel(r.Context(), &nc); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "notification_channel", nc.ID, "")
	writeJSON(w, http.StatusCreated, nc)
}

func (s *Server) apiListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	channels, err := s.store.ListNotificationChannels(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if channels == nil {
		channels = []store.NotificationChannel{}
	}
	writeJSON(w, http.StatusOK, channels)
}

func (s *Server) apiGetNotificationChannel(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	ch, err := s.store.GetNotificationChannel(r.Context(), id)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) apiDeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "channelId")
	if err := s.store.DeleteNotificationChannel(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "notification_channel", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) apiTestNotificationChannel(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "channelId")
	ch, err := s.store.GetNotificationChannel(r.Context(), id)
	if handleErr(w, err) {
		return
	}
	d := notify.NewDispatcher(s.store, s.log)
	if err := d.SendTest(r.Context(), *ch); err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", err.Error(), map[string]any{"status": "failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
