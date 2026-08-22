package registries

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newRouter wires the real auth middleware around every registry endpoint so
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
		gr.Get("/api/v1/registries", h.ListRegistries)
		gr.Post("/api/v1/registries", h.CreateRegistry)
		gr.Get("/api/v1/registries/{id}", h.GetRegistry)
		gr.Put("/api/v1/registries/{id}", h.UpdateRegistry)
		gr.Delete("/api/v1/registries/{id}", h.DeleteRegistry)
		gr.Post("/api/v1/registries/{id}/test", h.TestRegistry)
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

func seedRegistry(t *testing.T, router http.Handler, headers http.Header, name, url string) string {
	t.Helper()
	rec := doJSON(router, http.MethodPost, "/api/v1/registries", headers,
		`{"name":"`+name+`","url":"`+url+`","username":"ci","secretName":"reg-pass"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed registry status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("seed registry response = %s err=%v", rec.Body.String(), err)
	}
	return resp.ID
}

func TestListRegistries(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)

	// Empty list first.
	rec := doJSON(router, http.MethodGet, "/api/v1/registries", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list status = %d body=%s", rec.Code, rec.Body.String())
	}

	first := seedRegistry(t, router, org.Headers, "ghcr", "https://ghcr.io")
	time.Sleep(2 * time.Millisecond)
	second := seedRegistry(t, router, org.Headers, "dockerhub", "https://registry-1.docker.io")

	rec = doJSON(router, http.MethodGet, "/api/v1/registries", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			URL       string `json:"url"`
			Username  string `json:"username"`
			IsDefault bool   `json:"isDefault"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(resp.Items))
	}
	// Ordered by created_at desc: newest first.
	if resp.Items[0].ID != second || resp.Items[1].ID != first {
		t.Fatalf("order = [%s %s], want newest first", resp.Items[0].ID, resp.Items[1].ID)
	}
	if resp.Items[0].URL != "https://registry-1.docker.io" || resp.Items[0].Username != "ci" || resp.Items[0].IsDefault {
		t.Fatalf("item = %+v", resp.Items[0])
	}
}

func TestCreateRegistry(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)

	rec := doJSON(router, http.MethodPost, "/api/v1/registries", org.Headers,
		`{"name":"ghcr","url":"https://ghcr.io","username":"ci","secretName":"reg-pass","isDefault":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("response = %s err=%v", rec.Body.String(), err)
	}
	if got := testdb.QueryCount(t, `select count(*) from registries where id=$1::uuid and is_default and secret_name='reg-pass'`, resp.ID); got != 1 {
		t.Fatalf("registry rows = %d, want 1", got)
	}
}

func TestCreateRegistryValidation(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	cases := []struct {
		name string
		body string
	}{
		{"missing name", `{"url":"https://x.io"}`},
		{"missing url", `{"name":"x"}`},
		{"malformed json", `{"name":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, http.MethodPost, "/api/v1/registries", org.Headers, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}

	t.Run("duplicate name", func(t *testing.T) {
		seedRegistry(t, router, org.Headers, "dupe", "https://a.io")
		rec := doJSON(router, http.MethodPost, "/api/v1/registries", org.Headers,
			`{"name":"dupe","url":"https://b.io"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

func TestGetRegistry(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedRegistry(t, router, org.Headers, "ghcr", "https://ghcr.io")

	rec := doJSON(router, http.MethodGet, "/api/v1/registries/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		URL        string `json:"url"`
		SecretName string `json:"secretName"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != id || resp.Name != "ghcr" || resp.SecretName != "reg-pass" {
		t.Fatalf("registry = %+v", resp)
	}

	rec = doJSON(router, http.MethodGet, "/api/v1/registries/11111111-1111-1111-1111-111111111111", org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", rec.Code)
	}
}

func TestUpdateRegistry(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedRegistry(t, router, org.Headers, "old", "https://old.io")

	rec := doJSON(router, http.MethodPut, "/api/v1/registries/"+id, org.Headers,
		`{"name":"new","url":"https://new.io","username":"deploy","secretName":"other","isDefault":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Name       string `json:"name"`
		URL        string `json:"url"`
		Username   string `json:"username"`
		SecretName string `json:"secretName"`
		IsDefault  bool   `json:"isDefault"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Name != "new" || resp.URL != "https://new.io" || resp.Username != "deploy" || resp.SecretName != "other" || !resp.IsDefault {
		t.Fatalf("updated registry = %+v", resp)
	}

	// Partial update: blank strings keep stored values, nil isDefault untouched.
	rec = doJSON(router, http.MethodPut, "/api/v1/registries/"+id, org.Headers, `{"username":"ci2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode partial: %v", err)
	}
	if resp.Name != "new" || resp.URL != "https://new.io" || resp.Username != "ci2" || !resp.IsDefault {
		t.Fatalf("partially updated registry = %+v", resp)
	}

	rec = doJSON(router, http.MethodPut, "/api/v1/registries/11111111-1111-1111-1111-111111111111", org.Headers, `{"name":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing update status = %d, want 404", rec.Code)
	}

	rec = doJSON(router, http.MethodPut, "/api/v1/registries/"+id, org.Headers, `{`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed update status = %d, want 400", rec.Code)
	}
}

func TestDeleteRegistry(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedRegistry(t, router, org.Headers, "ghcr", "https://ghcr.io")

	rec := doJSON(router, http.MethodDelete, "/api/v1/registries/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from registries where id=$1::uuid`, id); n != 0 {
		t.Fatalf("registry rows after delete = %d, want 0", n)
	}

	rec = doJSON(router, http.MethodDelete, "/api/v1/registries/11111111-1111-1111-1111-111111111111", org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing delete status = %d, want 404", rec.Code)
	}
}

func TestRegistryAdminOnly(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	member := org.AddMember(t, rbac.RoleMember)
	id := seedRegistry(t, router, org.Headers, "ghcr", "https://ghcr.io")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"update", http.MethodPut, "/api/v1/registries/" + id, `{"name":"hax"}`},
		{"delete", http.MethodDelete, "/api/v1/registries/" + id, ""},
		{"test", http.MethodPost, "/api/v1/registries/" + id + "/test", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, tc.method, tc.path, member.Headers, tc.body)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s as member status = %d, want 403", tc.name, rec.Code)
			}
		})
	}
}

