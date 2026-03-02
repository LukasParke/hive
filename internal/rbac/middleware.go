package rbac

import (
	"encoding/json"
	"net/http"

	"github.com/lholliger/hive/internal/api/middleware"
)

func RequirePermission(perm Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := middleware.GetSession(r.Context())
			if sess == nil {
				write403(w, "no session")
				return
			}
			st := middleware.GetStore(r.Context())
			if st == nil {
				write403(w, "store unavailable")
				return
			}
			orgID := sess.Session.OrgID
			if orgID == "" {
				orgID = "default"
			}
			roleObj, err := st.GetOrgRole(r.Context(), orgID, sess.User.ID)
			role := "viewer"
			if err == nil && roleObj != nil {
				role = roleObj.Role
			}
			if !HasPermission(role, perm) {
				write403(w, "permission denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func write403(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
