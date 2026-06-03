package environments

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/rbac"
)

type Handler struct {
	Pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool}
}

func (h *Handler) ListEnvironments(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select e.id::text, e.project_id::text, e.name, e.slug, e.created_at
		from environments e
		join projects p on p.id = e.project_id
		where p.organization_id = $1::uuid
		order by e.created_at desc
	`, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, projectID, name, slug string
		var createdAt time.Time
		if err := rows.Scan(&id, &projectID, &name, &slug, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{"id": id, "projectId": projectID, "name": name, "slug": slug, "createdAt": createdAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) CreateEnvironment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	var req struct {
		ProjectID string `json:"projectId"`
		Name      string `json:"name"`
		Slug      string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Name == "" || req.Slug == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	var projectOrg string
	if err := h.Pool.QueryRow(r.Context(), `select organization_id::text from projects where id = $1::uuid`, req.ProjectID).Scan(&projectOrg); err != nil || projectOrg != orgID {
		http.Error(w, `{"message":"project not in active organization"}`, http.StatusForbidden)
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into environments(project_id, name, slug)
		values ($1::uuid, $2, $3)
		returning id::text
	`, req.ProjectID, req.Name, req.Slug).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) GetEnvironment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var projectID, name, slug string
	var createdAt time.Time
	if err := h.Pool.QueryRow(r.Context(), `
		select e.project_id::text, e.name, e.slug, e.created_at
		from environments e
		join projects p on p.id = e.project_id
		where e.id = $1::uuid and p.organization_id = $2::uuid
	`, id, orgID).Scan(&projectID, &name, &slug, &createdAt); err != nil {
		http.Error(w, `{"message":"environment not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "projectId": projectID, "name": name, "slug": slug, "createdAt": createdAt})
}

func (h *Handler) UpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	cmd, err := h.Pool.Exec(r.Context(), `
		update environments e
		set name = coalesce(nullif($3,''), e.name),
			slug = coalesce(nullif($4,''), e.slug)
		from projects p
		where e.id = $1::uuid and p.id = e.project_id and p.organization_id = $2::uuid
	`, id, orgID, req.Name, req.Slug)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"environment not found"}`, http.StatusNotFound)
		return
	}
	h.GetEnvironment(w, r)
}

func (h *Handler) DeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	cmd, err := h.Pool.Exec(r.Context(), `
		delete from environments e
		using projects p
		where e.id = $1::uuid and p.id = e.project_id and p.organization_id = $2::uuid
	`, id, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"environment not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
