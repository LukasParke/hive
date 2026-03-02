package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/encryption"
)

// envVarResponse is the response shape with decrypted value (masked for secrets).
type envVarResponse struct {
	ID        string `json:"id"`
	AppID     string `json:"app_id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	IsSecret  bool   `json:"is_secret"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func maskSecret(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return "****" + value[len(value)-4:]
}

func ListEnvVars(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)

	vars, err := s.ListAppEnvVars(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	resp := make([]envVarResponse, 0, len(vars))
	for _, ev := range vars {
		plain, err := encryption.Decrypt(ev.ValueEncrypted)
		if err != nil {
			plain = []byte("(decrypt error)")
		}
		val := string(plain)
		if ev.IsSecret {
			val = maskSecret(val)
		}
		resp = append(resp, envVarResponse{
			ID:        ev.ID,
			AppID:     ev.AppID,
			Key:       ev.Key,
			Value:     val,
			IsSecret:  ev.IsSecret,
			Source:    ev.Source,
			CreatedAt: ev.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: ev.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func SetEnvVar(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)

	_, err := s.GetApp(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	var body struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		IsSecret bool   `json:"is_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	body.Key = strings.TrimSpace(body.Key)
	if body.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}

	encrypted, err := encryption.Encrypt([]byte(body.Value))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encryption failed"})
		return
	}

	existing, _ := s.GetAppEnvVarByKey(r.Context(), appID, body.Key)
	if existing != nil {
		existing.ValueEncrypted = encrypted
		existing.IsSecret = body.IsSecret
		if err := s.UpdateAppEnvVar(r.Context(), existing); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		val := body.Value
		if body.IsSecret {
			val = maskSecret(val)
		}
		writeJSON(w, http.StatusOK, envVarResponse{
			ID:        existing.ID,
			AppID:     existing.AppID,
			Key:       existing.Key,
			Value:     val,
			IsSecret:  existing.IsSecret,
			Source:    existing.Source,
			CreatedAt: existing.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: existing.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
		return
	}

	ev := &store.AppEnvVar{
		AppID:          appID,
		Key:            body.Key,
		ValueEncrypted: encrypted,
		IsSecret:       body.IsSecret,
		Source:         "user",
	}
	if err := s.CreateAppEnvVar(r.Context(), ev); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	val := body.Value
	if body.IsSecret {
		val = maskSecret(val)
	}
	writeJSON(w, http.StatusCreated, envVarResponse{
		ID:        ev.ID,
		AppID:     ev.AppID,
		Key:       ev.Key,
		Value:     val,
		IsSecret:  ev.IsSecret,
		Source:    ev.Source,
		CreatedAt: ev.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: ev.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func DeleteEnvVar(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	key := chi.URLParam(r, "key")
	s := storeFromRequest(r)

	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}

	if err := s.DeleteAppEnvVarByKey(r.Context(), appID, key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": key})
}

func ImportEnvVars(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)

	_, err := s.GetApp(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	vars := parseDotenv(body.Content)
	if len(vars) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"imported": 0,
			"message":  "no valid env vars found",
		})
		return
	}

	toUpsert := make([]store.AppEnvVar, 0, len(vars))
	for k, v := range vars {
		encrypted, err := encryption.Encrypt([]byte(v))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encryption failed for key " + k})
			return
		}
		toUpsert = append(toUpsert, store.AppEnvVar{
			Key:            k,
			ValueEncrypted: encrypted,
			IsSecret:       false,
			Source:         "user",
		})
	}

	if err := s.BulkUpsertAppEnvVars(r.Context(), appID, toUpsert); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"imported": len(toUpsert),
		"message":  "success",
	})
}

func ExportEnvVars(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)

	vars, err := s.ListAppEnvVars(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var b bytes.Buffer
	for _, ev := range vars {
		plain, err := encryption.Decrypt(ev.ValueEncrypted)
		if err != nil {
			continue
		}
		val := string(plain)
		if ev.IsSecret {
			val = maskSecret(val)
		}
		key := ev.Key
		if needsQuotes(val) {
			val = `"` + strings.ReplaceAll(val, `"`, `\"`) + `"`
		}
		b.WriteString(key + "=" + val + "\n")
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename=".env"`)
	w.WriteHeader(http.StatusOK)
	w.Write(b.Bytes())
}

func needsQuotes(s string) bool {
	return strings.ContainsAny(s, " \t\n#\"'$\\") || s == ""
}

// parseDotenv parses a dotenv-style string into key=value pairs.
func parseDotenv(content string) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		key = strings.TrimFunc(key, func(r rune) bool { return r == '"' || r == '\'' })
		if key == "" || !isValidEnvKey(key) {
			continue
		}
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		val = strings.ReplaceAll(val, `\"`, `"`)
		result[key] = val
	}
	return result
}

func isValidEnvKey(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && unicode.IsDigit(r) {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}
