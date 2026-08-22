package deployments

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/jobs/riverjobs"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/riverqueue/river"
)

// Handler serves deployment endpoints.
type Handler struct {
	Pool        *pgxpool.Pool
	RiverClient *river.Client[pgx.Tx]
}

// NewHandler returns a deployment Handler wired to the given dependencies.
func NewHandler(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) *Handler {
	return &Handler{Pool: pool, RiverClient: riverClient}
}

// writeEnqueueConflict maps EnqueueBuild's duplicate-active-build error to
// an HTTP 409 response, returning true when handled.
func writeEnqueueConflict(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, riverjobs.ErrBuildAlreadyQueued) {
		return false
	}
	common.WriteError(w, http.StatusConflict, "build_in_progress", err.Error())
	return true
}

// EnqueueDeploy queues a deployment job for an application.
func (h *Handler) EnqueueDeploy(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(appID); err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid_id", "invalid application id")
		return
	}
	if err := h.Pool.QueryRow(r.Context(), `
		select from applications a
		join projects p on p.id = a.project_id
		where a.id = $1::uuid and p.organization_id = $2::uuid
	`, appID, orgID).Scan(); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			common.WriteError(w, http.StatusNotFound, "not_found", "application not found")
			return
		}
		common.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	buildID, err := riverjobs.EnqueueBuild(r.Context(), h.RiverClient, h.Pool, appID, "api", "")
	if writeEnqueueConflict(w, err) {
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "buildId": buildID})
}
// ListApplicationDeployments lists deployments of an application.
func (h *Handler) ListApplicationDeployments(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(appID); err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid_id", "invalid application id")
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select d.id::text, d.image_tag, d.status, d.trigger, d.created_at
		from deployments d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where d.application_id = $1::uuid and p.organization_id = $2::uuid
		order by d.created_at desc
	`, appID, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, imageTag, status, trigger string
		var createdAt time.Time
		if err := rows.Scan(&id, &imageTag, &status, &trigger, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{
			"id": id, "imageTag": imageTag, "status": status, "trigger": trigger, "createdAt": createdAt,
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// RollbackApplication redeploys a previous deployment.
func (h *Handler) RollbackApplication(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(appID); err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid_id", "invalid application id")
		return
	}
	var imageTag string
	err := h.Pool.QueryRow(r.Context(), `
		select d.image_tag
		from deployments d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where d.application_id = $1::uuid and p.organization_id = $2::uuid
		order by d.created_at desc
		offset 1
		limit 1
	`, appID, orgID).Scan(&imageTag)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "no_previous_deployment", "no previous deployment found")
		return
	}
	buildID, err := riverjobs.EnqueueBuild(r.Context(), h.RiverClient, h.Pool, appID, "rollback", imageTag)
	if writeEnqueueConflict(w, err) {
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "buildId": buildID})
}

// ApplicationLogs returns recent logs for an application's service.
func (h *Handler) ApplicationLogs(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(appID); err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid_id", "invalid application id")
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select d.id::text, d.image_tag, d.status, d.trigger, d.created_at
		from deployments d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where d.application_id = $1::uuid and p.organization_id = $2::uuid
		order by d.created_at desc
		limit 100
	`, appID, orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to read application logs")
		return
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var id, imageTag, status, trigger string
		var createdAt time.Time
		if scanErr := rows.Scan(&id, &imageTag, &status, &trigger, &createdAt); scanErr == nil {
			lines = append(lines, fmt.Sprintf("%s deployment=%s image=%s status=%s trigger=%s", createdAt.Format(time.RFC3339), id, imageTag, status, trigger))
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"logs": lines})
}

// ListDeployments lists the organization's recent deployments.
func (h *Handler) ListDeployments(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select d.id::text, d.application_id::text, a.name, d.image_tag, d.status, d.trigger, d.created_at
		from deployments d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where p.organization_id = $1::uuid
		order by d.created_at desc
		limit 200
	`, orgID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list deployments")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, appID, appName, imageTag, status, trigger string
		var createdAt time.Time
		if scanErr := rows.Scan(&id, &appID, &appName, &imageTag, &status, &trigger, &createdAt); scanErr == nil {
			out = append(out, map[string]any{"id": id, "applicationId": appID, "applicationName": appName, "imageTag": imageTag, "status": status, "trigger": trigger, "createdAt": createdAt})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// DeleteDeployment removes a deployment record.
func (h *Handler) DeleteDeployment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	deploymentID := chi.URLParam(r, "id")
	_, err := h.Pool.Exec(r.Context(), `
		delete from deployments d
		using applications a, projects p
		where d.id = $1::uuid and a.id = d.application_id and p.id = a.project_id and p.organization_id = $2::uuid
	`, deploymentID, orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to delete deployment")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
