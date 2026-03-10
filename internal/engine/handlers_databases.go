package engine

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiListManagedDatabases(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	dbs, err := s.store.ListManagedDatabases(r.Context(), chi.URLParam(r, "projectId"))
	if handleErr(w, err) {
		return
	}
	if dbs == nil {
		dbs = []store.ManagedDatabase{}
	}
	writeJSON(w, http.StatusOK, dbs)
}

func (s *Server) apiCreateManagedDatabase(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Name          string `json:"name"`
		DBType        string `json:"db_type"`
		Version       string `json:"version"`
		StorageMode   string `json:"storage_mode"`
		StorageHostID string `json:"storage_host_id"`
		NodeID        string `json:"node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if req.Name == "" || req.DBType == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name and db_type are required", nil)
		return
	}
	if req.Version == "" {
		req.Version = "latest"
	}
	if req.StorageMode == "" {
		req.StorageMode = "local"
	}
	db := &store.ManagedDatabase{
		ProjectID:     chi.URLParam(r, "projectId"),
		Name:          req.Name,
		DBType:        req.DBType,
		Version:       req.Version,
		Status:        "pending",
		StorageMode:   req.StorageMode,
		StorageHostID: req.StorageHostID,
		NodeID:        req.NodeID,
	}
	if err := s.store.CreateManagedDatabase(r.Context(), db); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "managed_database", db.ID, "")

	jobData := map[string]string{
		"action":       "provision",
		"db_id":        db.ID,
		"name":         db.Name,
		"db_type":      db.DBType,
		"version":      db.Version,
		"storage_mode": req.StorageMode,
	}
	if req.StorageHostID != "" {
		jobData["storage_host_id"] = req.StorageHostID
	}
	if req.NodeID != "" {
		jobData["node_id"] = req.NodeID
	}
	if s.nc != nil {
		job, _ := json.Marshal(jobData)
		_ = s.nc.Publish("hive.deploy", job)
	}

	writeJSON(w, http.StatusCreated, db)
}

func (s *Server) apiGetManagedDatabase(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	db, err := s.store.GetManagedDatabase(r.Context(), chi.URLParam(r, "databaseId"))
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, db)
}

func (s *Server) apiDeleteManagedDatabase(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "databaseId")
	db, err := s.store.GetManagedDatabase(r.Context(), id)
	if handleErr(w, err) {
		return
	}

	if s.sc != nil {
		svcName := "hive-db-" + db.Name
		svc, _ := s.sc.GetService(r.Context(), svcName)
		if svc != nil {
			_ = s.sc.RemoveService(r.Context(), svc.ID)
		}
	}

	if err := s.store.DeleteManagedDatabase(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "managed_database", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
