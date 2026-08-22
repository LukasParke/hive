package mounts

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

// newRouter wires the real auth middleware around every mount endpoint so
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
		gr.Get("/api/v1/mounts", h.ListMounts)
		gr.Post("/api/v1/mounts", h.CreateMount)
		gr.Get("/api/v1/mounts/{id}", h.GetMount)
		gr.Put("/api/v1/mounts/{id}", h.UpdateMount)
		gr.Delete("/api/v1/mounts/{id}", h.DeleteMount)
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

func seedApp(t *testing.T, projectID string) string {
	t.Helper()
	return testdb.SeedApplication(t, projectID, "", "https://github.com/example/repo", nil)
}

func seedMount(t *testing.T, router http.Handler, headers http.Header, appID, typ, source, target string) string {
	t.Helper()
	rec := doJSON(router, http.MethodPost, "/api/v1/mounts", headers,
		`{"applicationId":"`+appID+`","type":"`+typ+`","source":"`+source+`","target":"`+target+`","readOnly":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed mount status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("seed mount response = %s err=%v", rec.Body.String(), err)
	}
	return resp.ID
}

func TestCreateAndGetMount(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	appID := seedApp(t, org.ProjectID)

	id := seedMount(t, router, org.Headers, appID, "volume", "app-data", "/var/lib/app")

	rec := doJSON(router, http.MethodGet, "/api/v1/mounts/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
	var item struct {
		ID            string `json:"id"`
		ApplicationID string `json:"application_id"`
		Type          string `json:"type"`
		Source        string `json:"source"`
		Target        string `json:"target"`
		ReadOnly      bool   `json:"read_only"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if item.ID != id || item.ApplicationID != appID || item.Type != "volume" || item.Source != "app-data" || item.Target != "/var/lib/app" || !item.ReadOnly {
		t.Fatalf("mount = %+v", item)
	}
}

func TestListMountsOrgScoped(t *testing.T) {
	router := newRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)
	seedMount(t, router, orgA.Headers, seedApp(t, orgA.ProjectID), "volume", "a-data", "/data")
	seedMount(t, router, orgB.Headers, seedApp(t, orgB.ProjectID), "bind", "b-src", "/srv")

	for _, tc := range []struct {
		headers  func() http.Header
		wantType string
	}{
		{orgA.Headers.Clone, "volume"},
		{orgB.Headers.Clone, "bind"},
	} {
		rec := doJSON(router, http.MethodGet, "/api/v1/mounts", tc.headers(), "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []struct {
				ID     string `json:"id"`
				Type   string `json:"type"`
				Source string `json:"source"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(resp.Items) != 1 || resp.Items[0].Type != tc.wantType {
			t.Fatalf("items = %+v, want single %s mount", resp.Items, tc.wantType)
		}
	}
}

func TestUpdateAndDeleteMount(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	appID := seedApp(t, org.ProjectID)
	id := seedMount(t, router, org.Headers, appID, "tmpfs", "", "/tmp/cache")

	rec := doJSON(router, http.MethodPut, "/api/v1/mounts/"+id, org.Headers,
		`{"applicationId":"`+appID+`","type":"bind","source":"/host/src","target":"/container","readOnly":false}`)
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
		`select count(*) from mounts where id=$1::uuid and type='bind' and source='/host/src' and target='/container' and not read_only`, id); n != 1 {
		t.Fatalf("updated rows = %d, want 1", n)
	}

	rec = doJSON(router, http.MethodDelete, "/api/v1/mounts/"+id, org.Headers, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s, want 204", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from mounts where id=$1::uuid`, id); n != 0 {
		t.Fatalf("mount rows after delete = %d, want 0", n)
	}
}

func TestMountValidationTable(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	appID := seedApp(t, org.ProjectID)
	id := seedMount(t, router, org.Headers, appID, "volume", "v", "/v")
	missing := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"get bad uuid", http.MethodGet, "/api/v1/mounts/nope", ""},
		{"create bad json", http.MethodPost, "/api/v1/mounts", `{`},
		{"create bad app id", http.MethodPost, "/api/v1/mounts", `{"applicationId":"nope"}`},
		{"create fk violation", http.MethodPost, "/api/v1/mounts",
			`{"applicationId":"` + missing + `","type":"volume","source":"s","target":"/t"}`},
		{"create bad type constraint", http.MethodPost, "/api/v1/mounts",
			`{"applicationId":"` + appID + `","type":"nfs","source":"s","target":"/t"}`},
		{"update bad json", http.MethodPut, "/api/v1/mounts/" + id, `{`},
		{"update bad app id", http.MethodPut, "/api/v1/mounts/" + id, `{"applicationId":"nope"}`},
		{"update bad uuid", http.MethodPut, "/api/v1/mounts/nope", `{"applicationId":"` + appID + `"}`},
		{"delete bad uuid", http.MethodDelete, "/api/v1/mounts/nope", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, tc.method, tc.path, org.Headers, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %s status = %d body=%s, want 400", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})

		// :exec queries do not check RowsAffected: unknown-but-valid IDs are
		// silent no-ops, and Get of an unknown ID is a plain 404.
		t.Run("update no-op", func(t *testing.T) {
			rec := doJSON(router, http.MethodPut, "/api/v1/mounts/"+missing, org.Headers,
				`{"applicationId":"11111111-1111-1111-1111-111111111111","type":"volume","source":"s","target":"/t"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("update no-op status = %d body=%s, want 200", rec.Code, rec.Body.String())
			}
		})
		t.Run("delete no-op", func(t *testing.T) {
			rec := doJSON(router, http.MethodDelete, "/api/v1/mounts/"+missing, org.Headers, "")
			if rec.Code != http.StatusNoContent {
				t.Fatalf("delete no-op status = %d, want 204", rec.Code)
			}
		})
		t.Run("get not found", func(t *testing.T) {
			rec := doJSON(router, http.MethodGet, "/api/v1/mounts/"+missing, org.Headers, "")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("get status = %d body=%s, want 404", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestMountCrossOrgScoping(t *testing.T) {
	router := newRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)
	id := seedMount(t, router, orgA.Headers, seedApp(t, orgA.ProjectID), "volume", "a-data", "/data")

	rec := doJSON(router, http.MethodGet, "/api/v1/mounts/"+id, orgB.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org get status = %d, want 404", rec.Code)
	}
	// Update matches zero rows for org B: silent no-op 200, row untouched.
	appB := seedApp(t, orgB.ProjectID)
	rec = doJSON(router, http.MethodPut, "/api/v1/mounts/"+id, orgB.Headers,
		`{"applicationId":"`+appB+`","type":"bind","source":"evil","target":"/evil"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-org update status = %d body=%s, want silent no-op 200", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from mounts where id=$1::uuid and source='evil'`, id); n != 0 {
		t.Fatalf("cross-org update mutated row %d times, want 0", n)
	}

	rec = doJSON(router, http.MethodDelete, "/api/v1/mounts/"+id, orgB.Headers, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cross-org delete status = %d, want silent no-op 204", rec.Code)
	}
	if n := testdb.QueryCount(t, `select count(*) from mounts where id=$1::uuid`, id); n != 1 {
		t.Fatalf("mount rows after cross-org delete = %d, want 1 (no-op)", n)
	}
}

func TestMountForeignMemberForbidden(t *testing.T) {
	router := newRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)

	foreignHeaders := orgB.Headers.Clone()
	foreignHeaders.Set("X-Organization-Id", orgA.OrgID)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/v1/mounts"},
		{"create", http.MethodPost, "/api/v1/mounts"},
		{"get", http.MethodGet, "/api/v1/mounts/11111111-1111-1111-1111-111111111111"},
		{"update", http.MethodPut, "/api/v1/mounts/11111111-1111-1111-1111-111111111111"},
		{"delete", http.MethodDelete, "/api/v1/mounts/11111111-1111-1111-1111-111111111111"},
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
		{"list", http.MethodGet, "/api/v1/mounts", ""},
		{"create", http.MethodPost, "/api/v1/mounts", `{"applicationId":"11111111-1111-1111-1111-111111111111","type":"volume","source":"s","target":"/t"}`},
		{"get", http.MethodGet, "/api/v1/mounts/" + missing, ""},
		{"update", http.MethodPut, "/api/v1/mounts/" + missing, `{"applicationId":"11111111-1111-1111-1111-111111111111","type":"volume","source":"s","target":"/t"}`},
		{"delete", http.MethodDelete, "/api/v1/mounts/" + missing, ""},
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
	renameColumn(t, "mounts", "source", "source_gone")
	rec := doJSON(router, http.MethodGet, "/api/v1/mounts", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestUpdateAndDeleteExecFailuresReturn400(t *testing.T) {
	t.Run("update exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := seedMount(t, router, org.Headers, seedApp(t, org.ProjectID), "volume", "v", "/v")
		renameColumn(t, "mounts", "source", "source_gone")
		rec := doJSON(router, http.MethodPut, "/api/v1/mounts/"+id, org.Headers, `{"applicationId":"11111111-1111-1111-1111-111111111111","type":"volume","source":"s","target":"/t"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := seedMount(t, router, org.Headers, seedApp(t, org.ProjectID), "volume", "v", "/v")
		renameTable(t, "mounts", "mounts_gone")
		rec := doJSON(router, http.MethodDelete, "/api/v1/mounts/"+id, org.Headers, "")
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