func TestTestRegistryLiveCheck(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)

	t.Run("up", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v2/" {
				t.Errorf("probe path = %q, want /v2/", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		id := seedRegistry(t, router, org.Headers, "up", srv.URL)
		rec := doJSON(router, http.MethodPost, "/api/v1/registries/"+id+"/test", org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"ok"`) {
			t.Fatalf("body = %s, want status ok", rec.Body.String())
		}
	})

	t.Run("responds 401", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		id := seedRegistry(t, router, org.Headers, "authfail", srv.URL)
		rec := doJSON(router, http.MethodPost, "/api/v1/registries/"+id+"/test", org.Headers, "")
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "status 401") {
			t.Fatalf("body = %s, want status 401 message", rec.Body.String())
		}
	})

	t.Run("down", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		srv.Close() // closed immediately: connection refused
		id := seedRegistry(t, router, org.Headers, "down", srv.URL)
		rec := doJSON(router, http.MethodPost, "/api/v1/registries/"+id+"/test", org.Headers, "")
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "registry check failed") {
			t.Fatalf("body = %s, want check failed message", rec.Body.String())
		}
	})

	t.Run("missing url", func(t *testing.T) {
		id := seedRegistry(t, router, org.Headers, "empty", "https://placeholder.io")
		_, _ = testdb.Get(t).Exec(t.Context(), `update registries set url='' where id=$1::uuid`, id)
		rec := doJSON(router, http.MethodPost, "/api/v1/registries/"+id+"/test", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("unparseable url", func(t *testing.T) {
		id := seedRegistry(t, router, org.Headers, "badurl", "http://exa mple.invalid")
		rec := doJSON(router, http.MethodPost, "/api/v1/registries/"+id+"/test", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "invalid registry url") {
			t.Fatalf("body = %s", rec.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/registries/11111111-1111-1111-1111-111111111111/test", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
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

func TestRegistryEndpointsRejectUnauthenticated(t *testing.T) {
	router := newRouter(t)
	testdb.SeedOrg(t)
	id := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/v1/registries"},
		{"get", http.MethodGet, "/api/v1/registries/" + id},
		{"update", http.MethodPut, "/api/v1/registries/" + id},
		{"delete", http.MethodDelete, "/api/v1/registries/" + id},
		{"test", http.MethodPost, "/api/v1/registries/" + id + "/test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, tc.method, tc.path, http.Header{}, "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s status = %d body=%s, want 401", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListRegistriesDatabaseErrorsReturn500(t *testing.T) {
	router := newRouterWithPool(t, simpleProtoPool(t))
	org := testdb.SeedOrg(t)

	t.Run("query failure", func(t *testing.T) {
		renameColumn(t, "registries", "url", "url_gone")
		rec := doJSON(router, http.MethodGet, "/api/v1/registries", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})

	t.Run("scan failure", func(t *testing.T) {
		if _, err := testdb.Get(t).Exec(t.Context(), `
			insert into registries(name, url) values ('no-user', 'https://x.io')
		`); err != nil {
			t.Fatalf("seed registry: %v", err)
		}
		rec := doJSON(router, http.MethodGet, "/api/v1/registries", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})
}

func TestRegistryStatementFailuresReturn400(t *testing.T) {
	t.Run("create insert failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		renameColumn(t, "registries", "secret_name", "secret_name_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/registries", org.Headers,
			`{"name":"x","url":"https://x.io"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("update exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := seedRegistry(t, router, org.Headers, "upd", "https://upd.io")
		renameColumn(t, "registries", "secret_name", "secret_name_gone")
		rec := doJSON(router, http.MethodPut, "/api/v1/registries/"+id, org.Headers, `{"name":"y"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := seedRegistry(t, router, org.Headers, "del", "https://del.io")
		renameTable(t, "registries", "registries_gone")
		rec := doJSON(router, http.MethodDelete, "/api/v1/registries/"+id, org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func TestCreateRegistryInvalidOrgHeaderSucceeds(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	h := org.Headers.Clone()
	h.Set("X-Organization-Id", "{"+org.OrgID+"}")
	rec := doJSON(router, http.MethodPost, "/api/v1/registries", h, `{"name":"braced","url":"https://b.io"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
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
