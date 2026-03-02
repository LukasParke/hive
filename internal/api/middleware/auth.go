package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

func Auth(authBaseURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sessionCookie, err := r.Cookie("better-auth.session_token")
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			session, err := validateSession(r.Context(), authBaseURL, sessionCookie.Value)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, &session.User)
			ctx = context.WithValue(ctx, SessionContextKey, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func validateSession(ctx context.Context, authBaseURL, token string) (*SessionData, error) {
	url := strings.TrimRight(authBaseURL, "/") + "/api/auth/get-session"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", fmt.Sprintf("better-auth.session_token=%s", token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth returned status %d", resp.StatusCode)
	}

	var session SessionData
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}

	if session.User.ID == "" {
		return nil, fmt.Errorf("invalid session: no user ID")
	}

	return &session, nil
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
