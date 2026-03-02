package middleware

import (
	"context"
	"net/http"

	"github.com/lholliger/hive/internal/store"
)

const storeContextKey contextKey = "store"

func StoreMiddleware(s *store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), storeContextKey, s)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetStore(ctx context.Context) *store.Store {
	s, _ := ctx.Value(storeContextKey).(*store.Store)
	return s
}
