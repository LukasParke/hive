// Package update provides HTTP handlers for the self-update system.
package update

import (
	"encoding/json"
	"net/http"

	"github.com/luke/hive/control-plane/internal/updater"
)

// Handler exposes update status and triggers.
type Handler struct {
	Updater *updater.Updater
}

// NewHandler creates an update handler.
func NewHandler(u *updater.Updater) *Handler {
	return &Handler{Updater: u}
}

// GetStatus returns the current update status.
func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request) {
	status := h.Updater.Status()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// TriggerUpdate forces an update check and, if available, starts the Swarm rolling update.
func (h *Handler) TriggerUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := h.Updater.CheckNow(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	status := h.Updater.Status()
	if !status.UpdateAvailable {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "no update available",
			"status":  status,
		})
		return
	}

	if err := h.Updater.Update(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "update triggered",
		"version": status.LatestVersion,
	})
}
