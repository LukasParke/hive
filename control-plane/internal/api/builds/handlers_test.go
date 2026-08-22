package builds

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	pgx "github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newBuildsRouter wires a real chi router with the same auth middleware used
// in production so JWTs and org headers are exercised end-to-end.
func newBuildsRouter(t *testing.T) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool, testdb.RiverClient(t))
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/builds", h.ListBuilds)
		gr.Get("/api/v1/builds/queue", h.ListBuildQueue)
		gr.Get("/api/v1/builds/{id}/logs", h.GetBuildLogs)
		gr.Post("/api/v1/builds/{id}/cancel", h.CancelBuild)
		gr.Post("/api/v1/builds/{id}/retry", h.RetryBuild)
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

	h := NewHandler(handlerPool, testdb.RiverClient(t))
	r := chi.NewRouter()
	r.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
	r.Get("/api/v1/builds", h.ListBuilds)
	r.Get("/api/v1/builds/queue", h.ListBuildQueue)
	r.Get("/api/v1/builds/{id}/logs", h.GetBuildLogs)
	return r
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

// blockWrites installs a row trigger failing every write of the given event,
// exercising exec-error branches without disturbing reads.
func blockWrites(t *testing.T, table, event string) {
	t.Helper()
	p := testdb.Get(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `
		create or replace function test_block_write_fn() returns trigger as $$
		begin raise exception 'blocked by test'; end
		$$ language plpgsql
	`); err != nil {
		t.Fatalf("create function: %v", err)
	}
	if _, err := p.Exec(ctx, fmt.Sprintf(
		"create trigger test_block_write_trg before %s on %s for each row execute function test_block_write_fn()",
		event, table)); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, fmt.Sprintf("drop trigger if exists test_block_write_trg on %s", table)); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
		if _, err := p.Exec(ctx, `drop function if exists test_block_write_fn()`); err != nil {
			t.Fatalf("drop function: %v", err)
		}
	})
}

// seedBuild inserts one build_jobs row and returns its id. An empty imageTag
// seeds a NULL, which exercises scan-error tolerance in list endpoints.
func seedBuild(t *testing.T, appID, status, imageTag string) string {
	t.Helper()
	p := testdb.Get(t)
	var id string
	err := p.QueryRow(context.Background(), `
		insert into build_jobs(application_id, status, trigger, image_tag, logs)
		values ($1::uuid, $2, 'api', nullif($3, ''), $4)
		returning id::text
	`, appID, status, imageTag, "build log for "+status).Scan(&id)
	if err != nil {
		t.Fatalf("seed build(%s): %v", status, err)
	}
	return id
}

// seedApp creates a uniquely named application in the org.
func seedApp(t *testing.T, org *testdb.OrgFixture) string {
	t.Helper()
	return testdb.SeedApplication(t, org.ProjectID, "", "", nil)
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return resp
}

