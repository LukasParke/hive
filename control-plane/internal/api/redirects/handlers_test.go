package redirects

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newRouter wires the real auth middleware around every redirect endpoint so
// JWTs and RBAC are exercised end to end against the shared Postgres pool.
func newRouter(t *testing.T) http.Handler {
	t.Helper()
	return newRouterWithPool(t, testdb.Get(t))
}

func newRouterWithPool(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()
	testdb.TruncateAll(t)
	h := NewHandler(pool)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/redirects", h.ListRedirects)
		gr.Post("/api/v1/redirects", h.CreateRedirect)
		gr.Get("/api/v1/redirects/{id}", h.GetRedirect)
		gr.Put("/api/v1/redirects/{id}", h.UpdateRedirect)
		gr.Delete("/api/v1/redirects/{id}", h.DeleteRedirect)
	})
	return r
}

func doJSON(router http.Handler, method, path string, headers http.Header, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// seedDomain inserts an application and a domain owned by the fixture's org.
func seedDomain(t *testing.T, projectID, hostname string) string {
	t.Helper()
	appID := testdb.SeedApplication(t, projectID, "", "https://github.com/example/repo", nil)
	var id string
	if err := testdb.Get(t).QueryRow(t.Context(), `
		insert into domains(application_id, hostname) values ($1::uuid, $2) returning id::text
	`, appID, hostname).Scan(&id); err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	return id
}

func seedRedirect(t *testing.T, router http.Handler, headers http.Header, domainID, path, target string) string {
	rec := doJSON(router, http.MethodPost, "/api/v1/redirects", headers,
		`{"domainId":"`+domainID+`","path":"`+path+`","target":"`+target+`","statusCode":302,"permanent":false}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed redirect status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("seed redirect response = %s err=%v", rec.Body.String(), err)
	}
	return resp.ID
}

func TestCreateAndGetRedirect(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	domainID := seedDomain(t, org.ProjectID, "app.example.test")

	id := seedRedirect(t, router, org.Headers, domainID, "/old", "https://new.example.test")

	rec := doJSON(router, http.MethodGet, "/api/v1/redirects/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
	var item struct {
		ID         string `json:"id"`
		DomainID   string `json:"domain_id"`
		Path       string `json:"path"`
		Target     string `json:"target"`
		StatusCode int32  `json:"status_code"`
		Permanent  bool   `json:"permanent"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if item.ID != id || item.DomainID != domainID || item.Path != "/old" || item.Target != "https://new.example.test" || item.StatusCode != 302 || item.Permanent {
		t.Fatalf("redirect = %+v", item)
	}
}

func TestListRedirectsOrgScoped(t *testing.T) {
	router := newRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)
	domainA := seedDomain(t, orgA.ProjectID, "a.example.test")
	domainB := seedDomain(t, orgB.ProjectID, "b.example.test")
	seedRedirect(t, router, orgA.Headers, domainA, "/a", "https://a")
	seedRedirect(t, router, orgB.Headers, domainB, "/b", "https://b")

	rec := doJSON(router, http.MethodGet, "/api/v1/redirects", orgA.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			Path string `json:"path"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Path != "/a" {
		t.Fatalf("org A items = %+v, want only /a", resp.Items)
	}
}

func TestUpdateAndDeleteRedirect(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	domainID := seedDomain(t, org.ProjectID, "app.example.test")
	id := seedRedirect(t, router, org.Headers, domainID, "/old", "https://old")

	rec := doJSON(router, http.MethodPut, "/api/v1/redirects/"+id, org.Headers,
		`{"domainId":"`+domainID+`","path":"/fresh","target":"https://fresh","statusCode":301,"permanent":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID != id {
		t.Fatalf("update response = %s err=%v", rec.Body.String(), err)
	}
	if n := testdb.QueryCount(t,
		`select count(*) from redirects where id=$1::uuid and path='/fresh' and target='https://fresh' and status_code=301 and permanent`, id); n != 1 {
		t.Fatalf("updated rows = %d, want 1", n)
	}

	rec = doJSON(router, http.MethodDelete, "/api/v1/redirects/"+id, org.Headers, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s, want 204", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from redirects where id=$1::uuid`, id); n != 0 {
		t.Fatalf("redirect rows after delete = %d, want 0", n)
	}
}

func TestRedirectValidationTable(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	domainID := seedDomain(t, org.ProjectID, "app.example.test")
	id := seedRedirect(t, router, org.Headers, domainID, "/x", "https://x")
	missing := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"get bad uuid", http.MethodGet, "/api/v1/redirects/nope", "", http.StatusBadRequest},
		{"get not found", http.MethodGet, "/api/v1/redirects/" + missing, "", http.StatusNotFound},
		{"create bad json", http.MethodPost, "/api/v1/redirects", `{`, http.StatusBadRequest},
		{"create bad domain", http.MethodPost, "/api/v1/redirects", `{"domainId":"nope"}`, http.StatusBadRequest},
		{"create fk violation", http.MethodPost, "/api/v1/redirects",
			`{"domainId":"` + missing + `","path":"/p","target":"https://t"}`, http.StatusBadRequest},
		{"update bad json", http.MethodPut, "/api/v1/redirects/" + id, `{`, http.StatusBadRequest},
		{"update bad domain", http.MethodPut, "/api/v1/redirects/" + id, `{"domainId":"nope"}`, http.StatusBadRequest},
		{"update bad uuid", http.MethodPut, "/api/v1/redirects/nope", `{"domainId":"` + domainID + `"}`, http.StatusBadRequest},
		{"delete bad uuid", http.MethodDelete, "/api/v1/redirects/nope", "", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, tc.method, tc.path, org.Headers, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("%s %s status = %d body=%s, want %d", tc.method, tc.path, rec.Code, rec.Body.String(), tc.want)
			}
		})

		// :exec queries do not check RowsAffected: unknown-but-valid IDs are
		// silent no-ops, and Get of an unknown ID is a plain 404.
		t.Run("update no-op", func(t *testing.T) {
			rec := doJSON(router, http.MethodPut, "/api/v1/redirects/"+missing, org.Headers,
				`{"domainId":"11111111-1111-1111-1111-111111111111","path":"/p","target":"https://t"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("update no-op status = %d body=%s, want 200", rec.Code, rec.Body.String())
			}
		})
		t.Run("delete no-op", func(t *testing.T) {
			rec := doJSON(router, http.MethodDelete, "/api/v1/redirects/"+missing, org.Headers, "")
			if rec.Code != http.StatusNoContent {
				t.Fatalf("delete no-op status = %d, want 204", rec.Code)
			}
		})
		t.Run("get not found", func(t *testing.T) {
			rec := doJSON(router, http.MethodGet, "/api/v1/redirects/"+missing, org.Headers, "")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("get status = %d body=%s, want 404", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRedirectCrossOrgScoping(t *testing.T) {
	router := newRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)
	domainA := seedDomain(t, orgA.ProjectID, "a.example.test")
	id := seedRedirect(t, router, orgA.Headers, domainA, "/a", "https://a")

	// Org B cannot see, change, or delete org A's redirect.
	rec := doJSON(router, http.MethodGet, "/api/v1/redirects/"+id, orgB.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org get status = %d, want 404", rec.Code)
	}
	rec = doJSON(router, http.MethodPut, "/api/v1/redirects/"+id, orgB.Headers,
		`{"domainId":"`+domainA+`","path":"/hijacked","target":"https://evil"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-org update status = %d, want silent no-op 200", rec.Code)
	}
	if n := testdb.QueryCount(t, `select count(*) from redirects where id=$1::uuid and target='https://evil'`, id); n != 0 {
		t.Fatalf("cross-org update mutated row %d times, want 0", n)
	}
	rec = doJSON(router, http.MethodDelete, "/api/v1/redirects/"+id, orgB.Headers, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cross-org delete status = %d, want silent no-op 204", rec.Code)
	}
	if n := testdb.QueryCount(t, `select count(*) from redirects where id=$1::uuid`, id); n != 1 {
		t.Fatalf("redirect rows after cross-org delete = %d, want 1 (no-op)", n)
	}
}

func TestRedirectForeignMemberForbidden(t *testing.T) {
	router := newRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)

	// Org B's member claims org A in the header: RBAC must reject with 403.
	foreignHeaders := orgB.Headers.Clone()
	foreignHeaders.Set("X-Organization-Id", orgA.OrgID)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/v1/redirects"},
		{"create", http.MethodPost, "/api/v1/redirects"},
		{"get", http.MethodGet, "/api/v1/redirects/11111111-1111-1111-1111-111111111111"},
		{"update", http.MethodPut, "/api/v1/redirects/11111111-1111-1111-1111-111111111111"},
		{"delete", http.MethodDelete, "/api/v1/redirects/11111111-1111-1111-1111-111111111111"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, tc.method, tc.path, foreignHeaders, `{}`)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
			}
		})
	}
}

