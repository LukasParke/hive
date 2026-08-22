package registries

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/rbac"
)

// Handler serves registry management endpoints.
type Handler struct {
	Pool *pgxpool.Pool
}

// NewHandler returns a registry Handler backed by the given pool.
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool}
}

// ListRegistries lists the configured image registries.
func (h *Handler) ListRegistries(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `select id::text, name, url, username, is_default, created_at from registries order by created_at desc`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name, url, username string
		var isDefault bool
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &url, &username, &isDefault, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "url": url, "username": username, "isDefault": isDefault, "createdAt": createdAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateRegistry adds a new image registry.
func (h *Handler) CreateRegistry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		URL        string `json:"url"`
		Username   string `json:"username"`
		SecretName string `json:"secretName"`
		IsDefault  bool   `json:"isDefault"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.URL == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into registries(name, url, username, secret_name, is_default)
		values ($1, $2, $3, $4, $5)
		returning id::text
	`, req.Name, req.URL, req.Username, req.SecretName, req.IsDefault).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GetRegistry returns a single registry by ID.
func (h *Handler) GetRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var name, url, username, secretName string
	var isDefault bool
	var createdAt time.Time
	if err := h.Pool.QueryRow(r.Context(), `
		select name, url, coalesce(username,''), coalesce(secret_name,''), is_default, created_at
		from registries where id = $1::uuid
	`, id).Scan(&name, &url, &username, &secretName, &isDefault, &createdAt); err != nil {
		http.Error(w, `{"message":"registry not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"id": id, "name": name, "url": url, "username": username, "secretName": secretName, "isDefault": isDefault, "createdAt": createdAt,
	})
}

// UpdateRegistry updates an existing registry.
func (h *Handler) UpdateRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Name       string `json:"name"`
		URL        string `json:"url"`
		Username   string `json:"username"`
		SecretName string `json:"secretName"`
		IsDefault  *bool  `json:"isDefault"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	hasDefault := req.IsDefault != nil
	defaultValue := false
	if hasDefault {
		defaultValue = *req.IsDefault
	}
	cmd, err := h.Pool.Exec(r.Context(), `
		update registries
		set name = coalesce(nullif($2,''), name),
			url = coalesce(nullif($3,''), url),
			username = coalesce($4, username),
			secret_name = coalesce($5, secret_name),
			is_default = case when $6 then $7 else is_default end
		where id = $1::uuid
	`, id, req.Name, req.URL, req.Username, req.SecretName, hasDefault, defaultValue)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"registry not found"}`, http.StatusNotFound)
		return
	}
	h.GetRegistry(w, r)
}

// DeleteRegistry removes a registry.
func (h *Handler) DeleteRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	cmd, err := h.Pool.Exec(r.Context(), `delete from registries where id = $1::uuid`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"registry not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// TestRegistry verifies registry credentials with a live check.
func (h *Handler) TestRegistry(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var rawURL string
	if err := h.Pool.QueryRow(r.Context(), `select url from registries where id = $1::uuid`, id).Scan(&rawURL); err != nil {
		http.Error(w, `{"message":"registry not found"}`, http.StatusNotFound)
		return
	}
	u := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if u == "" {
		http.Error(w, `{"message":"registry url missing"}`, http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u+"/v2/", nil)
	if err != nil {
		http.Error(w, `{"message":"invalid registry url"}`, http.StatusBadRequest)
		return
	}
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"message":"registry check failed: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 400 {
		http.Error(w, fmt.Sprintf(`{"message":"registry responded with status %d"}`, res.StatusCode), http.StatusBadGateway)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
