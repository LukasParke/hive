package ctx

import (
	"context"

	"github.com/luke/hive/control-plane/internal/auth"
)

type contextKey string

const claimsKey contextKey = "auth_claims"

// WithClaims returns a new context with auth claims attached.
func WithClaims(c context.Context, claims *auth.Claims) context.Context {
	return context.WithValue(c, claimsKey, claims)
}

// ClaimsFromContext retrieves auth claims from the context.
func ClaimsFromContext(c context.Context) (*auth.Claims, bool) {
	claims, ok := c.Value(claimsKey).(*auth.Claims)
	return claims, ok
}
