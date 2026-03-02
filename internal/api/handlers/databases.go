package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/store"
)

func CreateDatabase(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectId")

		var body struct {
			Name    string `json:"name"`
			DBType  string `json:"db_type"`
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.Name == "" || body.DBType == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and db_type are required"})
			return
		}
		if body.Version == "" {
			body.Version = "latest"
		}

		s := storeFromRequest(r)
		db := &store.ManagedDatabase{
			ProjectID: projectID,
			Name:      body.Name,
			DBType:    body.DBType,
			Version:   body.Version,
		}
		if err := s.CreateManagedDatabase(r.Context(), db); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		job, _ := json.Marshal(map[string]string{
			"action":  "provision",
			"db_id":   db.ID,
			"db_type": db.DBType,
			"version": db.Version,
			"name":    db.Name,
		})
		nc.Publish("hive.deploy", job)

		writeJSON(w, http.StatusCreated, db)
	}
}

func ListDatabases(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	s := storeFromRequest(r)
	dbs, err := s.ListManagedDatabases(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, dbs)
}
