package notifications

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `select id::text, channel, target, enabled, created_at from notifications order by created_at desc`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, channel, target string
		var enabled bool
		var createdAt time.Time
		if err := rows.Scan(&id, &channel, &target, &enabled, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{"id": id, "channel": channel, "target": target, "enabled": enabled, "createdAt": createdAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) CreateNotification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
		Target  string `json:"target"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Channel == "" || req.Target == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into notifications(channel, target, enabled)
		values ($1, $2, $3)
		returning id::text
	`, req.Channel, req.Target, req.Enabled).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) GetNotification(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var channel, target string
	var enabled bool
	var createdAt time.Time
	if err := h.Pool.QueryRow(r.Context(), `
		select channel, target, enabled, created_at
		from notifications
		where id = $1::uuid
	`, id).Scan(&channel, &target, &enabled, &createdAt); err != nil {
		http.Error(w, `{"message":"notification not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"id": id, "channel": channel, "target": target, "enabled": enabled, "createdAt": createdAt,
	})
}

func (h *Handler) UpdateNotification(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Channel string `json:"channel"`
		Target  string `json:"target"`
		Enabled *bool  `json:"enabled"`
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
		update notifications
		set channel = coalesce(nullif($2,''), channel),
			target = coalesce(nullif($3,''), target),
			enabled = case when $4 then $5 else enabled end
		where id = $1::uuid
	`, id, req.Channel, req.Target, hasEnabled, enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"notification not found"}`, http.StatusNotFound)
		return
	}
	h.GetNotification(w, r)
}

func (h *Handler) DeleteNotification(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	cmd, err := h.Pool.Exec(r.Context(), `delete from notifications where id = $1::uuid`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"notification not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) TestNotification(w http.ResponseWriter, r *http.Request) {
	if _, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var channel, target string
	if err := h.Pool.QueryRow(r.Context(), `select channel, target from notifications where id = $1::uuid`, id).Scan(&channel, &target); err != nil {
		http.Error(w, `{"message":"notification not found"}`, http.StatusNotFound)
		return
	}
	body, _ := json.Marshal(map[string]any{
		"event":   "notification.test",
		"channel": channel,
		"payload": map[string]any{"ok": true},
	})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"message":"invalid notification target"}`, http.StatusBadRequest)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"message":"notification test failed: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		http.Error(w, fmt.Sprintf(`{"message":"notification target responded with status %d"}`, res.StatusCode), http.StatusBadGateway)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
