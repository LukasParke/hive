package engine

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiListSystemTasks(w http.ResponseWriter, r *http.Request) {
	if _, err := requireMember(r); handleErr(w, err) {
		return
	}
	if s.store == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	tasks, err := s.store.ListSystemTasks(r.Context())
	if handleErr(w, err) {
		return
	}
	if tasks == nil {
		tasks = []store.SystemTask{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) apiTriggerSystemTask(w http.ResponseWriter, r *http.Request) {
	if _, err := requireMember(r); handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "taskId")

	if s.taskManager == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "task manager not initialized", nil)
		return
	}

	if err := s.taskManager.Trigger(id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "triggered", "task_id": id})
}

func (s *Server) apiUpdateSystemTask(w http.ResponseWriter, r *http.Request) {
	if _, err := requireMember(r); handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "taskId")

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	if s.store == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "service_unavailable", "store not available", nil)
		return
	}

	if req.Enabled != nil {
		if err := s.store.UpdateSystemTaskEnabled(r.Context(), id, *req.Enabled); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
			return
		}
	}

	task, err := s.store.GetSystemTask(r.Context(), id)
	if err != nil || task == nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "task not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, task)
}
