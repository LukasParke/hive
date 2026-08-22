package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/auth"
)

// WithAuth returns middleware that authenticates requests via JWT bearer token or API key.
func WithAuth(svc *auth.Service, pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := strings.TrimSpace(r.Header.Get("Authorization"))
			var claims *auth.Claims
			var err error
			if h != "" && strings.HasPrefix(h, "Bearer ") {
				token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
				claims, err = svc.ParseAccessToken(token)
				if err != nil {
					common.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
					return
				}
			} else {
				rawKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
				if rawKey == "" {
					common.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token or api key")
					return
				}
				sum := sha256.Sum256([]byte(rawKey))
				hash := hex.EncodeToString(sum[:])
				var userID string
				var email string
				if err := pool.QueryRow(r.Context(), `
					select u.id::text, u.email
					from api_keys ak
					join users u on u.id = ak.user_id
					where ak.token_hash = $1 and u.is_active = true
				`, hash).Scan(&userID, &email); err != nil {
					common.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid api key")
					return
				}
				_, _ = pool.Exec(r.Context(), `update api_keys set last_used_at=now() where token_hash=$1`, hash)
				claims = &auth.Claims{UserID: userID, Email: email}
			}
			c := apicxt.WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(c))
		})
	}
}

// ClaimsFromContext retrieves auth claims from the context.
//
// Deprecated: use ctx.ClaimsFromContext instead.
func ClaimsFromContext(c context.Context) (*auth.Claims, bool) {
	return apicxt.ClaimsFromContext(c)
}