// withChiURLParams attaches a chi route context so direct handler invocations
// can supply URL parameters.
func withChiURLParams(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestListBuilds(t *testing.T) {
	router := newBuildsRouter(t)
	org := testdb.SeedOrg(t)
	appA := seedApp(t, org)
	seedBuild(t, appA, "failed", "")
	complete := seedBuild(t, appA, "complete", "img:1")
	otherOrg := testdb.SeedOrg(t)
	seedBuild(t, seedApp(t, otherOrg), "complete", "img:x")

	t.Run("lists org builds newest first without null image tags", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/builds", org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		items, ok := decodeBody(t, rec)["items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("items = %v, want only the build with an image tag (null-image row omitted)", items)
		}
		item := items[0].(map[string]any)
		if item["id"] != complete || item["status"] != "complete" || item["trigger"] != "api" ||
			item["applicationId"] != appA || item["imageTag"] != "img:1" {
			t.Fatalf("item = %v, want seeded complete build", item)
		}
	})

	t.Run("member allowed", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		if rec := doJSON(router, http.MethodGet, "/api/v1/builds", member.Headers, ""); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("outsider forbidden", func(t *testing.T) {
		intruder := http.Header{}
		intruder.Set("Authorization", "Bearer "+otherOrg.Token)
		intruder.Set("X-Organization-Id", org.OrgID)
		if rec := doJSON(router, http.MethodGet, "/api/v1/builds", intruder, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("query failure returns 500", func(t *testing.T) {
		simple := simpleProtocolRouter(t)
		o := testdb.SeedOrg(t)
		renameColumn(t, "build_jobs", "trigger", "trigger_gone")
		rec := doJSON(simple, http.MethodGet, "/api/v1/builds", o.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})

	t.Run("unauthenticated direct call", func(t *testing.T) {
		h := NewHandler(testdb.Get(t), testdb.RiverClient(t))
		rec := httptest.NewRecorder()
		h.ListBuilds(rec, httptest.NewRequest(http.MethodGet, "/api/v1/builds", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}

func TestListBuildQueue(t *testing.T) {
	router := newBuildsRouter(t)
	org := testdb.SeedOrg(t)
	app := seedApp(t, org)
	app2 := seedApp(t, org)
	queued := seedBuild(t, app, "queued", "img:q")
	building := seedBuild(t, app2, "building", "img:b")
	seedBuild(t, app, "complete", "img:c") // terminal: must not appear
	seedBuild(t, app2, "cancelled", "")    // terminal + null tag
	app3 := seedApp(t, org)
	nullTag := seedBuild(t, app3, "queued", "") // active row with NULL image_tag fails Scan and is skipped

	rec := doJSON(router, http.MethodGet, "/api/v1/builds/queue", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	items, ok := decodeBody(t, rec)["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %v, want queued+building only (null-tag row skipped)", items)
	}
	for _, it := range items {
		if m := it.(map[string]any); m["id"] == nullTag {
			t.Fatalf("unscannable null-tag row must be skipped, got %v", items)
		}
	}
	first := items[0].(map[string]any)
	second := items[1].(map[string]any)
	if first["id"] != queued || second["id"] != building {
		t.Fatalf("order = %v then %v, want oldest (queued) first", first["id"], second["id"])
	}
	if first["retries"].(float64) != 0 {
		t.Fatalf("retries = %v, want 0", first["retries"])
	}

	member := org.AddMember(t, rbac.RoleMember)
	if rec := doJSON(router, http.MethodGet, "/api/v1/builds/queue", member.Headers, ""); rec.Code != http.StatusOK {
		t.Fatalf("member status = %d, want 200", rec.Code)
	}

	orgB := testdb.SeedOrg(t)
	intruder := http.Header{}
	intruder.Set("Authorization", "Bearer "+orgB.Token)
	intruder.Set("X-Organization-Id", org.OrgID)
	if rec := doJSON(router, http.MethodGet, "/api/v1/builds/queue", intruder, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider status = %d, want 403", rec.Code)
	}

	h := NewHandler(testdb.Get(t), testdb.RiverClient(t))
	rec = httptest.NewRecorder()
	h.ListBuildQueue(rec, httptest.NewRequest(http.MethodGet, "/api/v1/builds/queue", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rec.Code)
	}

	simple := simpleProtocolRouter(t)
	o := testdb.SeedOrg(t)
	renameColumn(t, "build_jobs", "trigger", "trigger_gone")
	if rec := doJSON(simple, http.MethodGet, "/api/v1/builds/queue", o.Headers, ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("query failure status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestGetBuildLogs(t *testing.T) {
	router := newBuildsRouter(t)
	org := testdb.SeedOrg(t)
	app := testdb.SeedApplication(t, org.ProjectID, "app", "", nil)
	buildID := seedBuild(t, app, "complete", "img:1")

	t.Run("returns log text", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/builds/"+buildID+"/logs", org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Fatalf("content-type = %q, want text/plain; charset=utf-8", ct)
		}
		if got := rec.Body.String(); got != "build log for complete" {
			t.Fatalf("body = %q", got)
		}
	})

	t.Run("unknown build not found", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/builds/00000000-0000-0000-0000-000000000000/logs", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("cross-org build not found", func(t *testing.T) {
		orgB := testdb.SeedOrg(t)
		foreign := seedBuild(t, seedApp(t, orgB), "complete", "img:2")
		rec := doJSON(router, http.MethodGet, "/api/v1/builds/"+foreign+"/logs", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("invalid uuid rejected", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/builds/not-a-uuid/logs", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("member allowed and outsider forbidden", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		if rec := doJSON(router, http.MethodGet, "/api/v1/builds/"+buildID+"/logs", member.Headers, ""); rec.Code != http.StatusOK {
			t.Fatalf("member status = %d, want 200", rec.Code)
		}
		orgB := testdb.SeedOrg(t)
		intruder := http.Header{}
		intruder.Set("Authorization", "Bearer "+orgB.Token)
		intruder.Set("X-Organization-Id", org.OrgID)
		if rec := doJSON(router, http.MethodGet, "/api/v1/builds/"+buildID+"/logs", intruder, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("outsider status = %d, want 403", rec.Code)
		}
	})

	t.Run("unauthenticated direct call", func(t *testing.T) {
		h := NewHandler(testdb.Get(t), testdb.RiverClient(t))
		req := withChiURLParams(httptest.NewRequest(http.MethodGet, "/api/v1/builds/x/logs", nil), map[string]string{"id": "x"})
		rec := httptest.NewRecorder()
		h.GetBuildLogs(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("log query failure returns 500", func(t *testing.T) {
		simple := simpleProtocolRouter(t)
		o := testdb.SeedOrg(t)
		a := testdb.SeedApplication(t, o.ProjectID, "simple-app", "", nil)
		id := seedBuild(t, a, "complete", "img:s")
		renameColumn(t, "build_jobs", "logs", "logs_gone")
		rec := doJSON(simple, http.MethodGet, "/api/v1/builds/"+id+"/logs", o.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})
}

func TestCancelBuild(t *testing.T) {
	router := newBuildsRouter(t)
	org := testdb.SeedOrg(t)

	t.Run("cancels queued build", func(t *testing.T) {
		id := seedBuild(t, seedApp(t, org), "queued", "img:q")
		rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/cancel", org.Headers, "")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d body=%s, want 202", rec.Code, rec.Body.String())
		}
		if decodeBody(t, rec)["status"] != "canceled" {
			t.Fatal("response status must be canceled")
		}
		n := testdb.QueryCount(t, `select count(*) from build_jobs where id::text=$1 and status='cancelled'`, id)
		if n != 1 {
			t.Fatal("build_jobs row not cancelled")
		}
	})

	t.Run("cancels building build", func(t *testing.T) {
		id := seedBuild(t, seedApp(t, org), "building", "img:b")
		if rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/cancel", org.Headers, ""); rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d body=%s, want 202", rec.Code, rec.Body.String())
		}
	})

	t.Run("terminal builds are not cancelable", func(t *testing.T) {
		for _, status := range []string{"complete", "failed", "cancelled"} {
			id := seedBuild(t, seedApp(t, org), status, "img:t")
			rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/cancel", org.Headers, "")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s cancel status = %d body=%s, want 404", status, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("unknown build not found", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/builds/00000000-0000-0000-0000-000000000000/cancel", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("invalid uuid maps to bad request", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/builds/not-a-uuid/cancel", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("member forbidden", func(t *testing.T) {
		id := seedBuild(t, seedApp(t, org), "queued", "img:m")
		member := org.AddMember(t, rbac.RoleMember)
		if rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/cancel", member.Headers, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("outsider forbidden", func(t *testing.T) {
		id := seedBuild(t, seedApp(t, org), "queued", "img:o")
		orgB := testdb.SeedOrg(t)
		intruder := http.Header{}
		intruder.Set("Authorization", "Bearer "+orgB.Token)
		intruder.Set("X-Organization-Id", org.OrgID)
		if rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/cancel", intruder, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("exec failure maps to bad request", func(t *testing.T) {
		id := seedBuild(t, seedApp(t, org), "queued", "img:x")
		blockWrites(t, "build_jobs", "update")
		rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/cancel", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("unauthenticated direct call", func(t *testing.T) {
		h := NewHandler(testdb.Get(t), testdb.RiverClient(t))
		req := withChiURLParams(httptest.NewRequest(http.MethodPost, "/api/v1/builds/x/cancel", nil), map[string]string{"id": "x"})
		rec := httptest.NewRecorder()
		h.CancelBuild(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}

func TestRetryBuild(t *testing.T) {
	router := newBuildsRouter(t)
	org := testdb.SeedOrg(t)

	t.Run("requeues failed build and enqueues river job", func(t *testing.T) {
		testdb.Truncate(t, "river_job")
		id := seedBuild(t, seedApp(t, org), "failed", "img:f")
		rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/retry", org.Headers, "")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d body=%s, want 202", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		if resp["status"] != "accepted" || resp["buildId"] != id {
			t.Fatalf("response = %v, want accepted with same buildId", resp)
		}
		var retries int
		var status string
		if err := testdb.Get(t).QueryRow(context.Background(), `
			select retries, status::text from build_jobs where id::text=$1
		`, id).Scan(&retries, &status); err != nil || status != "queued" || retries != 0 {
			t.Fatalf("row retries=%d status=%q err=%v, want queued/0", retries, status, err)
		}
		if n := testdb.QueryCount(t, `select count(*) from river_job where state='available'`); n != 1 {
			t.Fatalf("river_job rows = %d, want 1", n)
		}
	})

	t.Run("conflict when another active build exists", func(t *testing.T) {
		app := seedApp(t, org)
		failed := seedBuild(t, app, "failed", "img:f2")
		seedBuild(t, app, "building", "img:blocker") // active per-app unique index blocks retry
		rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+failed+"/retry", org.Headers, "")
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
		}
		if code, _ := decodeBody(t, rec)["error"].(string); code != "build_in_progress" {
			t.Fatalf("error = %v, want build_in_progress", decodeBody(t, rec)["error"])
		}
	})

	t.Run("already active build not retryable", func(t *testing.T) {
		queued := seedBuild(t, seedApp(t, org), "queued", "img:a")
		rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+queued+"/retry", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown or cross-org build not found", func(t *testing.T) {
		orgB := testdb.SeedOrg(t)
		foreign := seedBuild(t, seedApp(t, orgB), "failed", "img:z")
		for _, id := range []string{"00000000-0000-0000-0000-000000000000", foreign} {
			rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/retry", org.Headers, "")
			if rec.Code != http.StatusNotFound {
				t.Fatalf("retry %s status = %d, want 404", id, rec.Code)
			}
		}
	})

	t.Run("invalid uuid rejected", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/builds/nope/retry", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("river enqueue failure returns 500", func(t *testing.T) {
		id := seedBuild(t, seedApp(t, org), "failed", "img:e")
		blockWrites(t, "river_job", "insert")
		rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/retry", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
		if code, _ := decodeBody(t, rec)["error"].(string); code != "enqueue_failed" {
			t.Fatalf("error = %v, want enqueue_failed", decodeBody(t, rec)["error"])
		}
	})

	t.Run("member forbidden and outsider forbidden", func(t *testing.T) {
		id := seedBuild(t, seedApp(t, org), "failed", "img:r")
		member := org.AddMember(t, rbac.RoleMember)
		if rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/retry", member.Headers, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("member status = %d, want 403", rec.Code)
		}
		orgB := testdb.SeedOrg(t)
		intruder := http.Header{}
		intruder.Set("Authorization", "Bearer "+orgB.Token)
		intruder.Set("X-Organization-Id", org.OrgID)
		if rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/retry", intruder, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("outsider status = %d, want 403", rec.Code)
		}
	})

	t.Run("unauthenticated direct call", func(t *testing.T) {
		h := NewHandler(testdb.Get(t), testdb.RiverClient(t))
		req := withChiURLParams(httptest.NewRequest(http.MethodPost, "/api/v1/builds/x/retry", nil), map[string]string{"id": "x"})
		rec := httptest.NewRecorder()
		h.RetryBuild(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})
}

func TestUuidOrNilMapsParseFailureToZeroUUID(t *testing.T) {
	if got := uuidOrNil("not-a-uuid"); got.Valid {
		t.Fatalf("uuidOrNil parse failure must yield zero UUID, got %+v", got)
	}
	id := uuidOrNil("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	if !id.Valid || id.String() != "6ba7b810-9dad-11d1-80b4-00c04fd430c8" {
		t.Fatalf("uuidOrNil valid path = %+v", id)
	}
}

func TestRetryBuildResetFailureMapsToBadRequest(t *testing.T) {
	router := newBuildsRouter(t)
	org := testdb.SeedOrg(t)
	id := seedBuild(t, seedApp(t, org), "failed", "img:resetfail")
	blockWrites(t, "build_jobs", "update")
	rec := doJSON(router, http.MethodPost, "/api/v1/builds/"+id+"/retry", org.Headers, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}
