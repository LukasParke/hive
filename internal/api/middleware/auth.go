package middleware

import (
	"context"
	"net/http"

	"github.com/lholliger/hive/internal/auth"
)

type contextKey string

const (
	UserContextKey    contextKey = "user"
	SessionContextKey contextKey = "session"
)

type SessionUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type SessionData struct {
	User    SessionUser `json:"user"`
	Session struct {
		ID    string `json:"id"`
		OrgID string `json:"activeOrganizationId"`
	} `json:"session"`
}

func Auth(authSvc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessionCookie, err := r.Cookie(auth.CookieName)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			sess, user, err := authSvc.ValidateSession(r.Context(), sessionCookie.Value)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			sd := &SessionData{
				User: SessionUser{
					ID:    user.ID,
					Email: user.Email,
					Name:  user.Name,
				},
			}
			sd.Session.ID = sess.ID
			sd.Session.OrgID = sess.ActiveOrg

			ctx := context.WithValue(r.Context(), UserContextKey, &sd.User)
			ctx = context.WithValue(ctx, SessionContextKey, sd)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUser(ctx context.Context) *SessionUser {
	u, _ := ctx.Value(UserContextKey).(*SessionUser)
	return u
}

func GetSession(ctx context.Context) *SessionData {
	s, _ := ctx.Value(SessionContextKey).(*SessionData)
	return s
}

func GetOrgID(ctx context.Context) string {
	s := GetSession(ctx)
	if s != nil {
		return s.Session.OrgID
	}
	return ""
}
