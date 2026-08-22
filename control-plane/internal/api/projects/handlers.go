package projects

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/rbac"
)

// Handler serves project management endpoints.
type Handler struct {
	Pool *pgxpool.Pool
}

// NewHandler returns a project Handler backed by the given pool.
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool}
}

// ListProjects lists projects for the organization.
func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID, ok := common.ResolveOrgID(w, r, h.Pool, claims.UserID)
	if !ok {
		return
	}
	if err := rbac.Require(h.Pool, orgID, claims.UserID, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); err != nil {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select id, name, created_at
		from projects
		where organization_id = $1::uuid
		order by created_at desc
	`, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type item struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"createdAt"`
	}
	var out []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Name, &it.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, it)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateProject creates a new project.
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID, ok := common.ResolveOrgID(w, r, h.Pool, claims.UserID)
	if !ok {
		return
	}
	if err := rbac.Require(h.Pool, orgID, claims.UserID, rbac.RoleOwner, rbac.RoleAdmin); err != nil {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		return
	}

	var id string
	var createdAt time.Time
	if err := h.Pool.QueryRow(r.Context(),
		"insert into projects(name, organization_id) values ($1, $2::uuid) returning id, created_at", req.Name, orgID,
	).Scan(&id, &createdAt); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "name": req.Name, "createdAt": createdAt})
}

// GetProject returns a single project by ID.
func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var name string
	var createdAt time.Time
	if err := h.Pool.QueryRow(r.Context(), `
		select p.name, p.created_at
		from projects p
		where p.id = $1::uuid and p.organization_id = $2::uuid
	`, id, orgID).Scan(&name, &createdAt); err != nil {
		http.Error(w, `{"message":"project not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "name": name, "createdAt": createdAt})
}

// UpdateProject updates an existing project.
func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	cmd, err := h.Pool.Exec(r.Context(), `
		update projects set name = $3
		where id = $1::uuid and organization_id = $2::uuid
	`, id, orgID, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"project not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "name": req.Name})
}

// DeleteProject removes a project.
func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	cmd, err := h.Pool.Exec(r.Context(), `delete from projects where id = $1::uuid and organization_id = $2::uuid`, id, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"project not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
