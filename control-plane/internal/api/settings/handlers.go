package settings

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
)

type Handler struct {
	Pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool}
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(), `select key, value from app_settings order by key`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var val any
		_ = json.Unmarshal(raw, &val)
		out[key] = val
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) PutSettings(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	for k, v := range payload {
		raw, _ := json.Marshal(v)
		if _, err := h.Pool.Exec(r.Context(), `
			insert into app_settings(key, value, updated_at)
			values ($1, $2::jsonb, now())
			on conflict (key) do update set value = excluded.value, updated_at = now()
		`, k, string(raw)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
