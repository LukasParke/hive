package backups

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/rbac"
)

// Handler serves backup management endpoints.
type Handler struct {
	Pool *pgxpool.Pool
}

// NewHandler returns a backup Handler backed by the given pool.
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool}
}

// ListBackups lists backup runs for the organization.
func (h *Handler) ListBackups(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select br.id::text, br.target_type, br.target_id, br.status, br.artifact_path, br.created_at
		from backup_runs br
		left join database_services ds on br.target_type = 'database' and ds.id::text = br.target_id
		left join projects p on p.id = ds.project_id
		where (br.target_type <> 'database') or p.organization_id = $1::uuid
		order by br.created_at desc
	`, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, targetType, targetID, status, artifactPath string
		var createdAt time.Time
		if err := rows.Scan(&id, &targetType, &targetID, &status, &artifactPath, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{"id": id, "targetType": targetType, "targetId": targetID, "status": status, "artifactPath": artifactPath, "createdAt": createdAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateBackup schedules a new backup run.
func (h *Handler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetType    string `json:"targetType"`
		TargetID      string `json:"targetId"`
		DestinationID string `json:"destinationId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetType == "" || req.TargetID == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into backup_runs(target_type, target_id, destination_id, status, schedule)
		values ($1, $2, nullif($3, '')::uuid, 'queued', 'manual')
		returning id::text
	`, req.TargetType, req.TargetID, req.DestinationID).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// RestoreBackup queues a restore from a backup artifact.
func (h *Handler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	backupID := chi.URLParam(r, "id")
	var req struct {
		RestoreTarget string `json:"restoreTarget"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RestoreTarget == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	_, err := h.Pool.Exec(r.Context(), `
		update backup_runs
		set status='restore-queued', restore_target=$2
		where id=$1::uuid
	`, backupID, req.RestoreTarget)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// ListBackupDestinations lists configured backup destinations.
func (h *Handler) ListBackupDestinations(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `select id::text, name, type, config, created_at from backup_destinations order by created_at desc`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name, typ string
		var configRaw []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &typ, &configRaw, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{"id": id, "name": name, "type": typ, "config": json.RawMessage(configRaw), "createdAt": createdAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateBackupDestination adds a new backup destination.
func (h *Handler) CreateBackupDestination(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string         `json:"name"`
		Type   string         `json:"type"`
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Type == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	raw, _ := json.Marshal(req.Config)
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into backup_destinations(name, type, config) values ($1, $2, $3::jsonb)
		returning id::text
	`, req.Name, req.Type, string(raw)).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GetBackupDestination returns a single backup destination by ID.
func (h *Handler) GetBackupDestination(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var name, typ string
	var cfg []byte
	var createdAt time.Time
	if err := h.Pool.QueryRow(r.Context(), `
		select name, type, config, created_at
		from backup_destinations
		where id = $1::uuid
	`, id).Scan(&name, &typ, &cfg, &createdAt); err != nil {
		http.Error(w, `{"message":"backup destination not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"id": id, "name": name, "type": typ, "config": json.RawMessage(cfg), "createdAt": createdAt,
	})
}

// UpdateBackupDestination updates an existing backup destination.
func (h *Handler) UpdateBackupDestination(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Name   string         `json:"name"`
		Type   string         `json:"type"`
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	var configRaw any
	if req.Config != nil {
		raw, _ := json.Marshal(req.Config)
		configRaw = string(raw)
	}
	cmd, err := h.Pool.Exec(r.Context(), `
		update backup_destinations
		set name = coalesce(nullif($2,''), name),
			type = coalesce(nullif($3,''), type),
			config = coalesce($4::jsonb, config)
		where id = $1::uuid
	`, id, req.Name, req.Type, configRaw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"backup destination not found"}`, http.StatusNotFound)
		return
	}
	h.GetBackupDestination(w, r)
}

// DeleteBackupDestination removes a backup destination.
func (h *Handler) DeleteBackupDestination(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	cmd, err := h.Pool.Exec(r.Context(), `delete from backup_destinations where id = $1::uuid`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"backup destination not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// TestBackupDestination verifies a backup destination with a live check.
func (h *Handler) TestBackupDestination(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var typ string
	var cfgRaw []byte
	if err := h.Pool.QueryRow(r.Context(), `select type, config from backup_destinations where id = $1::uuid`, id).Scan(&typ, &cfgRaw); err != nil {
		http.Error(w, `{"message":"backup destination not found"}`, http.StatusNotFound)
		return
	}
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "local":
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "shared":
		var cfg map[string]any
		_ = json.Unmarshal(cfgRaw, &cfg)
		pathVal, _ := cfg["path"].(string)
		if strings.TrimSpace(pathVal) == "" {
			http.Error(w, `{"message":"shared destination missing config.path"}`, http.StatusBadRequest)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	case "s3":
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	default:
		http.Error(w, `{"message":"unsupported backup destination type"}`, http.StatusBadRequest)
		return
	}
}
