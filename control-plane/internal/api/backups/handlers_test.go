package backups

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newRouter wires the real auth middleware around every backup endpoint so
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
		gr.Get("/api/v1/backups", h.ListBackups)
		gr.Post("/api/v1/backups", h.CreateBackup)
		gr.Post("/api/v1/backups/{id}/restore", h.RestoreBackup)
		gr.Get("/api/v1/backup-destinations", h.ListBackupDestinations)
		gr.Post("/api/v1/backup-destinations", h.CreateBackupDestination)
		gr.Get("/api/v1/backup-destinations/{id}", h.GetBackupDestination)
		gr.Put("/api/v1/backup-destinations/{id}", h.UpdateBackupDestination)
		gr.Delete("/api/v1/backup-destinations/{id}", h.DeleteBackupDestination)
		gr.Post("/api/v1/backup-destinations/{id}/test", h.TestBackupDestination)
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

func mustCreateDestination(t *testing.T, router http.Handler, headers http.Header, name, typ, config string) string {
	t.Helper()
	rec := doJSON(router, http.MethodPost, "/api/v1/backup-destinations", headers,
		`{"name":"`+name+`","type":"`+typ+`","config":`+config+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed destination status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("seed destination response = %s err=%v", rec.Body.String(), err)
	}
	return resp.ID
}

func TestListBackupsEmpty(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)

	rec := doJSON(router, http.MethodGet, "/api/v1/backups", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"items"`) {
		t.Fatalf("missing items key: %s", rec.Body.String())
	}
}

func TestListBackupsOrgScoped(t *testing.T) {
	router := newRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)
	pool := testdb.Get(t)

	// Database-backed run belongs to org A through its project.
	dbID := ""
	if err := pool.QueryRow(t.Context(), `
		insert into database_services(project_id, engine, name, service_name, port)
		values ($1::uuid, 'postgres', 'db-a', 'db-a-svc', 5432) returning id::text
	`, orgA.ProjectID).Scan(&dbID); err != nil {
		t.Fatalf("seed database service: %v", err)
	}
	runDB := func(targetType, targetID string) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), `
			insert into backup_runs(target_type, target_id, status, artifact_path)
			values ($1, $2, 'complete', '/artifacts/' || $2 || '.tar')
		`, targetType, targetID); err != nil {
			t.Fatalf("seed backup run: %v", err)
		}
	}
	runDB("database", dbID)
	runDB("volume", "vol-shared")

	listTargets := func(headers http.Header) []string {
		t.Helper()
		rec := doJSON(router, http.MethodGet, "/api/v1/backups", headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []struct {
				ID         string `json:"id"`
				TargetType string `json:"targetType"`
				Status     string `json:"status"`
			} `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		var types []string
		for _, it := range resp.Items {
			types = append(types, it.TargetType)
			if it.Status != "complete" {
				t.Fatalf("status = %q, want complete", it.Status)
			}
		}
		return types
	}

	aTypes := listTargets(orgA.Headers)
	if len(aTypes) != 2 {
		t.Fatalf("org A items = %v, want database + volume", aTypes)
	}
	bTypes := listTargets(orgB.Headers)
	if len(bTypes) != 1 || bTypes[0] != "volume" {
		t.Fatalf("org B items = %v, want only volume run", bTypes)
	}
}

