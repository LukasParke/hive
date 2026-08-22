package gitproviders

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newGitProvidersRouter wires the real auth middleware in front of the handler.
func newGitProvidersRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()
	h := NewHandler(pool)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/git-providers", h.ListGitProviders)
		gr.Post("/api/v1/git-providers", h.CreateGitProvider)
	})
	return r
}

// deadGitPool returns a closed pool so any query fails immediately.
func deadGitPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(testdb.Get(t).Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatalf("parse conn string: %v", err)
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	p.Close()
	return p
}

func doJSON(t *testing.T, router http.Handler, method, path string, headers http.Header, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestListGitProvidersEmptyAndPopulated(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	router := newGitProvidersRouter(t, testdb.Get(t))

	rec := doJSON(t, router, http.MethodGet, "/api/v1/git-providers", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":null`) {
		t.Fatalf("empty list status = %d body=%s", rec.Code, rec.Body.String())
	}

	testdb.SeedGitProvider(t, "github", "whsec-1")
	testdb.SeedGitProvider(t, "gitlab", "whsec-2")

	rec = doJSON(t, router, http.MethodGet, "/api/v1/git-providers", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Name     string `json:"name"`
			BaseURL  string `json:"baseUrl"`
			Enabled  bool   `json:"enabled"`
			CreateAt string `json:"createdAt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(resp.Items))
	}
	types := map[string]bool{}
	for _, it := range resp.Items {
		types[it.Type] = true
		if it.ID == "" || it.BaseURL != "https://example.test" {
			t.Fatalf("unexpected item %+v", it)
		}
	}
	if !types["github"] || !types["gitlab"] {
		t.Fatalf("missing provider types in %s", rec.Body.String())
	}
}

func TestListGitProvidersQueryFailureReturns500(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)

	// Dropping a selected column makes every fresh preparation of the list
	// query fail; a brand-new pool guarantees nothing is cached.
	if _, err := pool.Exec(context.Background(), `alter table git_providers drop column base_url`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatalf("parse conn string: %v", err)
	}
	fresh, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer fresh.Close()
	router := newGitProvidersRouter(t, fresh)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/git-providers", org.Headers, "")
	if _, err := pool.Exec(context.Background(), `alter table git_providers add column base_url text not null default ''`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestCreateGitProviderHappyPath(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	router := newGitProvidersRouter(t, pool)

	body := `{"type":"github","name":"acme","baseUrl":"https://github.acme.dev","tokenSecretName":"gh-token","webhookSecret":"whsec-x","enabled":true}`
	rec := doJSON(t, router, http.MethodPost, "/api/v1/git-providers", org.Headers, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("decode id: %v %s", err, rec.Body.String())
	}

	var typ, name, baseURL, tokenSecret, webhookSecret string
	var enabled bool
	if err := pool.QueryRow(context.Background(), `
		select type, name, base_url, coalesce(token_secret_name,''), coalesce(webhook_secret,''), enabled
		from git_providers where id = $1::uuid
	`, resp.ID).Scan(&typ, &name, &baseURL, &tokenSecret, &webhookSecret, &enabled); err != nil {
		t.Fatalf("provider row missing: %v", err)
	}
	if typ != "github" || name != "acme" || baseURL != "https://github.acme.dev" ||
		tokenSecret != "gh-token" || webhookSecret != "whsec-x" || !enabled {
		t.Fatalf("row = %v", []any{typ, name, baseURL, tokenSecret, webhookSecret, enabled})
	}
}

func TestCreateGitProviderValidation(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	router := newGitProvidersRouter(t, testdb.Get(t))

	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{"type":`},
		{"missing type", `{"name":"a","baseUrl":"https://x"}`},
		{"missing name", `{"type":"github","baseUrl":"https://x"}`},
		{"missing baseUrl", `{"type":"github","name":"a"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPost, "/api/v1/git-providers", org.Headers, tc.body)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid payload") {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	if n := testdb.QueryCount(t, `select count(*) from git_providers`); n != 0 {
		t.Fatalf("git_providers rows = %d, want 0", n)
	}
}

func TestCreateGitProviderInsertFailureReturns400(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(deadGitPool(t))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/git-providers", strings.NewReader(
		`{"type":"github","name":"a","baseUrl":"https://x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CreateGitProvider(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGitProvidersRequireAuthentication(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	router := newGitProvidersRouter(t, testdb.Get(t))

	rec := doJSON(t, router, http.MethodGet, "/api/v1/git-providers", http.Header{}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list status = %d, want 401", rec.Code)
	}
	rec = doJSON(t, router, http.MethodPost, "/api/v1/git-providers", http.Header{},
		`{"type":"github","name":"a","baseUrl":"https://x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d, want 401", rec.Code)
	}
}

func outsiderHeaders(t *testing.T, orgID string) http.Header {
	t.Helper()
	svc := testdb.Auth(t)
	email := fmt.Sprintf("outsider-%s@test.local", strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
	if _, err := svc.Register(context.Background(), email, "sup3rsecret!", "Outsider"); err != nil {
		t.Fatalf("register outsider: %v", err)
	}
	token, _, err := svc.Login(context.Background(), email, "sup3rsecret!")
	if err != nil {
		t.Fatalf("login outsider: %v", err)
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set("X-Organization-Id", orgID)
	return h
}

func TestGitProvidersDenyNonMembers(t *testing.T) {
	router := newGitProvidersRouter(t, testdb.Get(t))
	org := testdb.SeedOrg(t)
	headers := outsiderHeaders(t, org.OrgID)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/git-providers", headers, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("outsider list status = %d body=%s, want 403", rec.Code, rec.Body.String())
	}
}

func TestListGitProvidersScanFailureReturns500(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	testdb.SeedGitProvider(t, "github", "whsec-scan")

	// Make created_at unscannable as time.Time while the query still succeeds.
	if _, err := pool.Exec(context.Background(),
		`alter table git_providers alter column created_at drop default,
		 alter column created_at type text[] using array['x']`); err != nil {
		t.Fatalf("alter: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatalf("parse conn string: %v", err)
	}
	fresh, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer fresh.Close()
	router := newGitProvidersRouter(t, fresh)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/git-providers", org.Headers, "")
	if _, err := pool.Exec(context.Background(),
		`alter table git_providers alter column created_at type timestamptz using now(),
		 alter column created_at set default now()`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}
