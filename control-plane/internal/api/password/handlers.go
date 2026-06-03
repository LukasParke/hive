package password

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	Pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool}
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (h *Handler) SendResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "email is required")
		return
	}
	var userID string
	if err := h.Pool.QueryRow(r.Context(), `select id::text from users where lower(email) = $1`, email).Scan(&userID); err == nil {
		token, err := randomToken(40)
		if err == nil {
			_, _ = h.Pool.Exec(r.Context(), `insert into password_reset_tokens(user_id, token_hash, expires_at) values ($1::uuid, $2, now() + interval '1 hour')`, userID, common.SHA256Hex(token))
			common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "token": token})
			return
		}
	}
	// Privacy-preserving response when user does not exist.
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	if strings.TrimSpace(req.Token) == "" || len(strings.TrimSpace(req.NewPassword)) < 8 {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "token and strong password required")
		return
	}
	var userID string
	var tokenID string
	err := h.Pool.QueryRow(r.Context(), `
		select id::text, user_id::text
		from password_reset_tokens
		where token_hash = $1 and expires_at > now()
		order by created_at desc
		limit 1
	`, common.SHA256Hex(strings.TrimSpace(req.Token))).Scan(&tokenID, &userID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid_token", "invalid or expired token")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to process password")
		return
	}
	if _, err := h.Pool.Exec(r.Context(), `update users set password_hash = $2 where id = $1::uuid`, userID, string(hash)); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update password")
		return
	}
	_, _ = h.Pool.Exec(r.Context(), `delete from password_reset_tokens where id = $1::uuid`, tokenID)
	_, _ = h.Pool.Exec(r.Context(), `delete from sessions where user_id = $1::uuid`, userID)
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
