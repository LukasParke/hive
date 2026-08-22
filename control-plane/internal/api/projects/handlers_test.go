package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newProjectsRouter wires a real chi router with the same auth middleware
// used in production so JWTs and org headers are exercised end-to-end.
func newProjectsRouter(t *testing.T) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/projects", h.ListProjects)
		gr.Post("/api/v1/projects", h.CreateProject)
		gr.Get("/api/v1/projects/{id}", h.GetProject)
		gr.Put("/api/v1/projects/{id}", h.UpdateProject)
		gr.Delete("/api/v1/projects/{id}", h.DeleteProject)
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

// callHandler invokes a handler method directly, bypassing the auth
// middleware, so the handlers' own unauthorized branches are exercised.
func callHandler(h *Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	switch method {
	case http.MethodGet:
		if strings.HasSuffix(path, "projects") {
			h.ListProjects(rec, req)
		} else {
			h.GetProject(rec, req)
		}
	case http.MethodPost:
		h.CreateProject(rec, req)
	case http.MethodPut:
		h.UpdateProject(rec, req)
	case http.MethodDelete:
		h.DeleteProject(rec, req)
	}
	return rec
}

func seedProjectNamed(t *testing.T, orgID, name string) string {
	t.Helper()
	p := testdb.Get(t)
	var id string
	err := p.QueryRow(context.Background(),
		`insert into projects(name, organization_id) values ($1, $2::uuid) returning id::text`, name, orgID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return id
}

func projectResponse(t *testing.T, body string) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode response %q: %v", body, err)
	}
	return resp
}

