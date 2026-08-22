package events

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/realtime"
	"github.com/luke/hive/control-plane/internal/testdb"
)

func newEventsRouter(t *testing.T, hub *realtime.Hub) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool, testdb.Auth(t), hub)
	r := http.NewServeMux()
	authed := apimiddleware.WithAuth(testdb.Auth(t), pool)
	r.Handle("GET /api/v1/ws/events", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authed(http.HandlerFunc(h.WsEvents)).ServeHTTP(w, req)
	}))
	r.Handle("GET /api/v1/events", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authed(http.HandlerFunc(h.ListRequestEvents)).ServeHTTP(w, req)
	}))
	return r
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

func TestWsEventsNilHubReturns503(t *testing.T) {
	router := newEventsRouter(t, nil)
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/ws/events?access_token="+org.Token, org.Headers, "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "ws hub unavailable") {
		t.Fatalf("status = %d body=%s, want 503 hub unavailable", rec.Code, rec.Body.String())
	}
}

func TestWsEventsMissingTokenReturns401(t *testing.T) {
	router := newEventsRouter(t, realtime.NewHub())
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/ws/events", org.Headers, "")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "missing access token") {
		t.Fatalf("status = %d body=%s, want 401 missing token", rec.Code, rec.Body.String())
	}

	// Whitespace-only tokens are rejected too.
	rec = doJSON(t, router, http.MethodGet, "/api/v1/ws/events?access_token=%20%20", org.Headers, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("blank token status = %d, want 401", rec.Code)
	}
}

func TestWsEventsInvalidTokenReturns401(t *testing.T) {
	router := newEventsRouter(t, realtime.NewHub())
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/ws/events?access_token=not-a-jwt", org.Headers, "")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "invalid access token") {
		t.Fatalf("status = %d body=%s, want 401 invalid token", rec.Code, rec.Body.String())
	}
}

func TestListRequestEventsReturnsStoredRows(t *testing.T) {
	pool := testdb.Get(t)
	router := newEventsRouter(t, nil)
	org := testdb.SeedOrg(t)

	if _, err := pool.Exec(context.Background(),
		`insert into request_events(category, message, payload) values ('server', 'older', '{"n":1}')`); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`insert into request_events(category, message, payload) values ('deploy', 'newer', '{"ok":true}')`); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	rec := doJSON(t, router, http.MethodGet, "/api/v1/events", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID       string          `json:"id"`
			Category string          `json:"category"`
			Message  string          `json:"message"`
			Payload  json.RawMessage `json:"payload"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2 (%s)", len(resp.Items), rec.Body.String())
	}
	// Newest first.
	if resp.Items[0].Message != "newer" || resp.Items[1].Message != "older" {
		t.Fatalf("order = [%s %s], want [newer older]", resp.Items[0].Message, resp.Items[1].Message)
	}
	if string(resp.Items[0].Payload) != `{"ok":true}` {
		t.Fatalf("payload = %s", resp.Items[0].Payload)
	}
	for _, it := range resp.Items {
		if it.ID == "" || it.Category == "" {
			t.Fatalf("incomplete item %+v", it)
		}
	}
}

func TestListRequestEventsEmpty(t *testing.T) {
	router := newEventsRouter(t, nil)
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/events", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("empty list status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestListRequestEventsQueryFailureReturns500(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(deadEventsPool(t), testdb.Auth(t), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()
	h.ListRequestEvents(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// deadEventsPool returns a closed pool so any query fails immediately.
func deadEventsPool(t *testing.T) *pgxpool.Pool {
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
