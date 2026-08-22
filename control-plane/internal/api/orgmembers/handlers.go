package orgmembers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/rbac"
)

// Handler serves organization membership endpoints.
type Handler struct {
	Pool *pgxpool.Pool
}

// NewHandler returns an org member Handler backed by the given pool.
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

func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// GetInvitationByToken returns invitation details for a token.
func (h *Handler) GetInvitationByToken(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "token is required")
		return
	}
	var id, organizationID, email, role, status string
	var expiresAt time.Time
	err := h.Pool.QueryRow(r.Context(), `
		select id::text, organization_id::text, email, role::text, status, expires_at
		from organization_invitations
		where token_hash = $1
	`, sha256Hex(token)).Scan(&id, &organizationID, &email, &role, &status, &expiresAt)
	if err != nil || status != "pending" || expiresAt.Before(time.Now()) {
		common.WriteError(w, http.StatusNotFound, "not_found", "invitation not found")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "organizationId": organizationID, "email": email, "role": role, "status": status, "expiresAt": expiresAt})
}

// AcceptInvitationByToken joins the invited user to the organization.
func (h *Handler) AcceptInvitationByToken(w http.ResponseWriter, r *http.Request) {
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing auth")
		return
	}
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "token is required")
		return
	}
	var inviteID, orgID, role, status string
	var expiresAt time.Time
	err := h.Pool.QueryRow(r.Context(), `
		select id::text, organization_id::text, role::text, status, expires_at
		from organization_invitations
		where token_hash = $1
	`, sha256Hex(token)).Scan(&inviteID, &orgID, &role, &status, &expiresAt)
	if err != nil || status != "pending" || expiresAt.Before(time.Now()) {
		common.WriteError(w, http.StatusBadRequest, "invalid_token", "invitation invalid or expired")
		return
	}
	if role == "" {
		role = string(rbac.RoleMember)
	}
	_, _ = h.Pool.Exec(r.Context(), `insert into organization_members(organization_id, user_id, role) values ($1::uuid, $2::uuid, $3::member_role) on conflict (organization_id, user_id) do update set role = excluded.role`, orgID, claims.UserID, role)
	_, _ = h.Pool.Exec(r.Context(), `update organization_invitations set status = 'accepted' where id = $1::uuid`, inviteID)
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "accepted", "organizationId": orgID})
}

