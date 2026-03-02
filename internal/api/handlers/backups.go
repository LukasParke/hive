package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/store"
)

func CreateBackupConfig(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ResourceID string `json:"resource_id"`
			Schedule   string `json:"schedule"`
			S3Bucket   string `json:"s3_bucket"`
			S3Prefix   string `json:"s3_prefix"`
			BackupType string `json:"backup_type"`
			VolumeID   string `json:"volume_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.Schedule == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "schedule required"})
			return
		}
		if body.BackupType == "" {
			body.BackupType = "database"
		}

		s := storeFromRequest(r)
		bc := &store.BackupConfig{
			ResourceID: body.ResourceID,
			Schedule:   body.Schedule,
			S3Bucket:   body.S3Bucket,
			S3Prefix:   body.S3Prefix,
			BackupType: body.BackupType,
			VolumeID:   body.VolumeID,
		}
		if err := s.CreateBackupConfig(r.Context(), bc); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		// Publish a schedule event so the scheduler picks it up
		scheduleMsg, _ := json.Marshal(map[string]string{
			"action":    "schedule",
			"config_id": bc.ID,
			"schedule":  bc.Schedule,
		})
		if err := nc.Publish("hive.backup.schedule", scheduleMsg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to schedule backup"})
			return
		}

		writeJSON(w, http.StatusCreated, bc)
	}
}

func ListBackups(w http.ResponseWriter, r *http.Request) {
	s := storeFromRequest(r)
	configs, err := s.ListBackupConfigs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if configs == nil {
		configs = []store.BackupConfig{}
	}
	writeJSON(w, http.StatusOK, configs)
}

func TriggerBackup(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		configID := chi.URLParam(r, "configId")
		s := storeFromRequest(r)

		config, err := s.GetBackupConfig(r.Context(), configID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "config not found"})
			return
		}

		msg, _ := json.Marshal(map[string]string{"config_id": config.ID})
		if err := nc.Publish("hive.backup", msg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to trigger backup"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "triggered"})
	}
}

func RestoreBackup(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		configID := chi.URLParam(r, "configId")
		runID := chi.URLParam(r, "runId")

		msg, _ := json.Marshal(map[string]string{
			"action":    "restore",
			"config_id": configID,
			"run_id":    runID,
		})
		if err := nc.Publish("hive.backup", msg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to trigger restore"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "restore triggered"})
	}
}

func ListBackupRuns(w http.ResponseWriter, r *http.Request) {
	configID := chi.URLParam(r, "configId")
	s := storeFromRequest(r)

	runs, err := s.ListBackupRuns(r.Context(), configID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if runs == nil {
		runs = []store.BackupRun{}
	}
	writeJSON(w, http.StatusOK, runs)
}