func TestListBackupsUnauthorized(t *testing.T) {
	router := newRouter(t)
	testdb.SeedOrg(t)
	rec := doJSON(router, http.MethodGet, "/api/v1/backups", http.Header{}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateBackup(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	destID := mustCreateDestination(t, router, org.Headers, "dest", "local", `{}`)

	rec := doJSON(router, http.MethodPost, "/api/v1/backups", org.Headers,
		`{"targetType":"volume","targetId":"vol-1","destinationId":"`+destID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("response = %s err=%v", rec.Body.String(), err)
	}
	if got := testdb.QueryCount(t, `select count(*) from backup_runs where id=$1::uuid and status='queued' and schedule='manual' and destination_id=$2::uuid`, resp.ID, destID); got != 1 {
		t.Fatalf("queued manual run rows = %d, want 1", got)
	}
}

func TestCreateBackupValidationTable(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	cases := []struct {
		name string
		body string
	}{
		{"missing target type", `{"targetId":"x"}`},
		{"missing target id", `{"targetType":"volume"}`},
		{"malformed json", `{"targetType":`},
		{"bad destination uuid", `{"targetType":"volume","targetId":"v","destinationId":"nope"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, http.MethodPost, "/api/v1/backups", org.Headers, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRestoreBackup(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	pool := testdb.Get(t)

	var runID string
	if err := pool.QueryRow(t.Context(), `
		insert into backup_runs(target_type, target_id, status) values ('volume', 'vol-1', 'complete')
		returning id::text
	`).Scan(&runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	rec := doJSON(router, http.MethodPost, "/api/v1/backups/"+runID+"/restore", org.Headers,
		`{"restoreTarget":"vol-restored"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var target, status string
	if err := pool.QueryRow(t.Context(),
		`select status, restore_target from backup_runs where id=$1::uuid`, runID,
	).Scan(&status, &target); err != nil {
		t.Fatalf("reload run: %v", err)
	}
	if status != "restore-queued" || target != "vol-restored" {
		t.Fatalf("run = (%q,%q), want (restore-queued, vol-restored)", status, target)
	}
}

func TestRestoreBackupValidation(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	cases := []struct {
		name string
		path string
		body string
	}{
		{"missing restore target", "/api/v1/backups/11111111-1111-1111-1111-111111111111/restore", `{}`},
		{"malformed json", "/api/v1/backups/11111111-1111-1111-1111-111111111111/restore", `{`},
		{"invalid uuid", "/api/v1/backups/not-a-uuid/restore", `{"restoreTarget":"x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, http.MethodPost, tc.path, org.Headers, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestBackupDestinationLifecycle(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := mustCreateDestination(t, router, org.Headers, "nas", "shared", `{"path":"/srv/backups"}`)

	// List shows the created destination with its config round-tripped.
	rec := doJSON(router, http.MethodGet, "/api/v1/backup-destinations", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []struct {
			ID     string          `json:"id"`
			Name   string          `json:"name"`
			Type   string          `json:"type"`
			Config json.RawMessage `json:"config"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != id || list.Items[0].Name != "nas" || list.Items[0].Type != "shared" {
		t.Fatalf("list items = %+v, want single shared destination", list.Items)
	}
	if !strings.Contains(string(list.Items[0].Config), "/srv/backups") {
		t.Fatalf("config = %s, want path preserved", list.Items[0].Config)
	}

	// Get by ID.
	rec = doJSON(router, http.MethodGet, "/api/v1/backup-destinations/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Update renames and replaces config.
	rec = doJSON(router, http.MethodPut, "/api/v1/backup-destinations/"+id, org.Headers,
		`{"name":"nas-2","type":"local","config":{"dir":"/tmp"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Name   string `json:"name"`
		Type   string `json:"type"`
		Config struct {
			Dir string `json:"dir"`
		} `json:"config"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.Name != "nas-2" || got.Type != "local" || got.Config.Dir != "/tmp" {
		t.Fatalf("updated destination = %+v", got)
	}

	// Partial update keeps existing values when fields are blank.
	rec = doJSON(router, http.MethodPut, "/api/v1/backup-destinations/"+id, org.Headers, `{"config":{"dir":"/var"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode partial: %v", err)
	}
	if got.Name != "nas-2" || got.Type != "local" || got.Config.Dir != "/var" {
		t.Fatalf("partially updated destination = %+v", got)
	}

	// Delete removes it; second delete misses.
	rec = doJSON(router, http.MethodDelete, "/api/v1/backup-destinations/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from backup_destinations where id=$1::uuid`, id); n != 0 {
		t.Fatalf("destination rows after delete = %d, want 0", n)
	}
	rec = doJSON(router, http.MethodDelete, "/api/v1/backup-destinations/"+id, org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", rec.Code)
	}
}

func TestBackupDestinationErrors(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	missing := "11111111-1111-1111-1111-111111111111"

	t.Run("create validation", func(t *testing.T) {
		for _, body := range []string{
			`{"type":"local"}`,
			`{"name":"x"}`,
			`{`,
		} {
			rec := doJSON(router, http.MethodPost, "/api/v1/backup-destinations", org.Headers, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("body %s: status = %d, want 400", body, rec.Code)
			}
		}
	})

	t.Run("get not found", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/backup-destinations/"+missing, org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("update not found", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, "/api/v1/backup-destinations/"+missing, org.Headers, `{"name":"x"}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("update malformed json", func(t *testing.T) {
		id := mustCreateDestination(t, router, org.Headers, "d", "local", `{}`)
		rec := doJSON(router, http.MethodPut, "/api/v1/backup-destinations/"+id, org.Headers, `{`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

func TestTestBackupDestinationTable(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	missing := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		typ    string
		config string
		want   int
	}{
		{"local ok", "local", `{}`, http.StatusOK},
		{"shared with path ok", "Shared ", `{"path":"/srv/b"}`, http.StatusOK},
		{"shared missing path", "shared", `{"other":1}`, http.StatusBadRequest},
		{"s3 ok", "s3", `{"bucket":"b"}`, http.StatusOK},
		{"unsupported type", "gcs", `{}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := mustCreateDestination(t, router, org.Headers, "dest "+tc.name, tc.typ, tc.config)
			rec := doJSON(router, http.MethodPost, "/api/v1/backup-destinations/"+id+"/test", org.Headers, "")
			if rec.Code != tc.want {
				t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}

	t.Run("not found", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/backup-destinations/"+missing+"/test", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestBackupDestinationAdminOnly(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	member := org.AddMember(t, rbac.RoleMember)
	id := mustCreateDestination(t, router, org.Headers, "dest", "local", `{}`)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"update", http.MethodPut, "/api/v1/backup-destinations/" + id, `{"name":"hax"` + `}`},
		{"delete", http.MethodDelete, "/api/v1/backup-destinations/" + id, ""},
		{"test", http.MethodPost, "/api/v1/backup-destinations/" + id + "/test", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, tc.method, tc.path, member.Headers, tc.body)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s as member status = %d, want 403", tc.method, rec.Code)
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

func bracedOrgHeader(headers http.Header) http.Header {
	// Postgres accepts the {uuid} brace form in ::uuid casts but
	// common.ToUUID rejects it, so RBAC passes yet the handler's own UUID
	// conversion fails.
	h := headers.Clone()
	h.Set("X-Organization-Id", "{"+h.Get("X-Organization-Id")+"}")
	return h
}

func TestListBackupsDatabaseErrorsReturn500(t *testing.T) {
	router := newRouterWithPool(t, simpleProtoPool(t))
	org := testdb.SeedOrg(t)

	t.Run("query failure", func(t *testing.T) {
		renameColumn(t, "backup_runs", "status", "status_gone")
		rec := doJSON(router, http.MethodGet, "/api/v1/backups", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})

	t.Run("scan failure", func(t *testing.T) {
		pool := testdb.Get(t)
		if _, err := pool.Exec(t.Context(), `
			insert into backup_runs(target_type, target_id, status) values ('volume', 'vol-null', 'queued')
		`); err != nil {
			t.Fatalf("seed run: %v", err)
		}
		rec := doJSON(router, http.MethodGet, "/api/v1/backups", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})
}

func TestBackupEndpointsRejectUnauthenticated(t *testing.T) {
	router := newRouter(t)
	testdb.SeedOrg(t)
	id := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list backups", http.MethodGet, "/api/v1/backups"},
		{"restore", http.MethodPost, "/api/v1/backups/" + id + "/restore"},
		{"list destinations", http.MethodGet, "/api/v1/backup-destinations"},
		{"get destination", http.MethodGet, "/api/v1/backup-destinations/" + id},
		{"update destination", http.MethodPut, "/api/v1/backup-destinations/" + id},
		{"delete destination", http.MethodDelete, "/api/v1/backup-destinations/" + id},
		{"test destination", http.MethodPost, "/api/v1/backup-destinations/" + id + "/test"},
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

func TestListDestinationsQueryFailureReturns500(t *testing.T) {
	router := newRouterWithPool(t, simpleProtoPool(t))
	org := testdb.SeedOrg(t)

	t.Run("query failure", func(t *testing.T) {
		renameColumn(t, "backup_destinations", "type", "type_gone")
		rec := doJSON(router, http.MethodGet, "/api/v1/backup-destinations", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})
}

func TestDestinationStatementFailuresReturn400(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)

	t.Run("insert failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		renameColumn(t, "backup_destinations", "type", "type_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/backup-destinations", org.Headers,
			`{"name":"x","type":"local","config":{}}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("duplicate name insert failure", func(t *testing.T) {
		mustCreateDestination(t, router, org.Headers, "dupe", "local", `{}`)
		rec := doJSON(router, http.MethodPost, "/api/v1/backup-destinations", org.Headers,
			`{"name":"dupe","type":"local","config":{}}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("update exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := mustCreateDestination(t, router, org.Headers, "upd", "local", `{}`)
		renameColumn(t, "backup_destinations", "name", "name_gone")
		rec := doJSON(router, http.MethodPut, "/api/v1/backup-destinations/"+id, org.Headers, `{"name":"y"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := mustCreateDestination(t, router, org.Headers, "del", "local", `{}`)
		renameTable(t, "backup_destinations", "backup_destinations_gone")
		rec := doJSON(router, http.MethodDelete, "/api/v1/backup-destinations/"+id, org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func TestCreateBackupInvalidOrgHeaderStillCreates(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	rec := doJSON(router, http.MethodPost, "/api/v1/backups", bracedOrgHeader(org.Headers),
		`{"targetType":"volume","targetId":"vol-1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTestDestinationBracedOrgHeaderSucceeds(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := mustCreateDestination(t, router, org.Headers, "dest", "local", `{}`)
	rec := doJSON(router, http.MethodPost, "/api/v1/backup-destinations/"+id+"/test", bracedOrgHeader(org.Headers), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
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
