package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/cloudflare"
	"github.com/luke/hive/control-plane/internal/testdb"
	tunnels "github.com/luke/hive/control-plane/internal/tunnels"
)

// newRouterWithMgr builds a chi router around an arbitrary Manager seam.
func newRouterWithMgr(mgr Manager, _ struct{}) http.Handler {
	h := &Handler{Mgr: mgr, authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
	r := chi.NewRouter()
	r.Get("/api/v1/tunnels", h.ListTunnels)
	return r
}

// listErrManager overrides List to fail.
type listErrManager struct {
	*fakeManager
	err error
}

func (m *listErrManager) List(context.Context) ([]*tunnels.View, error) {
	return nil, m.err
}

func TestListTunnelsErrorMapsToBadGateway(t *testing.T) {
	mgr := &listErrManager{fakeManager: &fakeManager{}, err: errors.New("db down")}
	rec := do(t, newRouterWithMgr(mgr, struct{}{}), http.MethodGet, "/api/v1/tunnels", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateIngressInvalidPayload(t *testing.T) {
	mgr := &fakeManager{views: map[string]*tunnels.View{"created": testView()}}
	h := newTestHandler(mgr, true)
	rec := do(t, h, http.MethodPut, "/api/v1/tunnels/tid/ingress", `{invalid`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUpdateIngressErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{tunnels.ErrNotFound, http.StatusNotFound},
		{tunnels.ErrConflict, http.StatusConflict},
		{errors.New("swarm down"), http.StatusBadGateway},
	}
	for _, tc := range cases {
		mgr := &fakeManager{updateErr: tc.err}
		h := newTestHandler(mgr, true)
		rec := do(t, h, http.MethodPut, "/api/v1/tunnels/tid/ingress",
			`{"ingress":[{"hostname":"a.example.com","service":"http://x"}]}`)
		if rec.Code != tc.want {
			t.Errorf("err %v -> status %d, want %d", tc.err, rec.Code, tc.want)
		}
	}
}

func TestDeleteTunnelErrorMapsToBadGateway(t *testing.T) {
	mgr := &fakeManager{deleteErr: errors.New("teardown failed")}
	h := newTestHandler(mgr, true)
	rec := do(t, h, http.MethodDelete, "/api/v1/tunnels/tid", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

// --- DB-backed wiring tests ---

func TestNewHandlerWiresManager(t *testing.T) {
	h := NewHandler(nil, nil, func(string) cloudflare.API { return nil })
	if h == nil || h.Mgr == nil {
		t.Fatal("NewHandler must wire a Manager")
	}
	if h.authorizeOverride != nil {
		t.Fatal("authorizeOverride must be nil in production wiring")
	}
}

func TestAuthorizeAndAuditAgainstRealDB(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)

	h := &Handler{Pool: pool, Mgr: &fakeManager{}}
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/tunnels/{id}", h.GetTunnel)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tunnels/some-id", nil)
	for k, vs := range org.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	// Owner passes authorization; the unknown tunnel id then 404s via the manager seam... but our
	// fakeManager is not wired into this router's handler closure? It is: h.Mgr is the fake.
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("authorized request status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("owner should be authorized, got %d", rec.Code)
	}

	// Audit writes an audit_log row when a pool is present.
	h.audit(req, "create", "tunnel", "tid-1", map[string]any{"name": "x"})
	if n := testdb.QueryCount(t, `select count(*) from audit_log where resource_id = 'tid-1'`); n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
	// Malformed details are swallowed silently.
	h.audit(req, "create", "tunnel", "tid-2", map[string]any{"bad": func() {}})
}

func TestCreateTunnelAuditsAgainstRealDB(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)

	mgr := &fakeManager{views: map[string]*tunnels.View{"created": testView()}}
	h := &Handler{Pool: pool, Mgr: mgr}
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Post("/api/v1/tunnels", h.CreateTunnel)
	})
	body := `{"name":"edge","accountId":"acc","apiToken":"tok","zoneId":"z","ingress":[{"hostname":"a.example.com","service":"http://traefik:80"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnels", strings.NewReader(body))
	for k, vs := range org.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from audit_log where action = 'create' and resource_type = 'tunnel'`); n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
	var payload struct {
		Connector *struct {
			CloudflareStatus string `json:"cloudflareStatus"`
		} `json:"connector"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
}

func TestToPayloadNormalizesIngressAndError(t *testing.T) {
	v := &tunnels.View{
		Row: &tunnels.Row{
			ID: "id", Name: "n", CfTunnelID: "cf", Status: tunnels.StatusError,
			ErrorMessage: "deploy failed",
		},
	}
	p := toPayload(v)
	if p.Ingress == nil || len(p.Ingress) != 0 {
		t.Fatalf("nil ingress must serialize as [], got %+v", p.Ingress)
	}
	if p.ErrorMessage != "deploy failed" {
		t.Fatalf("error message = %q", p.ErrorMessage)
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), `"ingress":null`) {
		t.Fatal("ingress must not serialize as null")
	}
}

func TestListTunnelsSerializesItems(t *testing.T) {
	mgr := &fakeManager{list: []*tunnels.View{testView(), testView()}}
	h := newTestHandler(mgr, true)
	rec := do(t, h, http.MethodGet, "/api/v1/tunnels", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(out.Items))
	}
}

func TestUpdateAndDeleteRequireAuth(t *testing.T) {
	mgr := &fakeManager{views: map[string]*tunnels.View{"created": testView()}}
	h := newTestHandler(mgr, false)
	if rec := do(t, h, http.MethodPut, "/api/v1/tunnels/tid/ingress", `{"ingress":[]}`); rec.Code != http.StatusForbidden {
		t.Errorf("PUT status = %d, want 403", rec.Code)
	}
	if rec := do(t, h, http.MethodDelete, "/api/v1/tunnels/tid", ""); rec.Code != http.StatusForbidden {
		t.Errorf("DELETE status = %d, want 403", rec.Code)
	}
}