// ListOrganizationMembers lists an organization's members.
func (h *Handler) ListOrganizationMembers(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	if orgID != chi.URLParam(r, "id") {
		common.WriteError(w, http.StatusForbidden, "forbidden", "active organization mismatch")
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select u.id::text, u.email, u.display_name, om.role::text, om.created_at
		from organization_members om
		join users u on u.id = om.user_id
		where om.organization_id = $1::uuid
		order by om.created_at asc
	`, orgID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list members")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, email, displayName, role string
		var createdAt time.Time
		if err := rows.Scan(&id, &email, &displayName, &role, &createdAt); err == nil {
			out = append(out, map[string]any{"userId": id, "email": email, "displayName": displayName, "role": role, "createdAt": createdAt})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// UpdateOrganizationMemberRole changes a member's role.
func (h *Handler) UpdateOrganizationMemberRole(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner)
	if !ok {
		return
	}
	if orgID != chi.URLParam(r, "id") {
		common.WriteError(w, http.StatusForbidden, "forbidden", "active organization mismatch")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	role := strings.TrimSpace(strings.ToLower(req.Role))
	if role != string(rbac.RoleOwner) && role != string(rbac.RoleAdmin) && role != string(rbac.RoleMember) {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid role")
		return
	}
	userID := chi.URLParam(r, "userId")
	_, err := h.Pool.Exec(r.Context(), `update organization_members set role = $3::member_role where organization_id = $1::uuid and user_id = $2::uuid`, orgID, userID, role)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to update role")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ListOrganizationInvitations lists pending invitations.
func (h *Handler) ListOrganizationInvitations(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	if orgID != chi.URLParam(r, "id") {
		common.WriteError(w, http.StatusForbidden, "forbidden", "active organization mismatch")
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select id::text, email, role::text, status, expires_at, resent_at, created_at
		from organization_invitations
		where organization_id = $1::uuid
		order by created_at desc
	`, orgID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list invitations")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, email, role, status string
		var expiresAt, createdAt time.Time
		var resentAt *time.Time
		if err := rows.Scan(&id, &email, &role, &status, &expiresAt, &resentAt, &createdAt); err == nil {
			out = append(out, map[string]any{"id": id, "email": email, "role": role, "status": status, "expiresAt": expiresAt, "resentAt": resentAt, "createdAt": createdAt})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateOrganizationInvitation invites a user to the organization.
func (h *Handler) CreateOrganizationInvitation(w http.ResponseWriter, r *http.Request) {
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing auth")
		return
	}
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	if orgID != chi.URLParam(r, "id") {
		common.WriteError(w, http.StatusForbidden, "forbidden", "active organization mismatch")
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	role := strings.TrimSpace(strings.ToLower(req.Role))
	if email == "" {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "email is required")
		return
	}
	if role == "" {
		role = string(rbac.RoleMember)
	}
	token, err := randomToken(40)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to issue invitation token")
		return
	}
	_, _ = h.Pool.Exec(r.Context(), `delete from organization_invitations where organization_id = $1::uuid and lower(email) = lower($2) and status = 'pending'`, orgID, email)
	var id string
	err = h.Pool.QueryRow(r.Context(), `
		insert into organization_invitations(organization_id, email, role, token_hash, created_by)
		values ($1::uuid, $2, $3::member_role, $4, $5::uuid)
		returning id::text
	`, orgID, email, role, sha256Hex(token), claims.UserID).Scan(&id)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create invitation")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "token": token, "status": "pending"})
}

// DeleteOrganizationInvitation deletes an invitation.
func (h *Handler) DeleteOrganizationInvitation(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	if orgID != chi.URLParam(r, "id") {
		common.WriteError(w, http.StatusForbidden, "forbidden", "active organization mismatch")
		return
	}
	inviteID := chi.URLParam(r, "inviteId")
	_, err := h.Pool.Exec(r.Context(), `delete from organization_invitations where id = $1::uuid and organization_id = $2::uuid`, inviteID, orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to remove invitation")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// ResendOrganizationInvitation re-sends an invitation email.
func (h *Handler) ResendOrganizationInvitation(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	if orgID != chi.URLParam(r, "id") {
		common.WriteError(w, http.StatusForbidden, "forbidden", "active organization mismatch")
		return
	}
	inviteID := chi.URLParam(r, "inviteId")
	var email string
	err := h.Pool.QueryRow(r.Context(), `select email from organization_invitations where id = $1::uuid and organization_id = $2::uuid and status = 'pending'`, inviteID, orgID).Scan(&email)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "pending invitation not found")
		return
	}
	token, err := randomToken(40)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to issue token")
		return
	}
	_, err = h.Pool.Exec(r.Context(), `
		update organization_invitations
		set token_hash = $1, expires_at = now() + interval '7 days', resent_at = now()
		where id = $2::uuid and organization_id = $3::uuid
	`, sha256Hex(token), inviteID, orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to resend invitation")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": inviteID, "token": token, "email": email, "status": "pending"})
}

// RevokeOrganizationInvitation revokes a pending invitation.
func (h *Handler) RevokeOrganizationInvitation(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	if orgID != chi.URLParam(r, "id") {
		common.WriteError(w, http.StatusForbidden, "forbidden", "active organization mismatch")
		return
	}
	inviteID := chi.URLParam(r, "inviteId")
	_, err := h.Pool.Exec(r.Context(), `
		update organization_invitations set status = 'revoked' where id = $1::uuid and organization_id = $2::uuid
	`, inviteID, orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to revoke invitation")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
}
