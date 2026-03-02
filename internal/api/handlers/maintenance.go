package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/store"
)

// maintenanceTaskResp serializes Config as JSON object (not base64)
type maintenanceTaskResp struct {
	ID         string          `json:"id"`
	OrgID      string          `json:"org_id"`
	Type       string          `json:"type"`
	Schedule   string          `json:"schedule"`
	Enabled    bool            `json:"enabled"`
	LastRunAt  *time.Time      `json:"last_run_at"`
	LastStatus string          `json:"last_status"`
	Config     json.RawMessage `json:"config"`
	CreatedAt  time.Time       `json:"created_at"`
}

func maintenanceTaskToResp(mt *store.MaintenanceTask) maintenanceTaskResp {
	var lastRun *time.Time
	if mt.LastRunAt.Valid {
		lastRun = &mt.LastRunAt.Time
	}
	cfg := mt.Config
	if cfg == nil {
		cfg = []byte("{}")
	}
	return maintenanceTaskResp{
		ID:         mt.ID,
		OrgID:      mt.OrgID,
		Type:       mt.Type,
		Schedule:   mt.Schedule,
		Enabled:    mt.Enabled,
		LastRunAt:  lastRun,
		LastStatus: mt.LastStatus,
		Config:     cfg,
		CreatedAt:  mt.CreatedAt,
	}
}

func CreateMaintenanceTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type     string                 `json:"type"`
		Schedule string                 `json:"schedule"`
		Config   map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Type == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type is required"})
		return
	}
	if body.Schedule == "" {
		body.Schedule = "0 3 * * 0" // default: Sunday 3am
	}

	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	configJSON := []byte("{}")
	if body.Config != nil {
		configJSON, _ = json.Marshal(body.Config)
	}

	s := storeFromRequest(r)
	mt := &store.MaintenanceTask{
		OrgID:    orgID,
		Type:     body.Type,
		Schedule: body.Schedule,
		Enabled:  true,
		Config:   configJSON,
	}
	if err := s.CreateMaintenanceTask(r.Context(), mt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, maintenanceTaskToResp(mt))
}

func ListMaintenanceTasks(w http.ResponseWriter, r *http.Request) {
	s := storeFromRequest(r)
	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		orgID = "default"
	}

	tasks, err := s.ListMaintenanceTasks(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if tasks == nil {
		tasks = []store.MaintenanceTask{}
	}
	resp := make([]maintenanceTaskResp, len(tasks))
	for i := range tasks {
		resp[i] = maintenanceTaskToResp(&tasks[i])
	}
	writeJSON(w, http.StatusOK, resp)
}

func UpdateMaintenanceTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	var body struct {
		Type     string                 `json:"type"`
		Schedule string                 `json:"schedule"`
		Enabled  *bool                  `json:"enabled"`
		Config   map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	s := storeFromRequest(r)
	mt, err := s.GetMaintenanceTask(r.Context(), taskID)
	if err != nil || mt == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	if body.Type != "" {
		mt.Type = body.Type
	}
	if body.Schedule != "" {
		mt.Schedule = body.Schedule
	}
	if body.Enabled != nil {
		mt.Enabled = *body.Enabled
	}
	if body.Config != nil {
		mt.Config, _ = json.Marshal(body.Config)
	}

	if err := s.UpdateMaintenanceTask(r.Context(), mt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, maintenanceTaskToResp(mt))
}

func DeleteMaintenanceTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	s := storeFromRequest(r)
	if err := s.DeleteMaintenanceTask(r.Context(), taskID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": taskID})
}

func TriggerMaintenanceTask(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := chi.URLParam(r, "taskId")
		s := storeFromRequest(r)

		mt, err := s.GetMaintenanceTask(r.Context(), taskID)
		if err != nil || mt == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
			return
		}

		msg, _ := json.Marshal(map[string]string{
			"task_id": taskID,
			"type":    mt.Type,
		})
		if err := nc.Publish("hive.maintenance", msg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "triggered"})
	}
}

func ListMaintenanceRuns(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	s := storeFromRequest(r)

	runs, err := s.ListMaintenanceRuns(r.Context(), taskID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if runs == nil {
		runs = []store.MaintenanceRun{}
	}
	writeJSON(w, http.StatusOK, runs)
}
