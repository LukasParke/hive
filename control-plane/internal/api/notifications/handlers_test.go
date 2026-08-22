package notifications

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newRouter wires the real auth middleware around every notification endpoint
// so JWTs and RBAC are exercised end to end against the shared Postgres pool.
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
		gr.Get("/api/v1/notifications", h.ListNotifications)
		gr.Post("/api/v1/notifications", h.CreateNotification)
		gr.Get("/api/v1/notifications/{id}", h.GetNotification)
		gr.Put("/api/v1/notifications/{id}", h.UpdateNotification)
		gr.Delete("/api/v1/notifications/{id}", h.DeleteNotification)
		gr.Post("/api/v1/notifications/{id}/test", h.TestNotification)
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

func seedNotification(t *testing.T, router http.Handler, headers http.Header, channel, target string) string {
	t.Helper()
	rec := doJSON(router, http.MethodPost, "/api/v1/notifications", headers,
		`{"channel":"`+channel+`","target":"`+target+`","enabled":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed notification status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("seed notification response = %s err=%v", rec.Body.String(), err)
	}
	return resp.ID
}

func TestCreateNotification(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)

	id := seedNotification(t, router, org.Headers, "slack", "https://hooks.example.test/slack")
	var channel, target string
	var enabled bool
	if err := testdb.Get(t).QueryRow(t.Context(),
		`select channel, target, enabled from notifications where id=$1::uuid`, id,
	).Scan(&channel, &target, &enabled); err != nil {
		t.Fatalf("reload notification: %v", err)
	}
	if channel != "slack" || target != "https://hooks.example.test/slack" || !enabled {
		t.Fatalf("notification = (%q,%q,%v)", channel, target, enabled)
	}
}

func TestCreateNotificationValidation(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	cases := []struct {
		name string
		body string
	}{
		{"missing channel", `{"target":"https://x"}`},
		{"missing target", `{"channel":"slack"}`},
		{"malformed json", `{"channel":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, http.MethodPost, "/api/v1/notifications", org.Headers, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListNotifications(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	first := seedNotification(t, router, org.Headers, "slack", "https://a")
	second := seedNotification(t, router, org.Headers, "email", "ops@example.test")

	rec := doJSON(router, http.MethodGet, "/api/v1/notifications", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID      string `json:"id"`
			Channel string `json:"channel"`
			Target  string `json:"target"`
			Enabled bool   `json:"enabled"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(resp.Items))
	}
	// Ordered newest first.
	if resp.Items[0].ID != second || resp.Items[1].ID != first {
		t.Fatalf("order = [%s %s], want newest first", resp.Items[0].ID, resp.Items[1].ID)
	}
	if resp.Items[0].Channel != "email" || !resp.Items[0].Enabled {
		t.Fatalf("item = %+v", resp.Items[0])
	}
}

func TestGetNotification(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedNotification(t, router, org.Headers, "webhook", "https://hook")

	rec := doJSON(router, http.MethodGet, "/api/v1/notifications/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID      string `json:"id"`
		Channel string `json:"channel"`
		Target  string `json:"target"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID != id || resp.Channel != "webhook" {
		t.Fatalf("response = %+v err=%v", resp, err)
	}

	rec = doJSON(router, http.MethodGet, "/api/v1/notifications/11111111-1111-1111-1111-111111111111", org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", rec.Code)
	}
}

func TestUpdateNotification(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedNotification(t, router, org.Headers, "slack", "https://old")

	rec := doJSON(router, http.MethodPut, "/api/v1/notifications/"+id, org.Headers,
		`{"channel":"teams","target":"https://new","enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Channel string `json:"channel"`
		Target  string `json:"target"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Channel != "teams" || resp.Target != "https://new" || resp.Enabled {
		t.Fatalf("updated notification = %+v", resp)
	}

	// Blank fields keep stored values.
	rec = doJSON(router, http.MethodPut, "/api/v1/notifications/"+id, org.Headers, `{"enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t,
		`select count(*) from notifications where id=$1::uuid and enabled and channel='teams'`, id); n != 1 {
		t.Fatalf("partial update rows = %d, want 1", n)
	}
}

func TestUpdateNotificationErrors(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"not found", "/api/v1/notifications/11111111-1111-1111-1111-111111111111", `{"channel":"x"}`, http.StatusNotFound},
		{"malformed json", "/api/v1/notifications/11111111-1111-1111-1111-111111111111", `{`, http.StatusBadRequest},
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

func TestDeleteNotification(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedNotification(t, router, org.Headers, "slack", "https://a")

	rec := doJSON(router, http.MethodDelete, "/api/v1/notifications/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from notifications where id=$1::uuid`, id); n != 0 {
		t.Fatalf("notification rows after delete = %d, want 0", n)
	}

	rec = doJSON(router, http.MethodDelete, "/api/v1/notifications/11111111-1111-1111-1111-111111111111", org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing delete status = %d, want 404", rec.Code)
	}
}

func TestTestNotificationSendsPayload(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)

	var mu sync.Mutex
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotPath = r.URL.Path
		_ = json.Unmarshal(b, &gotBody)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	id := seedNotification(t, router, org.Headers, "webhook", srv.URL+"/hook")
	rec := doJSON(router, http.MethodPost, "/api/v1/notifications/"+id+"/test", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("body = %s, want status ok", rec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/hook" {
		t.Fatalf("deliver path = %q, want /hook", gotPath)
	}
	if gotBody["event"] != "notification.test" || gotBody["channel"] != "webhook" {
		t.Fatalf("payload = %v", gotBody)
	}
	payload, _ := gotBody["payload"].(map[string]any)
	if payload["ok"] != true {
		t.Fatalf("nested payload = %v", payload)
	}
}

func TestTestNotificationFailures(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)

	t.Run("server error response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		id := seedNotification(t, router, org.Headers, "webhook", srv.URL)
		rec := doJSON(router, http.MethodPost, "/api/v1/notifications/"+id+"/test", org.Headers, "")
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "responded with status 500") {
			t.Fatalf("body = %s", rec.Body.String())
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		srv.Close()
		id := seedNotification(t, router, org.Headers, "webhook", srv.URL)
		rec := doJSON(router, http.MethodPost, "/api/v1/notifications/"+id+"/test", org.Headers, "")
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "notification test failed") {
			t.Fatalf("body = %s", rec.Body.String())
		}
	})

	t.Run("unparseable target", func(t *testing.T) {
		id := seedNotification(t, router, org.Headers, "webhook", "http://exa mple.invalid")
		rec := doJSON(router, http.MethodPost, "/api/v1/notifications/"+id+"/test", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "invalid notification target") {
			t.Fatalf("body = %s", rec.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/notifications/11111111-1111-1111-1111-111111111111/test", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestNotificationAdminOnly(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	member := org.AddMember(t, rbac.RoleMember)
	id := seedNotification(t, router, org.Headers, "slack", "https://a")

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"update", http.MethodPut, "/api/v1/notifications/" + id, `{"channel":"hax"}`},
		{"delete", http.MethodDelete, "/api/v1/notifications/" + id, ""},
		{"test send", http.MethodPost, "/api/v1/notifications/" + id + "/test", ""},
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

func TestNotificationEndpointsRejectUnauthenticated(t *testing.T) {
	router := newRouter(t)
	testdb.SeedOrg(t)
	id := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/v1/notifications"},
		{"get", http.MethodGet, "/api/v1/notifications/" + id},
		{"update", http.MethodPut, "/api/v1/notifications/" + id},
		{"delete", http.MethodDelete, "/api/v1/notifications/" + id},
		{"test", http.MethodPost, "/api/v1/notifications/" + id + "/test"},
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

func TestListNotificationsQueryFailureReturns500(t *testing.T) {
	router := newRouterWithPool(t, simpleProtoPool(t))
	org := testdb.SeedOrg(t)
	renameColumn(t, "notifications", "channel", "channel_gone")
	rec := doJSON(router, http.MethodGet, "/api/v1/notifications", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestNotificationStatementFailuresReturn400(t *testing.T) {
	t.Run("create insert failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		renameColumn(t, "notifications", "channel", "channel_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/notifications", org.Headers,
			`{"channel":"slack","target":"https://x"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("update exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := seedNotification(t, router, org.Headers, "slack", "https://a")
		renameColumn(t, "notifications", "channel", "channel_gone")
		rec := doJSON(router, http.MethodPut, "/api/v1/notifications/"+id, org.Headers, `{"channel":"x"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete exec failure", func(t *testing.T) {
		router := newRouterWithPool(t, simpleProtoPool(t))
		org := testdb.SeedOrg(t)
		id := seedNotification(t, router, org.Headers, "slack", "https://a")
		renameTable(t, "notifications", "notifications_gone")
		rec := doJSON(router, http.MethodDelete, "/api/v1/notifications/"+id, org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func TestCreateNotificationInvalidOrgHeaderSucceeds(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	rec := doJSON(router, http.MethodPost, "/api/v1/notifications", bracedOrgHeader(org.Headers),
		`{"channel":"slack","target":"https://x"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTestNotificationBracedOrgHeaderSucceeds(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	id := seedNotification(t, router, org.Headers, "webhook", "https://127.0.0.1:1/hook")
	rec := doJSON(router, http.MethodPost, "/api/v1/notifications/"+id+"/test", bracedOrgHeader(org.Headers), "")
	// The braced org passes RBAC; the send itself fails against the bogus
	// target, proving the request got past authorization.
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s, want 502", rec.Code, rec.Body.String())
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
