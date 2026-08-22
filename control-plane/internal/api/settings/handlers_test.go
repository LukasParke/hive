package settings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newSettingsRouter wires the real auth middleware in front of the handler.
func newSettingsRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()
	h := NewHandler(pool)
	r := http.NewServeMux()
	authed := apimiddleware.WithAuth(testdb.Auth(t), pool)
	r.Handle("GET /api/v1/settings", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authed(http.HandlerFunc(h.GetSettings)).ServeHTTP(w, req)
	}))
	r.Handle("PUT /api/v1/settings", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authed(http.HandlerFunc(h.PutSettings)).ServeHTTP(w, req)
	}))
	return r
}

// deadPool returns a closed pool so any query fails immediately.
func deadPool(t *testing.T) *pgxpool.Pool {
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

func call(t *testing.T, router http.Handler, method, path, body string, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
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

func TestGetSettingsReturnsSeededRows(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	router := newSettingsRouter(t, pool)

	for key, val := range map[string]string{
		"cluster": `{"nodes":3}`,
		"servers": `[{"name":"w1"}]`,
	} {
		if _, err := pool.Exec(context.Background(),
			`insert into app_settings(key, value) values ($1, $2::jsonb)`, key, val); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	rec := call(t, router, http.MethodGet, "/api/v1/settings", "", org.Headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %s, want 2 keys", rec.Body.String())
	}
	if string(resp.Items["cluster"]) != `{"nodes":3}` {
		t.Fatalf("cluster = %s", resp.Items["cluster"])
	}
	if string(resp.Items["servers"]) != `[{"name":"w1"}]` {
		t.Fatalf("servers = %s", resp.Items["servers"])
	}
}

func TestGetSettingsEmpty(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	router := newSettingsRouter(t, testdb.Get(t))

	rec := call(t, router, http.MethodGet, "/api/v1/settings", "", org.Headers)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":{}`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetSettingsQueryFailureReturns500(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(deadPool(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	h.GetSettings(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestPutSettingsUpserts(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	router := newSettingsRouter(t, pool)

	rec := call(t, router, http.MethodPut, "/api/v1/settings",
		`{"cluster":{"nodes":1},"servers":["a","b"]}`, org.Headers)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var nodes int
	if err := pool.QueryRow(context.Background(),
		`select (value->>'nodes')::int from app_settings where key='cluster'`).Scan(&nodes); err != nil {
		t.Fatalf("cluster row missing: %v", err)
	}
	if nodes != 1 {
		t.Fatalf("nodes = %d, want 1", nodes)
	}

	// A second PUT replaces the value for an existing key.
	rec = call(t, router, http.MethodPut, "/api/v1/settings", `{"cluster":{"nodes":9}}`, org.Headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("second put status = %d", rec.Code)
	}
	if err := pool.QueryRow(context.Background(),
		`select (value->>'nodes')::int from app_settings where key='cluster'`).Scan(&nodes); err != nil {
		t.Fatalf("cluster row missing after update: %v", err)
	}
	if nodes != 9 {
		t.Fatalf("nodes = %d, want 9 after upsert", nodes)
	}
	if n := testdb.QueryCount(t, `select count(*) from app_settings`); n != 2 {
		t.Fatalf("app_settings rows = %d, want 2", n)
	}
}

func TestPutSettingsInvalidPayload(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	router := newSettingsRouter(t, testdb.Get(t))

	rec := call(t, router, http.MethodPut, "/api/v1/settings", `{"broken":`, org.Headers)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid payload") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetSettingsScanFailureReturns500(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)

	if _, err := pool.Exec(context.Background(),
		`insert into app_settings(key, value) values ('k', '{"a":1}')`); err != nil {
		t.Fatalf("seed: %v", err)
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
	router := newSettingsRouter(t, fresh)
	// Make the key column unscannable as text while the query still succeeds.
	if _, err := pool.Exec(context.Background(),
		`alter table app_settings alter column key type text[] using array[key]`); err != nil {
		t.Fatalf("alter: %v", err)
	}
	rec := call(t, router, http.MethodGet, "/api/v1/settings", "", org.Headers)
	if _, err := pool.Exec(context.Background(),
		`alter table app_settings alter column key type text using key[1]`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestPutSettingsExecFailureReturns400(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(deadPool(t))

	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(`{"k":"v"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.PutSettings(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
