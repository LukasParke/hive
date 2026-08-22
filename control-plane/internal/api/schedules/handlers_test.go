package schedules

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/go-chi/chi/v5"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newRouter wires the real auth middleware around every schedule endpoint so
// JWTs and RBAC are exercised end to end against the shared Postgres pool.
func newRouter(t *testing.T) http.Handler {
	t.Helper()
	return newRouterWithPool(t, testdb.Get(t))
}

func newRouterWithPool(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()
	testdb.TruncateAll(t)
	h := NewHandler(pool, testdb.RiverClient(t))
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/schedules", h.ListSchedules)
		gr.Post("/api/v1/schedules", h.CreateSchedule)
		gr.Put("/api/v1/schedules/{id}", h.UpdateSchedule)
		gr.Delete("/api/v1/schedules/{id}", h.DeleteSchedule)
		gr.Post("/api/v1/schedules/{id}/run", h.RunScheduleNow)
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

func seedSchedule(t *testing.T, router http.Handler, headers http.Header, body string) string {
	t.Helper()
	rec := doJSON(router, http.MethodPost, "/api/v1/schedules", headers, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed schedule status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("seed schedule response = %s err=%v", rec.Body.String(), err)
	}
	return resp.ID
}

func validScheduleBody() string {
	return `{"name":"nightly","cronExpr":"0 2 * * *","targetType":"backup","targetId":"vol-1"}`
}

func TestCreateSchedule(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)

	id := seedSchedule(t, router, org.Headers, validScheduleBody())
	var name, cron, targetType, targetID string
	var enabled bool
	if err := testdb.Get(t).QueryRow(t.Context(), `
		select name, cron_expr, target_type, target_id, enabled from schedules where id=$1::uuid
	`, id).Scan(&name, &cron, &targetType, &targetID, &enabled); err != nil {
		t.Fatalf("reload schedule: %v", err)
	}
	if name != "nightly" || cron != "0 2 * * *" || targetType != "backup" || targetID != "vol-1" || !enabled {
		t.Fatalf("schedule = (%q,%q,%q,%q,%v)", name, cron, targetType, targetID, enabled)
	}
}

func TestCreateScheduleDisabled(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	body := strings.Replace(validScheduleBody(), `"cronExpr"`, `"enabled":false,"cronExpr"`, 1)
	id := seedSchedule(t, router, org.Headers, body)
	if n := testdb.QueryCount(t, `select count(*) from schedules where id=$1::uuid and not enabled`, id); n != 1 {
		t.Fatalf("disabled schedule rows = %d, want 1", n)
	}
}

func TestCreateScheduleValidation(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	cases := []struct {
		name string
		body string
	}{
		{"missing name", `{"cronExpr":"* * * * *","targetType":"backup","targetId":"v"}`},
		{"missing cron", `{"name":"n","targetType":"backup","targetId":"v"}`},
		{"missing target type", `{"name":"n","cronExpr":"* * * * *","targetId":"v"}`},
		{"missing target id", `{"name":"n","cronExpr":"* * * * *","targetType":"backup"}`},
		{"malformed json", `{"name":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, http.MethodPost, "/api/v1/schedules", org.Headers, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListSchedules(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	first := seedSchedule(t, router, org.Headers, validScheduleBody())
	second := seedSchedule(t, router, org.Headers,
		`{"name":"hourly","cronExpr":"0 * * * *","targetType":"backup","targetId":"vol-2"}`)

	rec := doJSON(router, http.MethodGet, "/api/v1/schedules", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID         string     `json:"id"`
			Name       string     `json:"name"`
			CronExpr   string     `json:"cronExpr"`
			Enabled    bool       `json:"enabled"`
			LastRunAt  *time.Time `json:"lastRunAt"`
			TargetType string     `json:"targetType"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(resp.Items))
	}
	if resp.Items[0].ID != second || resp.Items[1].ID != first {
		t.Fatalf("order = [%s %s], want newest first", resp.Items[0].ID, resp.Items[1].ID)
	}
	if resp.Items[0].Name != "hourly" || resp.Items[0].CronExpr != "0 * * * *" || !resp.Items[0].Enabled || resp.Items[0].LastRunAt != nil {
		t.Fatalf("item = %+v", resp.Items[0])
	}
}

