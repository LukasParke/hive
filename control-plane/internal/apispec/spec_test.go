package apispec

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpec(t *testing.T) {
	rec := httptest.NewRecorder()
	Spec().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "openapi: 3") {
		t.Errorf("body does not start with an OpenAPI 3 document: %q", firstLine(rec.Body.String()))
	}
	if len(OpenAPI) == 0 {
		t.Error("embedded OpenAPI is empty")
	}
}

func TestDocs(t *testing.T) {
	rec := httptest.NewRecorder()
	Docs().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/api", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "cdn.jsdelivr.net/npm/@scalar/api-reference") {
		t.Error("docs page does not load Scalar from the CDN")
	}
	if !strings.Contains(body, "/api/v1/openapi.yaml") {
		t.Error("docs page does not point Scalar at /api/v1/openapi.yaml")
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
