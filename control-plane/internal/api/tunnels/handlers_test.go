package tunnels

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/luke/hive/control-plane/internal/tunnels"
)

// fakeManager records handler calls and returns canned results.
type fakeManager struct {
	views      map[string]*tunnels.View
	list       []*tunnels.View
	createErr  error
	getErr     error
	updateErr  error
	deleteErr  error
	lastCreate tunnels.CreateParams
	lastUpdate []tunnels.IngressRule
	deletedID  string
}

func (f *fakeManager) Create(_ context.Context, p tunnels.CreateParams) (*tunnels.View, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.lastCreate = p
	return f.views["created"], nil
}

func (f *fakeManager) List(context.Context) ([]*tunnels.View, error) { return f.list, nil }

func (f *fakeManager) Get(_ context.Context, id string) (*tunnels.View, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	v, ok := f.views[id]
	if !ok {
		return nil, tunnels.ErrNotFound
	}
	return v, nil
}

func (f *fakeManager) UpdateIngress(_ context.Context, _ string, rules []tunnels.IngressRule) (*tunnels.View, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.lastUpdate = rules
	return f.views["created"], nil
}

func (f *fakeManager) Delete(_ context.Context, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedID = id
	return nil
}

// testView builds a deterministic view for response-shape assertions.
func testView() *tunnels.View {
	return &tunnels.View{
		Row: &tunnels.Row{
			ID:         "550e8400-e29b-41d4-a716-446655440010",
			Name:       "prod-edge",
			CfTunnelID: "cf-123",
			Status:     tunnels.StatusDeployed,
			Ingress: []tunnels.IngressRule{
				{Hostname: "app.example.com", Service: "http://traefik:80"},
				{Hostname: "*.example.com", Path: "/api", Service: "http://traefik:80"},
			},
			CreatedAt: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 8, 21, 12, 5, 0, 0, time.UTC),
		},
		Connector: tunnels.ConnectorStatus{DesiredReplicas: 1, RunningReplicas: 1, CloudflareStatus: "healthy"},
	}
}

