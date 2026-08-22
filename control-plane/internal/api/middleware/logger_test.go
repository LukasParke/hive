package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/auth"
)

// withTestClaims attaches claims using the shared ctx helper (same key space
// as the middleware).
func withTestClaims(c context.Context, claims *auth.Claims) context.Context {
	return apicxt.WithClaims(c, claims)
}

// TestWithLoggerPassesThroughAndLogs verifies the wrapped handler still runs
// and one structured log record with method/path/status is emitted.
func TestWithLoggerPassesThroughAndLogs(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	called := false
	h := WithLogger()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodPost, "/logged/path", nil)
	req.RemoteAddr = "10.0.0.7:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("downstream handler never ran")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}

	out := buf.String()
	if !strings.Contains(out, "http request") {
		t.Fatalf("missing log record: %q", out)
	}
	for _, want := range []string{"POST", "/logged/path", "418", "10.0.0.7:1234"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log %q missing %q", out, want)
		}
	}
}

// TestWithLoggerJSONShape asserts the record decodes as structured JSON when a
// JSON handler is installed.
func TestWithLoggerJSONShape(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := WithLogger()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/json-check", nil))

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no log output")
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("log line is not JSON: %v (%s)", err, line)
	}
	if rec["msg"] != "http request" || rec["path"] != "/json-check" {
		t.Fatalf("record = %v", rec)
	}
	if _, ok := rec["duration"]; !ok {
		t.Fatalf("duration missing from record: %v", rec)
	}
}
