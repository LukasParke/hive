package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// WithLogger returns a Chi middleware that logs requests using slog.
func WithLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			duration := time.Since(start)
			slog.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", duration),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("request_id", middleware.GetReqID(r.Context())),
			)
		})
	}
}
