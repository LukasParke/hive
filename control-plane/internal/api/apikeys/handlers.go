package apikeys

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/rbac"
)

// Handler serves API key management endpoints.
type Handler struct {
	Pool *pgxpool.Pool
}

// NewHandler returns an API key Handler backed by the given pool.
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool}
}

// randomBytes is swappable so tests can exercise token-generation failures.
var randomBytes = rand.Read

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := randomBytes(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// CreateAPIKey issues a new API key.
func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := chi.URLParam(r, "id")
	if err := rbac.Require(h.Pool, orgID, claims.UserID, rbac.RoleOwner, rbac.RoleAdmin); err != nil {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	raw, err := randomToken(40)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = h.Pool.Exec(r.Context(), `
		insert into api_keys(user_id, name, token_hash) values ($1::uuid, $2, $3)
	`, claims.UserID, req.Name, sha256Hex(raw))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"token": raw})
}

// ListAPIKeys lists the caller's API keys.
func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	if orgID != chi.URLParam(r, "id") {
		common.WriteError(w, http.StatusForbidden, "forbidden", "active organization mismatch")
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select ak.id::text, ak.user_id::text, ak.name, ak.last_used_at, ak.created_at
		from api_keys ak
		join organization_members om on om.user_id = ak.user_id
		where om.organization_id = $1::uuid
		order by ak.created_at desc
	`, orgID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list api keys")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, userID, name string
		var lastUsed *time.Time
		var createdAt time.Time
		if err := rows.Scan(&id, &userID, &name, &lastUsed, &createdAt); err == nil {
			out = append(out, map[string]any{"id": id, "userId": userID, "name": name, "lastUsedAt": lastUsed, "createdAt": createdAt})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// DeleteAPIKey revokes an API key.
func (h *Handler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	if orgID != chi.URLParam(r, "id") {
		common.WriteError(w, http.StatusForbidden, "forbidden", "active organization mismatch")
		return
	}
	keyID := chi.URLParam(r, "keyId")
	_, err := h.Pool.Exec(r.Context(), `
		delete from api_keys ak
		using organization_members om
		where ak.id = $1::uuid and om.organization_id = $2::uuid and om.user_id = ak.user_id
	`, keyID, orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to delete api key")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// RegenerateAPIKey replaces an API key's secret.
func (h *Handler) RegenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	if orgID != chi.URLParam(r, "id") {
		common.WriteError(w, http.StatusForbidden, "forbidden", "active organization mismatch")
		return
	}
	keyID := chi.URLParam(r, "keyId")
	var name string
	var userID string
	err := h.Pool.QueryRow(r.Context(), `
		select ak.name, ak.user_id::text
		from api_keys ak
		join organization_members om on om.user_id = ak.user_id
		where ak.id = $1::uuid and om.organization_id = $2::uuid
	`, keyID, orgID).Scan(&name, &userID)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "api key not found")
		return
	}
	_, err = h.Pool.Exec(r.Context(), `delete from api_keys where id = $1::uuid`, keyID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to delete old api key")
		return
	}
	raw, err := randomToken(40)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to generate token")
		return
	}
	_, err = h.Pool.Exec(r.Context(), `
		insert into api_keys(user_id, name, token_hash) values ($1::uuid, $2, $3)
	`, userID, name, sha256Hex(raw))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create new api key")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"token": raw})
}