func newTestHandler(mgr *fakeManager, allow bool) http.Handler {
	h := &Handler{Mgr: mgr, authorizeOverride: func(w http.ResponseWriter, r *http.Request) bool {
		if !allow {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		}
		return allow
	}}
	r := chi.NewRouter()
	r.Get("/api/v1/tunnels", h.ListTunnels)
	r.Post("/api/v1/tunnels", h.CreateTunnel)
	r.Get("/api/v1/tunnels/{id}", h.GetTunnel)
	r.Put("/api/v1/tunnels/{id}/ingress", h.UpdateTunnelIngress)
	r.Delete("/api/v1/tunnels/{id}", h.DeleteTunnel)
	return r
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCreateTunnelRequiresAuth(t *testing.T) {
	h := newTestHandler(&fakeManager{}, false)
	rec := do(t, h, http.MethodPost, "/api/v1/tunnels", `{}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestListAndGetRequireAuth(t *testing.T) {
	mgr := &fakeManager{}
	h := newTestHandler(mgr, false)
	for _, tc := range [][2]string{
		{http.MethodGet, "/api/v1/tunnels"},
		{http.MethodGet, "/api/v1/tunnels/xid"},
	} {
		rec := do(t, h, tc[0], tc[1], "")
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want 403", tc[0], tc[1], rec.Code)
		}
	}
}

func TestCreateTunnelPayloadValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{"name":`},
		{"missing name", `{"accountId":"a","apiToken":"t","ingress":[{"hostname":"a.com","service":"http://x"}]}`},
		{"missing account", `{"name":"n","apiToken":"t","ingress":[{"hostname":"a.com","service":"http://x"}]}`},
		{"missing token", `{"name":"n","accountId":"a","ingress":[{"hostname":"a.com","service":"http://x"}]}`},
		{"empty ingress", `{"name":"n","accountId":"a","apiToken":"t","ingress":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &fakeManager{}
			h := newTestHandler(mgr, true)
			rec := do(t, h, http.MethodPost, "/api/v1/tunnels", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateTunnelSuccessShapeNeverEchoesToken(t *testing.T) {
	mgr := &fakeManager{views: map[string]*tunnels.View{"created": testView()}}
	h := newTestHandler(mgr, true)
	body := `{"name":"prod-edge","accountId":"acc","zoneId":"zone","apiToken":"super-secret","ingress":[{"hostname":"app.example.com","service":"http://traefik:80"}]}`
	rec := do(t, h, http.MethodPost, "/api/v1/tunnels", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "super-secret") || strings.Contains(rec.Body.String(), "apiToken") {
		t.Errorf("response leaks token material: %s", rec.Body.String())
	}
	assertTunnelShape(t, rec.Body.String())
	if mgr.lastCreate.Name != "prod-edge" || mgr.lastCreate.APIToken != "super-secret" {
		t.Errorf("params not forwarded: %+v", mgr.lastCreate)
	}
}

func assertTunnelShape(t *testing.T, body string) {
	t.Helper()
	for _, key := range []string{
		`"id"`, `"name"`, `"cloudflareTunnelId"`, `"status"`, `"ingress"`,
		`"connector"`, `"desiredReplicas"`, `"runningReplicas"`, `"cloudflareStatus"`,
		`"createdAt"`, `"updatedAt"`,
	} {
		if !strings.Contains(body, key) {
			t.Errorf("response missing %q: %s", key, body)
		}
	}
	for _, forbidden := range []string{"credentialsJSON", "credentialSecretName", "dnsRecords"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response leaks internal field %q", forbidden)
		}
	}
}

func TestGetTunnelSuccessAndNotFound(t *testing.T) {
	mgr := &fakeManager{views: map[string]*tunnels.View{"tid": testView()}}
	h := newTestHandler(mgr, true)
	rec := do(t, h, http.MethodGet, "/api/v1/tunnels/tid", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	assertTunnelShape(t, rec.Body.String())

	rec = do(t, h, http.MethodGet, "/api/v1/tunnels/missing", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", rec.Code)
	}
}

func TestDeleteTunnelReturnsStatusOK(t *testing.T) {
	mgr := &fakeManager{}
	h := newTestHandler(mgr, true)
	rec := do(t, h, http.MethodDelete, "/api/v1/tunnels/tid", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Errorf("body = %q, want {\"status\":\"ok\"}", rec.Body.String())
	}
	if mgr.deletedID != "tid" {
		t.Errorf("deleted id = %q", mgr.deletedID)
	}
}

// fmtWrapped wraps err with context the way manager code does.
func fmtWrapped(err error) error {
	return errors.Join(errors.New("deploy failed"), err)
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"invalid input", tunnels.InvalidInput("bad name"), http.StatusBadRequest},
		{"not found", tunnels.ErrNotFound, http.StatusNotFound},
		{"conflict", tunnels.ErrConflict, http.StatusConflict},
		{"wrapped conflict", fmtWrapped(tunnels.ErrConflict), http.StatusConflict},
		{"runtime", errors.New("boom"), http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &fakeManager{createErr: tc.err}
			h := newTestHandler(mgr, true)
			rec := do(t, h, http.MethodPost, "/api/v1/tunnels",
				`{"name":"n","accountId":"a","apiToken":"t","ingress":[{"hostname":"a.com","service":"http://x"}]}`)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestUpdateIngressForwardsRules(t *testing.T) {
	mgr := &fakeManager{views: map[string]*tunnels.View{"created": testView()}}
	h := newTestHandler(mgr, true)
	rec := do(t, h, http.MethodPut, "/api/v1/tunnels/tid/ingress",
		`{"ingress":[{"hostname":"*.example.org","service":"http://traefik:80"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if len(mgr.lastUpdate) != 1 || mgr.lastUpdate[0].Hostname != "*.example.org" {
		t.Errorf("rules not forwarded: %+v", mgr.lastUpdate)
	}
}
