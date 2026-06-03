package profile

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"github.com/luke/hive/control-plane/internal/api/common"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
)

type Handler struct {
	Pool *pgxpool.Pool
	Q    *dbgen.Queries
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool, Q: dbgen.New(pool)}
}

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing authentication")
		return
	}
	userUUID, err := common.ToUUID(claims.UserID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid user id")
		return
	}
	profile, err := h.Q.GetUserProfile(r.Context(), userUUID)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	common.WriteJSON(w, http.StatusOK, profile)
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing authentication")
		return
	}
	var req struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	userUUID, err := common.ToUUID(claims.UserID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid user id")
		return
	}
	if err := h.Q.UpdateUserProfile(r.Context(), dbgen.UpdateUserProfileParams{
		ID:          userUUID,
		DisplayName: req.DisplayName,
	}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to update profile")
		return
	}
	profile, err := h.Q.GetUserProfile(r.Context(), userUUID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to fetch updated profile")
		return
	}
	common.WriteJSON(w, http.StatusOK, profile)
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing authentication")
		return
	}
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CurrentPassword == "" || req.NewPassword == "" {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}

	var pwHash string
	err := h.Pool.QueryRow(r.Context(), `select password_hash from users where id = $1::uuid`, claims.UserID).Scan(&pwHash)
	if err != nil {
		common.WriteError(w, http.StatusUnauthorized, "unauthorized", "user not found")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(pwHash), []byte(req.CurrentPassword)) != nil {
		common.WriteError(w, http.StatusUnauthorized, "unauthorized", "incorrect current password")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to hash password")
		return
	}

	userUUID, err := common.ToUUID(claims.UserID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid user id")
		return
	}
	if err := h.Q.UpdateUserPassword(r.Context(), dbgen.UpdateUserPasswordParams{
		ID:           userUUID,
		PasswordHash: string(newHash),
	}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to update password")
		return
	}

	// Invalidate all existing sessions for security.
	_ = h.Q.DeleteUserSessions(r.Context(), userUUID)

	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
