package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
)

func CreateSecret(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectId")

		var body struct {
			Name        string `json:"name"`
			Value       string `json:"value"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.Name == "" || body.Value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and value are required"})
			return
		}

		sc, err := swarm.NewClient(nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker client: " + err.Error()})
			return
		}
		defer func() { _ = sc.Close() }()

		labels := map[string]string{
			"hive.project_id": projectID,
		}
		dockerID, err := sc.CreateSecret(r.Context(), body.Name, []byte(body.Value), labels)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		s := storeFromRequest(r)
		secret := &store.Secret{
			ProjectID:      projectID,
			Name:           body.Name,
			DockerSecretID: dockerID,
			Description:    body.Description,
		}
		if err := s.CreateSecret(r.Context(), secret); err != nil {
			_ = sc.RemoveSecret(r.Context(), dockerID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusCreated, secret)
	}
}

func ListSecrets(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	s := storeFromRequest(r)
	secrets, err := s.ListSecrets(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if secrets == nil {
		secrets = []store.Secret{}
	}
	writeJSON(w, http.StatusOK, secrets)
}

func DeleteSecret(w http.ResponseWriter, r *http.Request) {
	secretID := chi.URLParam(r, "secretId")
	s := storeFromRequest(r)

	secret, err := s.GetSecret(r.Context(), secretID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "secret not found"})
		return
	}

	if secret.DockerSecretID != "" {
		sc, err := swarm.NewClient(nil)
		if err == nil {
			_ = sc.RemoveSecret(r.Context(), secret.DockerSecretID)
			_ = sc.Close()
		}
	}

	if err := s.DeleteSecret(r.Context(), secretID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": secretID})
}

func AttachSecret(w http.ResponseWriter, r *http.Request) {
	secretID := chi.URLParam(r, "secretId")
	appID := chi.URLParam(r, "appId")

	var body struct {
		Target string `json:"target"`
		UID    string `json:"uid"`
		GID    string `json:"gid"`
		Mode   int    `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body = struct {
			Target string `json:"target"`
			UID    string `json:"uid"`
			GID    string `json:"gid"`
			Mode   int    `json:"mode"`
		}{}
	}

	if body.UID == "" {
		body.UID = "0"
	}
	if body.GID == "" {
		body.GID = "0"
	}
	if body.Mode == 0 {
		body.Mode = 292 // 0444
	}

	s := storeFromRequest(r)
	as := &store.AppSecret{
		AppID:    appID,
		SecretID: secretID,
		Target:   body.Target,
		UID:      body.UID,
		GID:      body.GID,
		Mode:     body.Mode,
	}
	if err := s.AttachSecret(r.Context(), as); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, as)
}

func DetachSecret(w http.ResponseWriter, r *http.Request) {
	secretID := chi.URLParam(r, "secretId")
	appID := chi.URLParam(r, "appId")

	s := storeFromRequest(r)
	if err := s.DetachSecret(r.Context(), appID, secretID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"detached": secretID})
}
