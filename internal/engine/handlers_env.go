package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/encryption"
)

func (s *Server) apiListAppEnvVars(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	vars, err := s.store.ListAppEnvVars(r.Context(), chi.URLParam(r, "appId"))
	if handleErr(w, err) {
		return
	}
	if vars == nil {
		vars = []store.AppEnvVar{}
	}
	writeJSON(w, http.StatusOK, vars)
}

func (s *Server) apiCreateAppEnvVar(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		IsSecret bool   `json:"is_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if req.Key == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "key is required", nil)
		return
	}
	encrypted, err := encryption.Encrypt([]byte(req.Value))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to encrypt value", nil)
		return
	}
	ev := &store.AppEnvVar{
		AppID:          chi.URLParam(r, "appId"),
		Key:            req.Key,
		ValueEncrypted: encrypted,
		IsSecret:       req.IsSecret,
		Source:         "user",
	}
	if err := s.store.CreateAppEnvVar(r.Context(), ev); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "env_var", ev.ID, ev.Key)
	writeJSON(w, http.StatusCreated, ev)
}

func (s *Server) apiGetAppEnvVarByKey(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	ev, err := s.store.GetAppEnvVarByKey(r.Context(), chi.URLParam(r, "appId"), chi.URLParam(r, "key"))
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (s *Server) apiUpdateAppEnvVar(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	key := chi.URLParam(r, "key")
	ev, err := s.store.GetAppEnvVarByKey(r.Context(), appId, key)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Value    string `json:"value"`
		IsSecret bool   `json:"is_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	encrypted, err := encryption.Encrypt([]byte(req.Value))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to encrypt value", nil)
		return
	}
	ev.ValueEncrypted = encrypted
	ev.IsSecret = req.IsSecret
	if err := s.store.UpdateAppEnvVar(r.Context(), ev); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (s *Server) apiDeleteAppEnvVarByKey(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	key := chi.URLParam(r, "key")
	if err := s.store.DeleteAppEnvVarByKey(r.Context(), appId, key); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "env_var", appId+":"+key, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) apiBulkUpsertAppEnvVars(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appId := chi.URLParam(r, "appId")
	var req struct {
		Vars []struct {
			Key      string `json:"key"`
			Value    string `json:"value"`
			IsSecret bool   `json:"is_secret"`
		} `json:"vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	vars := make([]store.AppEnvVar, 0, len(req.Vars))
	for _, v := range req.Vars {
		if v.Key == "" {
			continue
		}
		encrypted, err := encryption.Encrypt([]byte(v.Value))
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to encrypt value for key "+v.Key, nil)
			return
		}
		vars = append(vars, store.AppEnvVar{
			Key:            v.Key,
			ValueEncrypted: encrypted,
			IsSecret:       v.IsSecret,
		})
	}
	if err := s.store.BulkUpsertAppEnvVars(r.Context(), appId, vars); handleErr(w, err) {
		return
	}
	s.auditLog(r, "bulk_upsert", "env_var", appId, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "upserted", "count": len(vars)})
}

func (s *Server) apiImportEnvVars(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	var vars []store.AppEnvVar
	for _, line := range strings.Split(req.Content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		encrypted, err := encryption.Encrypt([]byte(value))
		if err != nil {
			continue
		}
		vars = append(vars, store.AppEnvVar{
			Key:            key,
			ValueEncrypted: encrypted,
		})
	}

	if err := s.store.BulkUpsertAppEnvVars(r.Context(), appID, vars); handleErr(w, err) {
		return
	}
	s.auditLog(r, "import", "env_var", appID, fmt.Sprintf("imported %d vars", len(vars)))
	writeJSON(w, http.StatusOK, map[string]any{"imported": len(vars), "message": fmt.Sprintf("Imported %d environment variables", len(vars))})
}

func (s *Server) apiExportEnvVars(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	vars, err := s.store.ListAppEnvVars(r.Context(), appID)
	if handleErr(w, err) {
		return
	}

	var sb strings.Builder
	for _, ev := range vars {
		value := string(ev.ValueEncrypted)
		decrypted, err := encryption.Decrypt(ev.ValueEncrypted)
		if err == nil {
			value = string(decrypted)
		}
		if ev.IsSecret {
			sb.WriteString(fmt.Sprintf("# %s=[REDACTED]\n", ev.Key))
		} else {
			sb.WriteString(fmt.Sprintf("%s=%s\n", ev.Key, value))
		}
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(sb.String()))
}