func TestUpdateSchedule(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedSchedule(t, router, org.Headers, validScheduleBody())

	rec := doJSON(router, http.MethodPut, "/api/v1/schedules/"+id, org.Headers,
		`{"name":"renamed","cronExpr":"30 3 * * *","enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var name, cron, targetType, targetID string
	var enabled bool
	if err := testdb.Get(t).QueryRow(t.Context(), `
		select name, cron_expr, target_type, target_id, enabled from schedules where id=$1::uuid
	`, id).Scan(&name, &cron, &targetType, &targetID, &enabled); err != nil {
		t.Fatalf("reload schedule: %v", err)
	}
	if name != "renamed" || cron != "30 3 * * *" || enabled || targetType != "backup" || targetID != "vol-1" {
		t.Fatalf("schedule = (%q,%q,%q,%q,%v)", name, cron, targetType, targetID, enabled)
	}

	// Blank fields keep stored values.
	rec = doJSON(router, http.MethodPut, "/api/v1/schedules/"+id, org.Headers, `{"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from schedules where id=$1::uuid and enabled and name='renamed'`, id); n != 1 {
		t.Fatalf("partial update rows = %d, want 1", n)
	}
}

func TestUpdateScheduleErrors(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"not found", "/api/v1/schedules/11111111-1111-1111-1111-111111111111", `{"name":"x"}`, http.StatusNotFound},
		{"malformed json", "/api/v1/schedules/11111111-1111-1111-1111-111111111111", `{`, http.StatusBadRequest},
		{"invalid uuid", "/api/v1/schedules/nope", `{"name":"x"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, http.MethodPut, tc.path, org.Headers, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}

func TestDeleteSchedule(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedSchedule(t, router, org.Headers, validScheduleBody())

	rec := doJSON(router, http.MethodDelete, "/api/v1/schedules/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from schedules where id=$1::uuid`, id); n != 0 {
		t.Fatalf("schedule rows after delete = %d, want 0", n)
	}

	rec = doJSON(router, http.MethodDelete, "/api/v1/schedules/11111111-1111-1111-1111-111111111111", org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing delete status = %d, want 404", rec.Code)
	}
}

func TestRunScheduleNowEnqueuesBackup(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedSchedule(t, router, org.Headers, validScheduleBody())

	rec := doJSON(router, http.MethodPost, "/api/v1/schedules/"+id+"/run", org.Headers, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status   string `json:"status"`
		BackupID string `json:"backupId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.BackupID == "" {
		t.Fatalf("response = %s err=%v", rec.Body.String(), err)
	}
	if resp.Status != "accepted" {
		t.Fatalf("status field = %q", resp.Status)
	}
	// A queued backup run row was created for the schedule target.
	if n := testdb.QueryCount(t, `select count(*) from backup_runs where id=$1::uuid and target_type='database' and target_id='vol-1' and status='queued'`, resp.BackupID); n != 1 {
		t.Fatalf("backup_runs rows = %d, want 1", n)
	}
	// The river job was enqueued.
	if n := testdb.QueryCount(t, `select count(*) from river_job where state='available' and kind='backup'`); n != 1 {
		t.Fatalf("river_job backup rows = %d, want 1", n)
	}
	// last_run_at was stamped.
	if n := testdb.QueryCount(t, `select count(*) from schedules where id=$1::uuid and last_run_at is not null`, id); n != 1 {
		t.Fatalf("last_run_at rows = %d, want 1", n)
	}
}

func TestRunScheduleNowWithoutRiverClient(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool, nil)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Post("/api/v1/schedules/{id}/run", h.RunScheduleNow)
	})
	org := testdb.SeedOrg(t)
	var id string
	if err := pool.QueryRow(t.Context(), `
		insert into schedules(name, cron_expr, target_type, target_id)
		values ('manual', '* * * * *', 'backup', 'vol-x') returning id::text
	`).Scan(&id); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	rec := doJSON(r, http.MethodPost, "/api/v1/schedules/"+id+"/run", org.Headers, "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from river_job`); n != 0 {
		t.Fatalf("river_job rows = %d, want 0 without river client", n)
	}
	if n := testdb.QueryCount(t, `select count(*) from backup_runs where status='queued'`); n != 1 {
		t.Fatalf("backup_runs rows = %d, want 1", n)
	}
}

