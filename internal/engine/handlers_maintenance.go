package engine

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiCreateMaintenanceTask(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var mt store.MaintenanceTask
	if err := json.NewDecoder(r.Body).Decode(&mt); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	mt.OrgID = user.OrgID
	if mt.Type == "" || mt.Schedule == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "type and schedule are required", nil)
		return
	}
	if err := s.store.CreateMaintenanceTask(r.Context(), &mt); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "maintenance_task", mt.ID, "")
	writeJSON(w, http.StatusCreated, mt)
}

func (s *Server) apiListMaintenanceTasks(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	tasks, err := s.store.ListMaintenanceTasks(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if tasks == nil {
		tasks = []store.MaintenanceTask{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) apiGetMaintenanceTask(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	mt, err := s.store.GetMaintenanceTask(r.Context(), id)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, mt)
}

func (s *Server) apiUpdateMaintenanceTask(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	var mt store.MaintenanceTask
	if err := json.NewDecoder(r.Body).Decode(&mt); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	mt.ID = id
	if err := s.store.UpdateMaintenanceTask(r.Context(), &mt); handleErr(w, err) {
		return
	}
	s.auditLog(r, "update", "maintenance_task", id, "")
	updated, _ := s.store.GetMaintenanceTask(r.Context(), id)
	if updated != nil {
		writeJSON(w, http.StatusOK, updated)
	} else {
		writeJSON(w, http.StatusOK, mt)
	}
}

func (s *Server) apiDeleteMaintenanceTask(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteMaintenanceTask(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "maintenance_task", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) apiCreateMaintenanceRun(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "task_id is required", nil)
		return
	}

	task, err := s.store.GetMaintenanceTask(r.Context(), taskID)
	if handleErr(w, err) {
		return
	}

	mr := store.MaintenanceRun{
		TaskID: taskID,
		Status: "running",
	}
	if err := s.store.CreateMaintenanceRun(r.Context(), &mr); handleErr(w, err) {
		return
	}

	if s.nc != nil {
		payload, _ := json.Marshal(map[string]any{
			"task_id": task.ID,
			"run_id":  mr.ID,
			"type":    task.Type,
			"config":  task.Config,
		})
		_ = s.nc.Publish("hive.maintenance", payload)
	}

	s.auditLog(r, "trigger", "maintenance_task", taskID, "")
	writeJSON(w, http.StatusCreated, mr)
}

func (s *Server) apiListMaintenanceRuns(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		taskID = r.URL.Query().Get("task_id")
	}
	if taskID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "task_id is required", nil)
		return
	}
	runs, err := s.store.ListMaintenanceRuns(r.Context(), taskID)
	if handleErr(w, err) {
		return
	}
	if runs == nil {
		runs = []store.MaintenanceRun{}
	}
	writeJSON(w, http.StatusOK, runs)
}
