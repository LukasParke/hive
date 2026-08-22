package auth

import (
	"encoding/json"
	"net/http"

	"github.com/luke/hive/control-plane/internal/api/common"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	authservice "github.com/luke/hive/control-plane/internal/auth"
)

// Handler serves authentication endpoints.
type Handler struct {
	Auth *authservice.Service
}

// NewHandler returns an auth Handler using the given auth service.
func NewHandler(auth *authservice.Service) *Handler {
	return &Handler{Auth: auth}
}

// RegisterUser creates a new user account.
func (h *Handler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	id, err := h.Auth.Register(r.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// Login authenticates a user and issues tokens.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	access, refresh, err := h.Auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		http.Error(w, `{"message":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"accessToken": access, "refreshToken": refresh})
}

// Refresh rotates the caller's session tokens.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	access, refresh, err := h.Auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		http.Error(w, `{"message":"invalid refresh token"}`, http.StatusUnauthorized)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"accessToken": access, "refreshToken": refresh})
}

// Logout deletes the caller's session.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	if err := h.Auth.Logout(r.Context(), req.RefreshToken); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Me returns the authenticated user's identity.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": claims.UserID, "email": claims.Email})
}
