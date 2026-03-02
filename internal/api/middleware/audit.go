package middleware

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/store"
)

func AuditLogger(s *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mutating := r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete
			if !mutating {
				next.ServeHTTP(w, r)
				return
			}

			sess := GetSession(r.Context())
			userID := ""
			orgID := ""
			if sess != nil {
				userID = sess.User.ID
				orgID = sess.Session.OrgID
				if orgID == "" {
					orgID = "default"
				}
			}

			wrapped := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			if s == nil || userID == "" {
				return
			}

			// Only audit successful or client-side requests, skip server errors that indicate bugs
			if wrapped.status >= 500 {
				return
			}

			resource := r.URL.Path
			resourceID := ""
			if id := chi.URLParam(r, "projectId"); id != "" {
				resourceID = id
			} else if id := chi.URLParam(r, "appId"); id != "" {
				resourceID = id
			} else if id := chi.URLParam(r, "taskId"); id != "" {
				resourceID = id
			} else if id := chi.URLParam(r, "userId"); id != "" {
				resourceID = id
			}

			al := &store.AuditLog{
				UserID:     userID,
				OrgID:      orgID,
				Action:     r.Method,
				Resource:   resource,
				ResourceID: resourceID,
				Details:    fmt.Sprintf("status:%d", wrapped.status),
			}
			_ = s.CreateAuditLog(r.Context(), al)
		})
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
