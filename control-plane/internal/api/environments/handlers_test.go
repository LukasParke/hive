package environments

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newEnvironmentsRouter wires a real chi router with the same auth middleware
// as production so RBAC and 401 paths are exercised end to end.
func newEnvironmentsRouter(t *testing.T) (http.Handler, *Handler) {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/environments", h.ListEnvironments)
		gr.Post("/api/v1/environments", h.CreateEnvironment)
		gr.Get("/api/v1/environments/{id}", h.GetEnvironment)
		gr.Put("/api/v1/environments/{id}", h.UpdateEnvironment)
		gr.Delete("/api/v1/environments/{id}", h.DeleteEnvironment)
	})
	return r, h
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

func seedEnvironment(t *testing.T, projectID, name, slug string) string {
	t.Helper()
	var id string
	if err := testdb.Get(t).QueryRow(context.Background(), `
		insert into environments(project_id, name, slug) values ($1::uuid, $2, $3) returning id::text
	`, projectID, name, slug).Scan(&id); err != nil {
		t.Fatalf("seed environment: %v", err)
	}
	return id
}

func TestCreateEnvironmentHappyPath(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/environments", org.Headers,
		`{"projectId":"`+org.ProjectID+`","name":"Production","slug":"prod"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("decode id: %v %s", err, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from environments where id=$1::uuid and slug='prod'`, resp.ID); n != 1 {
		t.Fatalf("environment rows = %d, want 1", n)
	}
}

