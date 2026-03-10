package engine

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiListSecrets(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	secrets, err := s.store.ListSecrets(r.Context(), chi.URLParam(r, "projectId"))
	if handleErr(w, err) {
		return
	}
	if secrets == nil {
		secrets = []store.Secret{}
	}
	writeJSON(w, http.StatusOK, secrets)
}

func (s *Server) apiCreateSecret(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Value       string `json:"value"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	// Create Docker secret via swarm client
	dockerID, err := s.sc.CreateSecret(r.Context(), req.Name, []byte(req.Value), nil)
	if handleErr(w, err) {
		return
	}
	sec := &store.Secret{
		ProjectID:      chi.URLParam(r, "projectId"),
		Name:           req.Name,
		DockerSecretID: dockerID,
		Description:    req.Description,
	}
	if err := s.store.CreateSecret(r.Context(), sec); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "secret", sec.ID, "")
	writeJSON(w, http.StatusCreated, sec)
}

func (s *Server) apiDeleteSecret(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	secretId := chi.URLParam(r, "secretId")
	sec, err := s.store.GetSecret(r.Context(), secretId)
	if handleErr(w, err) {
		return
	}
	if sec.DockerSecretID != "" {
		_ = s.sc.RemoveSecret(r.Context(), sec.DockerSecretID)
	}
	if err := s.store.DeleteSecret(r.Context(), secretId); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "secret", secretId, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) apiAttachSecret(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Target string `json:"target"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	as := &store.AppSecret{
		AppID:    chi.URLParam(r, "appId"),
		SecretID: chi.URLParam(r, "secretId"),
		Target:   req.Target,
	}
	if err := s.store.AttachSecret(r.Context(), as); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "attached"})
}

func (s *Server) apiDetachSecret(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	if err := s.store.DetachSecret(r.Context(), chi.URLParam(r, "appId"), chi.URLParam(r, "secretId")); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "detached"})
}
