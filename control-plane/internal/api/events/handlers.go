package events

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/realtime"
)

// Handler serves the events API and the event websocket stream.
type Handler struct {
	Pool *pgxpool.Pool
	Auth *auth.Service
	Hub  *realtime.Hub
}

// NewHandler returns an events Handler backed by the given pool, auth service, and hub.
func NewHandler(pool *pgxpool.Pool, auth *auth.Service, hub *realtime.Hub) *Handler {
	return &Handler{Pool: pool, Auth: auth, Hub: hub}
}

// WsEvents upgrades the request to a websocket that streams system events.
func (h *Handler) WsEvents(w http.ResponseWriter, r *http.Request) {
	if h.Hub == nil {
		http.Error(w, `{"message":"ws hub unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("access_token"))
	if token == "" {
		http.Error(w, `{"message":"missing access token"}`, http.StatusUnauthorized)
		return
	}
	if _, err := h.Auth.ParseAccessToken(token); err != nil {
		http.Error(w, `{"message":"invalid access token"}`, http.StatusUnauthorized)
		return
	}
	h.Hub.HandleWS(w, r)
}

// ListRequestEvents returns stored events for a request.
func (h *Handler) ListRequestEvents(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(), `select id::text, category, message, payload, created_at from request_events order by created_at desc limit 200`)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list request events")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, category, message string
		var payload []byte
		var createdAt time.Time
		if scanErr := rows.Scan(&id, &category, &message, &payload, &createdAt); scanErr == nil {
			out = append(out, map[string]any{"id": id, "category": category, "message": message, "payload": json.RawMessage(payload), "createdAt": createdAt})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}
