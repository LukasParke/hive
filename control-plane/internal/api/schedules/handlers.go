package schedules

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/jobs/riverjobs"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/riverqueue/river"
)

// Handler serves scheduled job endpoints.
type Handler struct {
	Pool        *pgxpool.Pool
	RiverClient *river.Client[pgx.Tx]
}

// NewHandler returns a schedule Handler backed by the given pool.
func NewHandler(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) *Handler {
	return &Handler{Pool: pool, RiverClient: riverClient}
}

// ListSchedules lists schedules for the organization.
func (h *Handler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select id::text, name, cron_expr, target_type, target_id, enabled, last_run_at, created_at
		from schedules
		order by created_at desc
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name, cronExpr, targetType, targetID string
		var enabled bool
		var lastRunAt *time.Time
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &cronExpr, &targetType, &targetID, &enabled, &lastRunAt, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{
			"id": id, "name": name, "cronExpr": cronExpr, "targetType": targetType, "targetId": targetID, "enabled": enabled, "lastRunAt": lastRunAt, "createdAt": createdAt,
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateSchedule creates a new schedule.
func (h *Handler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	var req struct {
		Name       string `json:"name"`
		CronExpr   string `json:"cronExpr"`
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		Enabled    *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.CronExpr == "" || req.TargetType == "" || req.TargetID == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into schedules(name, cron_expr, target_type, target_id, enabled)
		values ($1, $2, $3, $4, $5)
		returning id::text
	`, req.Name, req.CronExpr, req.TargetType, req.TargetID, enabled).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// UpdateSchedule updates an existing schedule.
func (h *Handler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Name       string `json:"name"`
		CronExpr   string `json:"cronExpr"`
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		Enabled    *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	hasEnabled := req.Enabled != nil
	enabled := false
	if hasEnabled {
		enabled = *req.Enabled
	}
	cmd, err := h.Pool.Exec(r.Context(), `
		update schedules
		set name = coalesce(nullif($2,''), name),
			cron_expr = coalesce(nullif($3,''), cron_expr),
			target_type = coalesce(nullif($4,''), target_type),
			target_id = coalesce(nullif($5,''), target_id),
			enabled = case when $6 then $7 else enabled end
		where id = $1::uuid
	`, id, req.Name, req.CronExpr, req.TargetType, req.TargetID, hasEnabled, enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"schedule not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteSchedule removes a schedule.
func (h *Handler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	cmd, err := h.Pool.Exec(r.Context(), `delete from schedules where id = $1::uuid`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"schedule not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// RunScheduleNow triggers a schedule immediately.
func (h *Handler) RunScheduleNow(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var targetType, targetID string
	if err := h.Pool.QueryRow(r.Context(), `select target_type, target_id from schedules where id = $1::uuid`, id).Scan(&targetType, &targetID); err != nil {
		http.Error(w, `{"message":"schedule not found"}`, http.StatusNotFound)
		return
	}
	switch strings.ToLower(strings.TrimSpace(targetType)) {
	case "backup":
		var backupID string
		if err := h.Pool.QueryRow(r.Context(), `
			insert into backup_runs(target_type, target_id, status)
			values ('database', $1, 'queued')
			returning id::text
		`, targetID).Scan(&backupID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if h.RiverClient != nil {
			if _, err := h.RiverClient.Insert(r.Context(), riverjobs.BackupJobArgs{
				TargetType: "database",
				TargetID:   targetID,
			}, nil); err != nil {
				http.Error(w, `{"message":"failed to enqueue backup"}`, http.StatusInternalServerError)
				return
			}
		}
		_, _ = h.Pool.Exec(r.Context(), `update schedules set last_run_at = now() where id = $1::uuid`, id)
		common.WriteJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "backupId": backupID})
	default:
		http.Error(w, `{"message":"unsupported schedule target type"}`, http.StatusBadRequest)
		return
	}
}