func TestCreateEnvironmentValidation(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrg(t)

	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{"projectId":`},
		{"missing projectId", `{"name":"a","slug":"a"}`},
		{"missing name", `{"projectId":"PROJECT","slug":"a"}`},
		{"missing slug", `{"projectId":"PROJECT","name":"a"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.ReplaceAll(tc.body, "PROJECT", org.ProjectID)
			rec := doJSON(t, router, http.MethodPost, "/api/v1/environments", org.Headers, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateEnvironmentForeignProjectForbidden(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/environments", orgB.Headers,
		`{"projectId":"`+orgA.ProjectID+`","name":"evil","slug":"evil"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
	}
}

func TestCreateEnvironmentUnknownProjectForbidden(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/environments", org.Headers,
		`{"projectId":"00000000-0000-0000-0000-000000000000","name":"x","slug":"x"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestCreateEnvironmentDuplicateSlugRejected(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrg(t)
	seedEnvironment(t, org.ProjectID, "prod", "prod")

	rec := doJSON(t, router, http.MethodPost, "/api/v1/environments", org.Headers,
		`{"projectId":"`+org.ProjectID+`","name":"other","slug":"prod"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400 on unique violation", rec.Code, rec.Body.String())
	}
}

func TestCreateEnvironmentMemberRoleForbidden(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrgWithRole(t, rbac.RoleOwner)
	member := org.AddMember(t, rbac.RoleMember)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/environments", member.Headers,
		`{"projectId":"`+org.ProjectID+`","name":"env","slug":"env"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for member role", rec.Code)
	}
}

func TestListEnvironmentsEmptyAndPopulated(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/environments", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":null`) {
		t.Fatalf("empty list status = %d body=%s", rec.Code, rec.Body.String())
	}

	first := seedEnvironment(t, org.ProjectID, "staging", "staging")
	second := seedEnvironment(t, org.ProjectID, "prod", "prod")

	rec = doJSON(t, router, http.MethodGet, "/api/v1/environments", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID        string `json:"id"`
			ProjectID string `json:"projectId"`
			Name      string `json:"name"`
			Slug      string `json:"slug"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2 (%s)", len(resp.Items), rec.Body.String())
	}
	// Ordered by created_at desc: newest first.
	if resp.Items[0].ID != second || resp.Items[1].ID != first {
		t.Fatalf("order = [%s %s], want [%s %s]", resp.Items[0].ID, resp.Items[1].ID, second, first)
	}
	if resp.Items[0].ProjectID != org.ProjectID || resp.Items[0].Slug != "prod" {
		t.Fatalf("item = %+v", resp.Items[0])
	}
}

func TestListEnvironmentsIsolatesOrganizations(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)
	seedEnvironment(t, orgA.ProjectID, "only-a", "only-a")

	otherRec := doJSON(t, router, http.MethodGet, "/api/v1/environments", orgB.Headers, "")
	if otherRec.Code != http.StatusOK || !strings.Contains(otherRec.Body.String(), `"items":null`) {
		t.Fatalf("foreign org sees rows: %d %s", otherRec.Code, otherRec.Body.String())
	}
}

func TestGetEnvironmentFoundAndMissing(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrg(t)
	id := seedEnvironment(t, org.ProjectID, "qa", "qa")

	rec := doJSON(t, router, http.MethodGet, "/api/v1/environments/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"slug":"qa"`) || !strings.Contains(rec.Body.String(), `"id":"`+id+`"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/api/v1/environments/00000000-0000-0000-0000-000000000000", org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id status = %d, want 404", rec.Code)
	}

	other := testdb.SeedOrg(t)
	rec = doJSON(t, router, http.MethodGet, "/api/v1/environments/"+id, other.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org status = %d, want 404", rec.Code)
	}
}

func TestUpdateEnvironmentMergesFields(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrgWithRole(t, rbac.RoleOwner)
	admin := org.AddMember(t, rbac.RoleAdmin)
	id := seedEnvironment(t, org.ProjectID, "old-name", "old-slug")

	rec := doJSON(t, router, http.MethodPut, "/api/v1/environments/"+id, admin.Headers,
		`{"name":"new-name"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var name, slug string
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select name, slug from environments where id=$1::uuid`, id).Scan(&name, &slug); err != nil {
		t.Fatalf("row missing: %v", err)
	}
	if name != "new-name" || slug != "old-slug" {
		t.Fatalf("name=%q slug=%q, want merged values", name, slug)
	}
}

func TestUpdateEnvironmentValidationAndMissing(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrg(t)
	id := seedEnvironment(t, org.ProjectID, "keep", "keep")

	rec := doJSON(t, router, http.MethodPut, "/api/v1/environments/"+id, org.Headers, `{broken`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d, want 400", rec.Code)
	}

	rec = doJSON(t, router, http.MethodPut, "/api/v1/environments/00000000-0000-0000-0000-000000000000",
		org.Headers, `{"name":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id status = %d, want 404", rec.Code)
	}
}

func TestUpdateEnvironmentMemberForbidden(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrgWithRole(t, rbac.RoleOwner)
	member := org.AddMember(t, rbac.RoleMember)
	id := seedEnvironment(t, org.ProjectID, "locked", "locked")

	rec := doJSON(t, router, http.MethodPut, "/api/v1/environments/"+id, member.Headers,
		`{"name":"nope"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestDeleteEnvironmentRemovesRow(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrg(t)
	id := seedEnvironment(t, org.ProjectID, "gone", "gone")

	rec := doJSON(t, router, http.MethodDelete, "/api/v1/environments/"+id, org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"deleted"`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from environments where id=$1::uuid`, id); n != 0 {
		t.Fatal("environment row not deleted")
	}

	rec = doJSON(t, router, http.MethodDelete, "/api/v1/environments/"+id, org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("repeat delete status = %d, want 404", rec.Code)
	}
}

func TestDeleteEnvironmentMemberForbiddenAndCrossOrg(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrgWithRole(t, rbac.RoleOwner)
	member := org.AddMember(t, rbac.RoleMember)
	id := seedEnvironment(t, org.ProjectID, "safe", "safe")

	rec := doJSON(t, router, http.MethodDelete, "/api/v1/environments/"+id, member.Headers, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member delete status = %d, want 403", rec.Code)
	}

	other := testdb.SeedOrg(t)
	rec = doJSON(t, router, http.MethodDelete, "/api/v1/environments/"+id, other.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org delete status = %d, want 404", rec.Code)
	}
}

func TestEndpointsRequireAuthentication(t *testing.T) {
	router, _ := newEnvironmentsRouter(t)
	org := testdb.SeedOrg(t)
	id := seedEnvironment(t, org.ProjectID, "auth", "auth")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/environments"},
		{http.MethodPost, "/api/v1/environments"},
		{http.MethodGet, "/api/v1/environments/" + id},
		{http.MethodPut, "/api/v1/environments/" + id},
		{http.MethodDelete, "/api/v1/environments/" + id},
	} {
		rec := doJSON(t, router, tc.method, tc.path, http.Header{}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s unauthenticated status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestListEnvironmentsQueryFailureReturns500(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)

	// Dropping a selected column makes every fresh preparation of the list
	// query fail; a brand-new pool guarantees nothing is cached.
	if _, err := pool.Exec(context.Background(), `alter table environments drop column slug`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	router, _ := newEnvRouterNoTruncate(t, freshEnvPool(t))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/environments", org.Headers, "")
	if _, err := pool.Exec(context.Background(), `alter table environments add column slug text not null default ''`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestListEnvironmentsScanFailureReturns500(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	seedEnvironment(t, org.ProjectID, "scanbreak", "scanbreak")

	// Make created_at unscannable as time.Time while the query still succeeds.
	if _, err := pool.Exec(context.Background(),
		`alter table environments alter column created_at drop default,
		 alter column created_at type text[] using array[to_char(created_at, 'YYYYMMDD')]`); err != nil {
		t.Fatalf("alter: %v", err)
	}
	router, _ := newEnvRouterNoTruncate(t, freshEnvPool(t))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/environments", org.Headers, "")
	if _, err := pool.Exec(context.Background(),
		`alter table environments alter column created_at type timestamptz using now(),
		 alter column created_at set default now()`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestUpdateAndDeleteEnvironmentExecFailures(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	router, _ := newEnvRouterNoTruncate(t, pool)
	id := seedEnvironment(t, org.ProjectID, "breakme", "breakme")

	// Dropping the whole table makes every statement against it fail.
	if _, err := pool.Exec(context.Background(), `alter table environments rename to environments_gone`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	putRec := doJSON(t, router, http.MethodPut, "/api/v1/environments/"+id, org.Headers, `{"name":"x"}`)
	delRec := doJSON(t, router, http.MethodDelete, "/api/v1/environments/"+id, org.Headers, "")
	if _, err := pool.Exec(context.Background(), `alter table environments_gone rename to environments`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if putRec.Code != http.StatusBadRequest {
		t.Fatalf("update exec failure status = %d body=%s, want 400", putRec.Code, putRec.Body.String())
	}
	if delRec.Code != http.StatusBadRequest {
		t.Fatalf("delete exec failure status = %d body=%s, want 400", delRec.Code, delRec.Body.String())
	}
}

// freshEnvPool opens an extra live pool with an empty prepared-statement cache.
func freshEnvPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(testdb.Get(t).Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatalf("parse conn string: %v", err)
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// newEnvRouterNoTruncate wires the router without resetting tables so DDL-based
// failure injection keeps its seeded rows.
func newEnvRouterNoTruncate(t *testing.T, pool *pgxpool.Pool) (http.Handler, *Handler) {
	t.Helper()
	h := NewHandler(pool)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/environments", h.ListEnvironments)
		gr.Put("/api/v1/environments/{id}", h.UpdateEnvironment)
		gr.Delete("/api/v1/environments/{id}", h.DeleteEnvironment)
	})
	return r, h
}