// renameColumn renames a table column so statements touching it fail at plan
// time, exercising the handlers' database-error branches; it restores itself
// via t.Cleanup.
func renameColumn(t *testing.T, table, from, to string) {
	t.Helper()
	ctx := context.Background()
	p := testdb.Get(t)
	if _, err := p.Exec(ctx, "alter table "+table+" rename column "+from+" to "+to); err != nil {
		t.Fatalf("rename %s.%s: %v", table, from, err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, "alter table "+table+" rename column "+to+" to "+from); err != nil {
			t.Fatalf("restore %s.%s: %v", table, to, err)
		}
	})
}

// bracedOrgHeader wraps the organization id in braces: Postgres accepts the
// form in ::uuid casts but common.ToUUID rejects it, so RBAC passes while the
// handler's own conversion fails with 400 invalid organization id.
func bracedOrgHeader(headers http.Header) http.Header {
	h := headers.Clone()
	h.Set("X-Organization-Id", "{"+h.Get("X-Organization-Id")+"}")
	return h
}

func TestInvalidOrgHeaderReturns400(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	missing := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"list", http.MethodGet, "/api/v1/redirects", ""},
		{"create", http.MethodPost, "/api/v1/redirects", `{"domainId":"11111111-1111-1111-1111-111111111111","path":"/p","target":"https://t"}`},
		{"get", http.MethodGet, "/api/v1/redirects/" + missing, ""},
		{"update", http.MethodPut, "/api/v1/redirects/" + missing, `{"domainId":"11111111-1111-1111-1111-111111111111","path":"/p","target":"https://t"}`},
		{"delete", http.MethodDelete, "/api/v1/redirects/" + missing, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, tc.method, tc.path, bracedOrgHeader(org.Headers), tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %s status = %d body=%s, want 400", tc.method, tc.path, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "invalid organization id") {
				t.Fatalf("body = %s, want invalid organization id", rec.Body.String())
			}
		})
	}
}

