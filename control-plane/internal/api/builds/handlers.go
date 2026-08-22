package builds

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/luke/hive/control-plane/internal/api/common"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/jobs/riverjobs"
	"github.com/luke/hive/control-plane/internal/rbac"
)

// Handler serves build endpoints.
type Handler struct {
	Pool        *pgxpool.Pool
	RiverClient *river.Client[pgx.Tx]
}

// NewHandler returns a build Handler backed by the given pool.
func NewHandler(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) *Handler {
	return &Handler{Pool: pool, RiverClient: riverClient}
}

// ListBuilds lists builds for an application.
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
		limit 200
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
			continue
		}
		out = append(out, map[string]any{
			"id": id, "applicationId": appID, "status": status, "trigger": trigger,
			"imageTag": imageTag, "createdAt": createdAt,
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// ListBuildQueue lists queued and in-flight builds.
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
		where p.organization_id = $1::uuid and b.status in ('queued','building')
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
			continue
		}
		out = append(out, map[string]any{
			"id": id, "applicationId": appID, "status": status, "trigger": trigger,
			"imageTag": imageTag, "retries": retries, "createdAt": createdAt,
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// GetBuildLogs returns the accumulated build log text for one build.
func (h *Handler) GetBuildLogs(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	buildID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(buildID); err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid_id", "invalid build id")
		return
	}
	logs, err := dbgen.New(h.Pool).GetBuildLog(r.Context(), dbgen.GetBuildLogParams{
		BuildID:        uuidOrNil(buildID),
		OrganizationID: uuidOrNil(orgID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "not_found", "build not found")
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to read build logs")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(logs))
}

// CancelBuild cancels a queued or running build.
func (h *Handler) CancelBuild(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	buildID := chi.URLParam(r, "id")
	cmd, err := h.Pool.Exec(r.Context(), `
		update build_jobs b
		set status = 'cancelled'
		from applications a, projects p
		where b.id = $1::uuid and a.id = b.application_id and p.id = a.project_id and p.organization_id = $2::uuid and b.status in ('queued','building')
	`, buildID, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"build not found or not cancelable"}`, http.StatusNotFound)
		return
	}
	// The BuildWorker polls the row between stages and aborts the River
	// job when it observes the cancelled status.
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "canceled"})
}

// RetryBuild re-queues a failed build.
func (h *Handler) RetryBuild(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	buildID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(buildID); err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid_id", "invalid build id")
		return
	}
	resetID, err := dbgen.New(h.Pool).ResetBuildForRetry(r.Context(), dbgen.ResetBuildForRetryParams{
		BuildID:        uuidOrNil(buildID),
		OrganizationID: uuidOrNil(orgID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, `{"message":"build not found or already active"}`, http.StatusNotFound)
		return
	}
	if riverjobs.IsUniqueViolation(err) {
		common.WriteError(w, http.StatusConflict, "build_in_progress", "another build is queued or running for this application")
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := h.RiverClient.Insert(r.Context(), riverjobs.BuildJobArgs{BuildID: resetID}, nil); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "enqueue_failed", "failed to enqueue build job")
		return
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "buildId": resetID})
}
func uuidOrNil(s string) pgtype.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
