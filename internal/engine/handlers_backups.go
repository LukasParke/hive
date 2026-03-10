package engine

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiCreateBackupConfig(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var bc store.BackupConfig
	if err := json.NewDecoder(r.Body).Decode(&bc); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if bc.ResourceID == "" && bc.VolumeID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "resource_id or volume_id is required", nil)
		return
	}
	if err := s.store.CreateBackupConfig(r.Context(), &bc); handleErr(w, err) {
		return
	}
	// Live-register the new schedule so it takes effect without an engine restart
	if bc.Schedule != "" && s.nc != nil {
		schedMsg, _ := json.Marshal(map[string]string{
			"action":    "schedule",
			"config_id": bc.ID,
			"schedule":  bc.Schedule,
		})
		if err := s.nc.Publish("hive.backup.schedule", schedMsg); err != nil {
			s.log.Warnf("publish backup schedule: %v", err)
		}
	}
	s.auditLog(r, "create", "backup_config", bc.ID, "")
	writeJSON(w, http.StatusCreated, bc)
}

func (s *Server) apiListBackupConfigsByOrg(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	configs, err := s.store.ListBackupConfigsByOrg(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if configs == nil {
		configs = []store.BackupConfig{}
	}
	writeJSON(w, http.StatusOK, configs)
}

func (s *Server) apiListBackupRuns(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	configID := chi.URLParam(r, "configId")
	runs, err := s.store.ListBackupRuns(r.Context(), configID)
	if handleErr(w, err) {
		return
	}
	if runs == nil {
		runs = []store.BackupRun{}
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) apiTriggerBackup(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	configID := body["config_id"]
	if configID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "config_id is required", nil)
		return
	}
	job, _ := json.Marshal(map[string]string{"config_id": configID})
	if err := s.nc.Publish("hive.backup", job); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) apiTriggerRestore(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	configID := body["config_id"]
	runID := body["run_id"]
	if configID == "" || runID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "config_id and run_id are required", nil)
		return
	}
	job, _ := json.Marshal(map[string]string{"action": "restore", "config_id": configID, "run_id": runID})
	if err := s.nc.Publish("hive.backup", job); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}
