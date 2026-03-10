package engine

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"
)

type AuthUser struct {
	UserID string
	OrgID  string
	Role   string
}

type contextKey string

const authUserKey contextKey = "authUser"

var roleHierarchy = map[string]int{
	"viewer": 0,
	"member": 1,
	"admin":  2,
	"owner":  3,
}

func UserFromContext(ctx context.Context) *AuthUser {
	u, _ := ctx.Value(authUserKey).(*AuthUser)
	return u
}

// sessionAuthMiddleware validates BetterAuth session cookies by querying Postgres.
// It also accepts the legacy engine secret via Authorization header for internal
// (SvelteKit-to-engine) calls.
func (s *Server) sessionAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow internal calls with the engine secret (backward compat + SSR loads)
		if s.secret != "" {
			if token := r.Header.Get("Authorization"); token == "Bearer "+s.secret {
				// Internal call — trust X-User-Id / X-Org-Id / X-Role headers if present
				if uid := r.Header.Get("X-User-Id"); uid != "" {
					user := &AuthUser{
						UserID: uid,
						OrgID:  r.Header.Get("X-Org-Id"),
						Role:   r.Header.Get("X-Role"),
					}
					if user.OrgID == "" {
						user.OrgID = "default"
					}
					if user.Role == "" {
						user.Role = "owner"
					}
					ctx := context.WithValue(r.Context(), authUserKey, user)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				// Internal call without user context — allow (engine-to-engine)
				ctx := context.WithValue(r.Context(), authUserKey, &AuthUser{
					UserID: "system",
					OrgID:  "default",
					Role:   "owner",
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// No engine secret set (dev mode) — try session cookie, fall through if missing
		if s.secret == "" && s.store == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Read the BetterAuth session cookie
		sessionToken := ""
		for _, c := range r.Cookies() {
			if c.Name == "better-auth.session_token" {
				sessionToken = c.Value
				break
			}
		}

		// BetterAuth may sign the token as "token.signature" — use the full value for lookup
		if sessionToken == "" {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "unauthorized", nil)
			return
		}

		// Strip signature if present (BetterAuth stores just the token part in the DB)
		dbToken := sessionToken
		if parts := strings.SplitN(sessionToken, ".", 2); len(parts) == 2 {
			dbToken = parts[0]
		}

		// Query session table
		var userID, activeOrg string
		var expiresAt time.Time
		err := s.store.DB().QueryRowContext(r.Context(),
			`SELECT "userId", "expiresAt", COALESCE("activeOrganizationId", 'default') FROM "session" WHERE "token" = $1`,
			dbToken,
		).Scan(&userID, &expiresAt, &activeOrg)
		if err == sql.ErrNoRows {
			writeAPIError(w, http.StatusUnauthorized, "invalid_session", "invalid session", nil)
			return
		}
		if err != nil {
			s.log.Errorw("session lookup failed", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "auth_error", "auth error", nil)
			return
		}

		if time.Now().After(expiresAt) {
			writeAPIError(w, http.StatusUnauthorized, "session_expired", "session expired", nil)
			return
		}

		// Look up org role
		role := "viewer"
		orgRole, err := s.store.GetOrgRole(r.Context(), activeOrg, userID)
		if err == nil && orgRole != nil {
			role = orgRole.Role
		}

		user := &AuthUser{
			UserID: userID,
			OrgID:  activeOrg,
			Role:   role,
		}
		ctx := context.WithValue(r.Context(), authUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireRole is a per-handler helper that checks the minimum role level.
func requireRole(r *http.Request, minRole string) (*AuthUser, error) {
	user := UserFromContext(r.Context())
	if user == nil {
		return nil, errUnauthorized
	}
	required := roleHierarchy[minRole]
	actual := roleHierarchy[user.Role]
	if actual < required {
		return nil, errForbidden
	}
	return user, nil
}

// requireViewer is a convenience for read-only endpoints.
func requireViewer(r *http.Request) (*AuthUser, error) {
	return requireRole(r, "viewer")
}

// requireMember is a convenience for write endpoints.
func requireMember(r *http.Request) (*AuthUser, error) {
	return requireRole(r, "member")
}

// requireAdmin is a convenience for admin endpoints.
func requireAdmin(r *http.Request) (*AuthUser, error) {
	return requireRole(r, "admin")
}

// requireOwner is a convenience for owner-only endpoints.
func requireOwner(r *http.Request) (*AuthUser, error) {
	return requireRole(r, "owner")
}

type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string { return e.Message }

var (
	errUnauthorized = &apiError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "unauthorized"}
	errForbidden    = &apiError{Status: http.StatusForbidden, Code: "forbidden", Message: "insufficient permissions"}
	errNotFound     = &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "not found"}
	errBadRequest   = &apiError{Status: http.StatusBadRequest, Code: "bad_request", Message: "bad request"}
)

func writeAPIError(w http.ResponseWriter, status int, code, message string, details any) {
	if code == "" {
		code = "internal_error"
	}
	if message == "" {
		message = "request failed"
	}
	resp := map[string]any{
		"error":   message,
		"code":    code,
		"message": message,
	}
	if details != nil {
		resp["details"] = details
	}
	writeJSON(w, status, resp)
}

// handleErr writes an error response. Returns true if an error was handled.
func handleErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if ae, ok := err.(*apiError); ok {
		writeAPIError(w, ae.Status, ae.Code, ae.Message, nil)
		return true
	}
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusNotFound, "not_found", "not found", nil)
		return true
	}
	writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
	return true
}
