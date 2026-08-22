package organizations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	pgx "github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newOrgRouter wires a real chi router with the same auth middleware used in
// production so JWTs and org headers are exercised end-to-end.
func newOrgRouter(t *testing.T) (http.Handler, *Handler) {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/organizations", h.ListOrganizations)
		gr.Post("/api/v1/organizations", h.CreateOrganization)
	})
	return r, h
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

// simpleProtocolRouter builds a router whose handler uses a simple-protocol
// pool: parse-time SQL errors surface synchronously from Query/Exec, letting
// us exercise statement failure branches deterministically.
func simpleProtocolRouter(t *testing.T) (http.Handler, *Handler) {
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
	r.Get("/api/v1/organizations", h.ListOrganizations)
	r.Post("/api/v1/organizations", h.CreateOrganization)
	return r, h
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

func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func TestListOrganizationsScopedToCaller(t *testing.T) {
	router, _ := newOrgRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)

	rec := doJSON(router, http.MethodGet, "/api/v1/organizations", orgA.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
			Role string `json:"role"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("user A sees %d orgs, want exactly 1: %v", len(got.Items), got.Items)
	}
	item := got.Items[0]
	if item.ID != orgA.OrgID {
		t.Fatalf("id = %q, want %q", item.ID, orgA.OrgID)
	}
	if item.Name == "" || item.Slug == "" || item.Role == "" {
		t.Fatalf("item missing fields: %+v", item)
	}

	rec = doJSON(router, http.MethodGet, "/api/v1/organizations", orgB.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	got.Items = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != orgB.OrgID {
		t.Fatalf("user B orgs = %+v, want only org B %s", got.Items, orgB.OrgID)
	}
}

func TestListOrganizationsQueryFailure(t *testing.T) {
	router, _ := simpleProtocolRouter(t)
	org := testdb.SeedOrg(t)

	renameColumn(t, "organizations", "slug", "slug_gone")
	rec := doJSON(router, http.MethodGet, "/api/v1/organizations", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestCreateOrganization(t *testing.T) {
	router, _ := newOrgRouter(t)
	org := testdb.SeedOrg(t)

	suffix := uniqueSuffix()
	body := fmt.Sprintf(`{"name":"Acme Corp","slug":"acme-%s"}`, suffix)
	rec := doJSON(router, http.MethodPost, "/api/v1/organizations", org.Headers, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] == "" || resp["name"] != "Acme Corp" || resp["slug"] != "acme-"+suffix {
		t.Fatalf("unexpected response: %v", resp)
	}

	p := testdb.Get(t)
	var name, slug string
	if err := p.QueryRow(context.Background(),
		`select name, slug from organizations where id::text = $1`, resp["id"],
	).Scan(&name, &slug); err != nil {
		t.Fatalf("created org row missing: %v", err)
	}
	if name != "Acme Corp" || slug != "acme-"+suffix {
		t.Fatalf("row name=%q slug=%q, want Acme Corp / acme-%s", name, slug, suffix)
	}
	var role string
	if err := p.QueryRow(context.Background(), `
		select role::text from organization_members
		where organization_id::text = $1 and user_id::text = $2
	`, resp["id"], org.UserID).Scan(&role); err != nil {
		t.Fatalf("creator membership missing: %v", err)
	}
	if role != "owner" {
		t.Fatalf("creator role = %q, want owner", role)
	}
}

func TestCreateOrganizationSlugConflicts(t *testing.T) {
	router, _ := newOrgRouter(t)
	orgA := testdb.SeedOrg(t)
	suffix := uniqueSuffix()
	slug := "conflict-" + suffix
	name := "Conflict Org " + suffix

	// Seed the pre-existing org via the API.
	rec := doJSON(router, http.MethodPost, "/api/v1/organizations", orgA.Headers,
		fmt.Sprintf(`{"name":%q,"slug":%q}`, name, slug))
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed create status = %d body=%s, want 201", rec.Code, rec.Body.String())
	}
	var seeded map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &seeded); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	t.Run("same user same slug returns existing org with 200", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations", orgA.Headers,
			fmt.Sprintf(`{"name":%q,"slug":%q}`, name, slug))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["id"] != seeded["id"] {
			t.Fatalf("id = %q, want existing %q", resp["id"], seeded["id"])
		}
	})

	t.Run("same user same name returns existing org with 200", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations", orgA.Headers,
			fmt.Sprintf(`{"name":%q,"slug":"other-%s"}`, name, uniqueSuffix()))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["id"] != seeded["id"] {
			t.Fatalf("id = %q, want existing %q", resp["id"], seeded["id"])
		}
	})

	t.Run("member user same slug returns existing org with 200", func(t *testing.T) {
		member := orgA.AddMember(t, rbac.RoleMember)
		// The UX-stabilization branch only applies when the caller is
		// already a member of the conflicting organization.
		if _, err := testdb.Get(t).Exec(context.Background(), `
			insert into organization_members(organization_id, user_id, role)
			values ($1::uuid, $2::uuid, 'member')
		`, seeded["id"], member.UserID); err != nil {
			t.Fatalf("add member to existing org: %v", err)
		}
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations", member.Headers,
			fmt.Sprintf(`{"name":"Whatever %s","slug":%q}`, uniqueSuffix(), slug))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["id"] != seeded["id"] {
			t.Fatalf("id = %q, want existing %q", resp["id"], seeded["id"])
		}
	})

	t.Run("non-member user same slug gets 400", func(t *testing.T) {
		orgB := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations", orgB.Headers,
			fmt.Sprintf(`{"name":%q,"slug":%q}`, name, slug))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func TestCreateOrganizationInvalidPayload(t *testing.T) {
	router, _ := newOrgRouter(t)
	org := testdb.SeedOrg(t)

	cases := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: "{not json"},
		{name: "missing name", body: `{"slug":"some-slug"}`},
		{name: "missing slug", body: `{"name":"Some Org"}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(router, http.MethodPost, "/api/v1/organizations", org.Headers, tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateOrganizationStatementFailures(t *testing.T) {
	router, _ := simpleProtocolRouter(t)
	org := testdb.SeedOrg(t)

	t.Run("org insert failure other than conflict maps to 400", func(t *testing.T) {
		renameColumn(t, "organizations", "name", "name_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations", org.Headers,
			fmt.Sprintf(`{"name":"Fresh Org","slug":"fresh-%s"}`, uniqueSuffix()))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("membership insert failure maps to 400", func(t *testing.T) {
		renameColumn(t, "organization_members", "role", "role_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations", org.Headers,
			fmt.Sprintf(`{"name":"Member Fail Org","slug":"member-fail-%s"}`, uniqueSuffix()))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func TestCreateOrganizationBeginFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	cfg, err := pgxpool.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = 2
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	handlerPool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open handler pool: %v", err)
	}
	handlerPool.Close() // closed pool makes tx.Begin fail

	h := NewHandler(handlerPool)
	r := chi.NewRouter()
	r.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
	r.Post("/api/v1/organizations", h.CreateOrganization)

	org := testdb.SeedOrg(t)
	rec := doJSON(r, http.MethodPost, "/api/v1/organizations", org.Headers,
		fmt.Sprintf(`{"name":"No Tx Org","slug":"no-tx-%s"}`, uniqueSuffix()))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestEndpointsRejectUnauthenticatedRequests(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool)

	// The auth middleware 401s before the handler runs, so the handlers'
	// own unauthenticated branches are exercised by invoking them directly
	// with a bare request (no middleware, no claims in context).
	t.Run("list organizations", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ListOrganizations(rec, httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("create organization", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.CreateOrganization(rec, httptest.NewRequest(http.MethodPost, "/api/v1/organizations", strings.NewReader(`{"name":"x","slug":"x"}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}

func TestListOrganizationsScanFailure(t *testing.T) {
	router, _ := newOrgRouter(t)
	org := testdb.SeedOrg(t)
	p := testdb.Get(t)
	ctx := context.Background()

	// name is scanned into a plain string; a NULL breaks the scan and must
	// surface as 500 rather than a panic.
	if _, err := p.Exec(ctx, `alter table organizations alter column name drop not null`); err != nil {
		t.Fatalf("drop not null: %v", err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, `delete from organizations where name is null`); err != nil {
			t.Fatalf("cleanup null orgs: %v", err)
		}
		if _, err := p.Exec(ctx, `alter table organizations alter column name set not null`); err != nil {
			t.Fatalf("restore not null: %v", err)
		}
	})
	suffix := uniqueSuffix()
	if _, err := p.Exec(ctx, `
		insert into organizations(name, slug) values (null, $1)
	`, "scanfail-"+suffix); err != nil {
		t.Fatalf("seed null-name org: %v", err)
	}
	if _, err := p.Exec(ctx, `
		insert into organization_members(organization_id, user_id, role)
		select id, $2::uuid, 'owner' from organizations where slug = $1
	`, "scanfail-"+suffix, org.UserID); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	rec := doJSON(router, http.MethodGet, "/api/v1/organizations", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestCreateOrganizationCommitFailure(t *testing.T) {
	router, _ := newOrgRouter(t)
	org := testdb.SeedOrg(t)
	p := testdb.Get(t)
	ctx := context.Background()

	// A deferred constraint trigger raises only at COMMIT time, exercising
	// the commit-failure branch deterministically.
	if _, err := p.Exec(ctx, `
		create or replace function test_block_commit() returns trigger as $$
		begin raise exception 'commit blocked by test'; end
		$$ language plpgsql
	`); err != nil {
		t.Fatalf("create function: %v", err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, `drop function if exists test_block_commit()`); err != nil {
			t.Fatalf("drop function: %v", err)
		}
	})
	if _, err := p.Exec(ctx, `
		create constraint trigger test_block_commit_trg
		after insert on organization_members
		deferrable initially deferred
		for each row execute function test_block_commit()
	`); err != nil {
		t.Fatalf("create constraint trigger: %v", err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, `drop trigger if exists test_block_commit_trg on organization_members`); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	})

	rec := doJSON(router, http.MethodPost, "/api/v1/organizations", org.Headers,
		fmt.Sprintf(`{"name":"Blocked Org","slug":"blocked-%s"}`, uniqueSuffix()))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}