func TestListProjects(t *testing.T) {
	router := newProjectsRouter(t)
	org := testdb.SeedOrg(t)
	otherOrg := testdb.SeedOrg(t)
	mine := seedProjectNamed(t, org.OrgID, "mine")
	mine2 := seedProjectNamed(t, org.OrgID, "mine-2")
	seedProjectNamed(t, otherOrg.OrgID, "theirs")

	t.Run("lists only the acting organization's projects", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/projects", org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				CreatedAt string `json:"createdAt"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Items) != 3 {
			t.Fatalf("items = %+v, want the acting org's three projects", resp.Items)
		}
		seen := map[string]string{}
		for _, it := range resp.Items {
			seen[it.ID] = it.Name
			if it.CreatedAt == "" {
				t.Fatalf("item missing createdAt: %+v", it)
			}
		}
		if seen[mine] != "mine" || seen[mine2] != "mine-2" || seen[org.ProjectID] == "" {
			t.Fatalf("seeded projects missing from list: %+v", seen)
		}
		for _, it := range resp.Items {
			if it.ID == "" {
				t.Fatalf("item missing id: %+v", it)
			}
		}
	})

	t.Run("member role can list", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		rec := doJSON(router, http.MethodGet, "/api/v1/projects", member.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
	})

	t.Run("outsider forbidden", func(t *testing.T) {
		intruder := http.Header{}
		intruder.Set("Authorization", "Bearer "+otherOrg.Token)
		intruder.Set("X-Organization-Id", org.OrgID)
		rec := doJSON(router, http.MethodGet, "/api/v1/projects", intruder, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("list query failure surfaces as 500", func(t *testing.T) {
		simple := simpleProtocolRouter(t)
		o := testdb.SeedOrg(t)
		renameColumn(t, "projects", "name", "name_gone")
		rec := doJSON(simple, http.MethodGet, "/api/v1/projects", o.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})

	t.Run("row scan failure surfaces as 500", func(t *testing.T) {
		simple := simpleProtocolRouter(t)
		o := testdb.SeedOrg(t)
		p := testdb.Get(t)
		if _, err := p.Exec(context.Background(), `alter table projects alter column name drop not null`); err != nil {
			t.Fatalf("drop not null: %v", err)
		}
		t.Cleanup(func() {
			if _, err := p.Exec(context.Background(), `delete from projects where name is null`); err != nil {
				t.Fatalf("cleanup null rows: %v", err)
			}
			if _, err := p.Exec(context.Background(), `alter table projects alter column name set not null`); err != nil {
				t.Fatalf("restore not null: %v", err)
			}
		})
		if _, err := p.Exec(context.Background(), `
			insert into projects(organization_id, name) values ($1::uuid, null)
		`, o.OrgID); err != nil {
			t.Fatalf("seed null-name project: %v", err)
		}
		rec := doJSON(simple, http.MethodGet, "/api/v1/projects", o.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})
}

func TestCreateProject(t *testing.T) {
	router := newProjectsRouter(t)
	org := testdb.SeedOrg(t)
	admin := org.AddMember(t, rbac.RoleAdmin)
	member := org.AddMember(t, rbac.RoleMember)
	otherOrg := testdb.SeedOrg(t)

	t.Run("owner creates project", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/projects", org.Headers, `{"name":"fresh"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s, want 201", rec.Code, rec.Body.String())
		}
		resp := projectResponse(t, rec.Body.String())
		if resp["id"] == "" || resp["name"] != "fresh" || resp["createdAt"] == "" {
			t.Fatalf("response = %v, want id/name/createdAt", resp)
		}
	})

	t.Run("admin can create", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/projects", admin.Headers, `{"name":"by-admin"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s, want 201", rec.Code, rec.Body.String())
		}
	})

	t.Run("member forbidden", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/projects", member.Headers, `{"name":"nope"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("outsider forbidden", func(t *testing.T) {
		intruder := http.Header{}
		intruder.Set("Authorization", "Bearer "+otherOrg.Token)
		intruder.Set("X-Organization-Id", org.OrgID)
		rec := doJSON(router, http.MethodPost, "/api/v1/projects", intruder, `{"name":"nope"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid json body", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/projects", org.Headers, `{not json`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("empty name", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/projects", org.Headers, `{"name":""}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("insert failure maps to bad request", func(t *testing.T) {
		simple := simpleProtocolRouter(t)
		o := testdb.SeedOrg(t)
		renameColumn(t, "projects", "name", "name_gone")
		rec := doJSON(simple, http.MethodPost, "/api/v1/projects", o.Headers, `{"name":"boom"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func TestGetProject(t *testing.T) {
	router := newProjectsRouter(t)
	org := testdb.SeedOrg(t)
	otherOrg := testdb.SeedOrg(t)
	member := org.AddMember(t, rbac.RoleMember)
	projectID := seedProjectNamed(t, org.OrgID, "gettable")

	t.Run("owner gets project", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/projects/"+projectID, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		resp := projectResponse(t, rec.Body.String())
		if resp["id"] != projectID || resp["name"] != "gettable" || resp["createdAt"] == "" {
			t.Fatalf("response = %v, want id/name/createdAt for %s", resp, projectID)
		}
	})

	t.Run("member can get", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/projects/"+projectID, member.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown uuid returns 404", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/projects/00000000-0000-0000-0000-000000000000", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
		}
	})

	t.Run("cross-org project returns 404", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/projects/"+otherOrg.ProjectID, org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
		}
	})

	t.Run("outsider forbidden", func(t *testing.T) {
		intruder := http.Header{}
		intruder.Set("Authorization", "Bearer "+otherOrg.Token)
		intruder.Set("X-Organization-Id", org.OrgID)
		rec := doJSON(router, http.MethodGet, "/api/v1/projects/"+projectID, intruder, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})
}

func TestUpdateProject(t *testing.T) {
	router := newProjectsRouter(t)
	org := testdb.SeedOrg(t)
	otherOrg := testdb.SeedOrg(t)
	admin := org.AddMember(t, rbac.RoleAdmin)
	member := org.AddMember(t, rbac.RoleMember)
	projectID := seedProjectNamed(t, org.OrgID, "before")

	t.Run("owner updates and round-trips", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, "/api/v1/projects/"+projectID, org.Headers, `{"name":"after"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		resp := projectResponse(t, rec.Body.String())
		if resp["id"] != projectID || resp["name"] != "after" {
			t.Fatalf("response = %v, want id=%s name=after", resp, projectID)
		}
		got := doJSON(router, http.MethodGet, "/api/v1/projects/"+projectID, org.Headers, "")
		if gotBody := projectResponse(t, got.Body.String()); gotBody["name"] != "after" {
			t.Fatalf("round-trip name = %v, want after", gotBody["name"])
		}
	})

	t.Run("admin can update", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, "/api/v1/projects/"+projectID, admin.Headers, `{"name":"by-admin"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
	})

	t.Run("member forbidden", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, "/api/v1/projects/"+projectID, member.Headers, `{"name":"nope"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("outsider forbidden", func(t *testing.T) {
		intruder := http.Header{}
		intruder.Set("Authorization", "Bearer "+otherOrg.Token)
		intruder.Set("X-Organization-Id", org.OrgID)
		rec := doJSON(router, http.MethodPut, "/api/v1/projects/"+projectID, intruder, `{"name":"nope"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid json body", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, "/api/v1/projects/"+projectID, org.Headers, `{bad`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("empty name", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, "/api/v1/projects/"+projectID, org.Headers, `{"name":""}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("whitespace name", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, "/api/v1/projects/"+projectID, org.Headers, `{"name":"   "}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown project returns 404", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, "/api/v1/projects/00000000-0000-0000-0000-000000000000", org.Headers, `{"name":"ghost"}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
		}
	})

	t.Run("cross-org project returns 404", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, "/api/v1/projects/"+otherOrg.ProjectID, org.Headers, `{"name":"steal"}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
		}
	})

	t.Run("update exec failure maps to bad request", func(t *testing.T) {
		simple := simpleProtocolRouter(t)
		o := testdb.SeedOrg(t)
		id := seedProjectNamed(t, o.OrgID, "victim")
		renameColumn(t, "projects", "name", "name_gone")
		rec := doJSON(simple, http.MethodPut, "/api/v1/projects/"+id, o.Headers, `{"name":"boom"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func TestDeleteProject(t *testing.T) {
	router := newProjectsRouter(t)
	org := testdb.SeedOrg(t)
	otherOrg := testdb.SeedOrg(t)
	admin := org.AddMember(t, rbac.RoleAdmin)
	member := org.AddMember(t, rbac.RoleMember)
	projectID := seedProjectNamed(t, org.OrgID, "doomed")

	t.Run("member forbidden", func(t *testing.T) {
		rec := doJSON(router, http.MethodDelete, "/api/v1/projects/"+projectID, member.Headers, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("outsider forbidden", func(t *testing.T) {
		intruder := http.Header{}
		intruder.Set("Authorization", "Bearer "+otherOrg.Token)
		intruder.Set("X-Organization-Id", org.OrgID)
		rec := doJSON(router, http.MethodDelete, "/api/v1/projects/"+projectID, intruder, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown project returns 404", func(t *testing.T) {
		rec := doJSON(router, http.MethodDelete, "/api/v1/projects/00000000-0000-0000-0000-000000000000", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
		}
	})

	t.Run("cross-org project returns 404", func(t *testing.T) {
		rec := doJSON(router, http.MethodDelete, "/api/v1/projects/"+otherOrg.ProjectID, org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
		}
	})

	t.Run("admin deletes and project is gone", func(t *testing.T) {
		rec := doJSON(router, http.MethodDelete, "/api/v1/projects/"+projectID, admin.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		resp := projectResponse(t, rec.Body.String())
		if resp["status"] != "deleted" {
			t.Fatalf("response = %v, want status=deleted", resp)
		}
		got := doJSON(router, http.MethodGet, "/api/v1/projects/"+projectID, org.Headers, "")
		if got.Code != http.StatusNotFound {
			t.Fatalf("get after delete status = %d, want 404", got.Code)
		}
	})

	t.Run("owner deletes", func(t *testing.T) {
		id := seedProjectNamed(t, org.OrgID, "owner-deleted")
		rec := doJSON(router, http.MethodDelete, "/api/v1/projects/"+id, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete exec failure maps to bad request", func(t *testing.T) {
		simple := simpleProtocolRouter(t)
		o := testdb.SeedOrg(t)
		id := seedProjectNamed(t, o.OrgID, "stuck")
		renameColumn(t, "projects", "id", "id_gone")
		rec := doJSON(simple, http.MethodDelete, "/api/v1/projects/"+id, o.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

// renameColumn renames a column so matching statements fail at parse time; it
// restores itself via t.Cleanup.
func renameColumn(t *testing.T, table, from, to string) {
	t.Helper()
	ctx := context.Background()
	p := testdb.Get(t)
	if _, err := p.Exec(ctx, fmt.Sprintf("alter table %s rename column %s to %s", table, from, to)); err != nil {
		t.Fatalf("rename %s.%s: %v", table, from, err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, fmt.Sprintf("alter table %s rename column %s to %s", table, to, from)); err != nil {
			t.Fatalf("restore %s.%s: %v", table, to, err)
		}
	})
}

// simpleProtocolRouter builds a router whose handler uses a simple-protocol
// pool: parse-time SQL errors surface synchronously from Query, letting us
// exercise the streaming-query failure branches deterministically.
func simpleProtocolRouter(t *testing.T) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	cfg, err := pgxpool.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	handlerPool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open simple protocol pool: %v", err)
	}
	t.Cleanup(handlerPool.Close)

	h := NewHandler(handlerPool)
	r := chi.NewRouter()
	r.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
	r.Get("/api/v1/projects", h.ListProjects)
	r.Post("/api/v1/projects", h.CreateProject)
	r.Get("/api/v1/projects/{id}", h.GetProject)
	r.Put("/api/v1/projects/{id}", h.UpdateProject)
	r.Delete("/api/v1/projects/{id}", h.DeleteProject)
	return r
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	h := NewHandler(testdb.Get(t))
	for _, tt := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/v1/projects"},
		{name: "create", method: http.MethodPost, path: "/api/v1/projects", body: `{"name":"x"}`},
		{name: "get", method: http.MethodGet, path: "/api/v1/projects/00000000-0000-0000-0000-000000000000"},
		{name: "update", method: http.MethodPut, path: "/api/v1/projects/00000000-0000-0000-0000-000000000000", body: `{"name":"x"}`},
		{name: "delete", method: http.MethodDelete, path: "/api/v1/projects/00000000-0000-0000-0000-000000000000"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := callHandler(h, tt.method, tt.path, tt.body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d body=%s, want 401", rec.Code, rec.Body.String())
			}
		})
	}
	// Through the middleware, a missing JWT is rejected before handlers run.
	router := newProjectsRouter(t)
	rec := doJSON(router, http.MethodGet, "/api/v1/projects", http.Header{}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("middleware status = %d, want 401", rec.Code)
	}
}
