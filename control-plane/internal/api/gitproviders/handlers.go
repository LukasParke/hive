package gitproviders

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/rbac"
)

// Handler serves git provider management endpoints.
type Handler struct {
	Pool *pgxpool.Pool
}

// NewHandler returns a git provider Handler backed by the given pool.
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool}
}

// ListGitProviders lists the configured git providers.
func (h *Handler) ListGitProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `select id::text, type, name, base_url, created_at from git_providers order by created_at desc`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, typ, name, baseURL string
		var createdAt time.Time
		if err := rows.Scan(&id, &typ, &name, &baseURL, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{"id": id, "type": typ, "name": name, "baseUrl": baseURL, "createdAt": createdAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateGitProvider adds a new git provider configuration.
func (h *Handler) CreateGitProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type            string `json:"type"`
		Name            string `json:"name"`
		BaseURL         string `json:"baseUrl"`
		TokenSecretName string `json:"tokenSecretName"`
		WebhookSecret   string `json:"webhookSecret"`
		Enabled         bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Type == "" || req.Name == "" || req.BaseURL == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into git_providers(type, name, base_url, token_secret_name, webhook_secret, enabled)
		values ($1, $2, $3, $4, $5, $6)
		returning id::text
	`, req.Type, req.Name, req.BaseURL, req.TokenSecretName, req.WebhookSecret, req.Enabled).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}
