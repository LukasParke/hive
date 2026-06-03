package builds

import (
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

func (h *Handler) ListBuilds(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select b.id::text, b.application_id::text, b.status::text, b.trigger, b.image_tag, b.created_at
		from build_jobs b
		join applications a on a.id = b.application_id
		join projects p on p.id = a.project_id
		where p.organization_id = $1::uuid
		order by b.created_at desc
	`, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, appID, status, trigger, imageTag string
		var createdAt time.Time
		if err := rows.Scan(&id, &appID, &status, &trigger, &imageTag, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{"id": id, "applicationId": appID, "status": status, "trigger": trigger, "imageTag": imageTag, "createdAt": createdAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) ListBuildQueue(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select b.id::text, b.application_id::text, b.status::text, b.trigger, b.image_tag, b.retries, b.created_at
		from build_jobs b
		join applications a on a.id = b.application_id
		join projects p on p.id = a.project_id
		where p.organization_id = $1::uuid and b.status in ('queued','running')
		order by b.created_at asc
	`, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, appID, status, trigger, imageTag string
		var retries int
		var createdAt time.Time
		if err := rows.Scan(&id, &appID, &status, &trigger, &imageTag, &retries, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{
			"id": id, "applicationId": appID, "status": status, "trigger": trigger, "imageTag": imageTag, "retries": retries, "createdAt": createdAt,
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) CancelBuild(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	buildID := chi.URLParam(r, "id")
	cmd, err := h.Pool.Exec(r.Context(), `
		update build_jobs b
		set status = 'canceled'
		from applications a, projects p
		where b.id = $1::uuid and a.id = b.application_id and p.id = a.project_id and p.organization_id = $2::uuid and b.status in ('queued','running')
	`, buildID, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"build not found or not cancelable"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "canceled"})
}

func (h *Handler) RetryBuild(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	buildID := chi.URLParam(r, "id")
	var appID string
	if err := h.Pool.QueryRow(r.Context(), `
		select b.application_id::text
		from build_jobs b
		join applications a on a.id = b.application_id
		join projects p on p.id = a.project_id
		where b.id = $1::uuid and p.organization_id = $2::uuid
	`, buildID, orgID).Scan(&appID); err != nil {
		http.Error(w, `{"message":"build not found"}`, http.StatusNotFound)
		return
	}
	if _, err := h.Pool.Exec(r.Context(), `
		insert into build_jobs(application_id, trigger, status) values ($1::uuid, 'retry', 'queued')
	`, appID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}
