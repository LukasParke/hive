package engine

import (
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiCreateStorageHost(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	var sh store.StorageHost
	if err := json.NewDecoder(r.Body).Decode(&sh); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if sh.Name == "" || sh.Address == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name and address are required", nil)
		return
	}
	if err := s.store.CreateStorageHost(r.Context(), &sh); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "storage_host", sh.ID, "")
	writeJSON(w, http.StatusCreated, sh)
}

func (s *Server) apiListStorageHosts(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	hosts, err := s.store.ListStorageHosts(r.Context())
	if handleErr(w, err) {
		return
	}
	if hosts == nil {
		hosts = []store.StorageHost{}
	}
	writeJSON(w, http.StatusOK, hosts)
}

func (s *Server) apiGetStorageHost(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "hostId")
	sh, err := s.store.GetStorageHost(r.Context(), id)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, sh)
}

func (s *Server) apiUpdateStorageHost(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "hostId")
	var sh store.StorageHost
	if err := json.NewDecoder(r.Body).Decode(&sh); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	sh.ID = id
	if err := s.store.UpdateStorageHost(r.Context(), &sh); handleErr(w, err) {
		return
	}
	s.auditLog(r, "update", "storage_host", id, "")
	updated, _ := s.store.GetStorageHost(r.Context(), id)
	if updated != nil {
		writeJSON(w, http.StatusOK, updated)
	} else {
		writeJSON(w, http.StatusOK, sh)
	}
}

func (s *Server) apiDeleteStorageHost(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "hostId")
	if err := s.store.DeleteStorageHost(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "storage_host", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) apiTestStorageHost(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "hostId")
	sh, err := s.store.GetStorageHost(r.Context(), id)
	if handleErr(w, err) {
		return
	}

	port := "2049"
	switch sh.Type {
	case "cifs", "smb":
		port = "445"
	case "ceph", "cephfs":
		port = "6789"
	}

	addr := net.JoinHostPort(sh.Address, port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      false,
			"message": err.Error(),
			"address": addr,
		})
		return
	}
	_ = conn.Close()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Reachable at " + addr,
		"address": addr,
	})
}