func TestRunScheduleNowErrors(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	unsupported := seedSchedule(t, router, org.Headers,
		`{"name":"deploy","cronExpr":"* * * * *","targetType":"deploy","targetId":"app-1"}`)

	cases := []struct {
		name string
		path string
		want int
	}{
		{"unsupported target type", "/api/v1/schedules/" + unsupported + "/run", http.StatusBadRequest},
		{"not found", "/api/v1/schedules/11111111-1111-1111-1111-111111111111/run", http.StatusNotFound},
		{"invalid uuid", "/api/v1/schedules/nope/run", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, http.MethodPost, tc.path, org.Headers, "")
			if rec.Code != tc.want {
				t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}

func TestScheduleAdminOnly(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	member := org.AddMember(t, rbac.RoleMember)
	id := seedSchedule(t, router, org.Headers, validScheduleBody())

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create", http.MethodPost, "/api/v1/schedules", validScheduleBody()},
		{"update", http.MethodPut, "/api/v1/schedules/" + id, `{"name":"hax"}`},
		{"delete", http.MethodDelete, "/api/v1/schedules/" + id, ""},
		{"run now", http.MethodPost, "/api/v1/schedules/" + id + "/run", ""},
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
// handler's own conversion fails.
func bracedOrgHeader(headers http.Header) http.Header {
	h := headers.Clone()
	h.Set("X-Organization-Id", "{"+h.Get("X-Organization-Id")+"}")
	return h
}

func TestScheduleEndpointsRejectUnauthenticated(t *testing.T) {
	router := newRouter(t)
	testdb.SeedOrg(t)
	id := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/v1/schedules"},
		{"create", http.MethodPost, "/api/v1/schedules"},
		{"update", http.MethodPut, "/api/v1/schedules/" + id},
		{"delete", http.MethodDelete, "/api/v1/schedules/" + id},
		{"run now", http.MethodPost, "/api/v1/schedules/" + id + "/run"},
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

func TestListSchedulesQueryFailureReturns500(t *testing.T) {
	router := newRouterWithPool(t, simpleProtoPool(t))
	org := testdb.SeedOrg(t)
	renameColumn(t, "schedules", "cron_expr", "cron_expr_gone")
	rec := doJSON(router, http.MethodGet, "/api/v1/schedules", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestScheduleStatementFailures(t *testing.T) {
	t.Run("create insert failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		renameColumn(t, "schedules", "cron_expr", "cron_expr_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/schedules", org.Headers, validScheduleBody())
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := seedSchedule(t, router, org.Headers, validScheduleBody())
		renameTable(t, "schedules", "schedules_gone")
		rec := doJSON(router, http.MethodDelete, "/api/v1/schedules/"+id, org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("run now backup insert failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := seedSchedule(t, router, org.Headers, validScheduleBody())
		renameColumn(t, "backup_runs", "status", "status_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/schedules/"+id+"/run", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func TestRunScheduleNowEnqueueFailureReturns500(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	closed, err := pgxpool.New(t.Context(), pool.Config().ConnString())
	if err != nil {
		t.Fatalf("open secondary pool: %v", err)
	}
	closed.Close()
	client, err := river.NewClient(riverpgxv5.New(closed), &river.Config{})
	if err != nil {
		t.Fatalf("build river client: %v", err)
	}

	h := NewHandler(pool, client)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Post("/api/v1/schedules/{id}/run", h.RunScheduleNow)
	})
	org := testdb.SeedOrg(t)
	var id string
	if err := pool.QueryRow(t.Context(), `
		insert into schedules(name, cron_expr, target_type, target_id)
		values ('manual', '* * * * *', 'backup', 'vol-x') returning id::text
	`).Scan(&id); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	rec := doJSON(r, http.MethodPost, "/api/v1/schedules/"+id+"/run", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to enqueue backup") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestRunScheduleNowBracedOrgHeaderAccepted(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedSchedule(t, router, org.Headers, validScheduleBody())
	rec := doJSON(router, http.MethodPost, "/api/v1/schedules/"+id+"/run", bracedOrgHeader(org.Headers), "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s, want 202", rec.Code, rec.Body.String())
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