func TestListQueryFailureReturns500(t *testing.T) {
	router := newRouterWithPool(t, simpleProtoPool(t))
	org := testdb.SeedOrg(t)
	renameColumn(t, "redirects", "path", "path_gone")
	rec := doJSON(router, http.MethodGet, "/api/v1/redirects", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestUpdateAndDeleteExecFailuresReturn400(t *testing.T) {
	t.Run("update exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := seedRedirect(t, router, org.Headers, seedDomain(t, org.ProjectID, "fail.example.test"), "/x", "https://x")
		renameColumn(t, "redirects", "path", "path_gone")
		rec := doJSON(router, http.MethodPut, "/api/v1/redirects/"+id, org.Headers, `{"domainId":"11111111-1111-1111-1111-111111111111","path":"/p","target":"https://t"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := seedRedirect(t, router, org.Headers, seedDomain(t, org.ProjectID, "fail.example.test"), "/x", "https://x")
		renameTable(t, "redirects", "redirects_gone")
		rec := doJSON(router, http.MethodDelete, "/api/v1/redirects/"+id, org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

// simpleProtoPool returns a second pool over the shared database that uses
// the simple query protocol, so SQL failures surface on every execution
// instead of being masked by cached prepared statements.
func simpleProtoPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testdb.Get(t)
	cfg, err := pgxpool.ParseConfig(shared.Config().ConnString())
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open simple-protocol pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// renameTable renames a table so every statement against it fails; restores
// via t.Cleanup. Uses context.Background because t.Context is canceled by
// the time cleanups run.
func renameTable(t *testing.T, from, to string) {
	t.Helper()
	ctx := context.Background()
	p := testdb.Get(t)
	if _, err := p.Exec(ctx, "alter table "+from+" rename to "+to); err != nil {
		t.Fatalf("rename table %s: %v", from, err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, "alter table "+to+" rename to "+from); err != nil {
			t.Fatalf("restore table %s: %v", to, err)
		}
	})
}
