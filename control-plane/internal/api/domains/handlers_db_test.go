package domains

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/testdb"
	"github.com/moby/moby/api/types/swarm"
)

// recordingSwarm is a proxy.ServiceStore double: ListServices reports
// services; UpdateService records specs pushed by the domain applier.
type recordingSwarm struct {
	services   []swarm.Service
	listErr    error
	updateErr  error
	updateSpec []swarm.ServiceSpec
}

func (r *recordingSwarm) ListServices(context.Context) ([]swarm.Service, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.services, nil
}

func (r *recordingSwarm) UpdateService(_ context.Context, _ string, _ uint64, spec swarm.ServiceSpec) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updateSpec = append(r.updateSpec, spec)
	for i := range r.services {
		if r.services[i].ID == "svc-app" {
			r.services[i].Spec = spec
		}
	}
	return nil
}

// appService builds the swarm service carrying the hive.app.id label.
func appService(appID string, extra map[string]string) swarm.Service {
	labels := map[string]string{"hive.app.id": appID}
	for k, v := range extra {
		labels[k] = v
	}
	return swarm.Service{
		ID:   "svc-app",
		Meta: swarm.Meta{Version: swarm.Version{Index: 3}},
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web", Labels: labels}},
	}
}

// newDomainsRouter wires the real auth middleware in front of the handler.
func newDomainsRouter(t *testing.T, fake *recordingSwarm) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool, fake)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/domains", h.ListDomains)
		gr.Post("/api/v1/domains", h.CreateDomain)
		gr.Get("/api/v1/domains/{id}", h.GetDomain)
		gr.Put("/api/v1/domains/{id}", h.UpdateDomain)
		gr.Delete("/api/v1/domains/{id}", h.DeleteDomain)
	})
	return r
}

func authed(req *http.Request, org *testdb.OrgFixture) *http.Request {
	for k, vs := range org.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	return req
}

func call(t *testing.T, router http.Handler, method, path string, org *testdb.OrgFixture, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw := ""
	if body != nil {
		enc, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = string(enc)
	}
	return callRaw(t, router, method, path, org, raw)
}

func callRaw(t *testing.T, router http.Handler, method, path string, org *testdb.OrgFixture, raw string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authed(req, org))
	return rec
}

// fixture seeds an org + project + application and returns ids.
func fixture(t *testing.T, fake *recordingSwarm) (http.Handler, *testdb.OrgFixture, string) {
	t.Helper()
	router := newDomainsRouter(t, fake)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "web", "https://github.com/acme/web", nil)
	return router, org, appID
}

func seedDomain(t *testing.T, appID, host string) string {
	t.Helper()
	var id string
	err := testdb.Get(t).QueryRow(context.Background(),
		`insert into domains(application_id, hostname, tls_enabled, route_type, path_prefix, strip_prefix, priority)
		 values ($1::uuid, $2, false, 'host', '', false, null) returning id::text`,
		appID, host).Scan(&id)
	if err != nil {
		t.Fatalf("seed domain row: %v", err)
	}
	return id
}

func TestListDomainsEmptyAndPopulated(t *testing.T) {
	fake := &recordingSwarm{}
	router, org, appID := fixture(t, fake)

	rec := call(t, router, http.MethodGet, "/api/v1/domains", org, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"items"`) {
		t.Fatalf("unexpected empty body %q", rec.Body.String())
	}

	host := "app.example.com"
	if _, err := testdb.Get(t).Exec(context.Background(),
		`insert into domains(application_id, hostname, tls_enabled, route_type, path_prefix, strip_prefix, priority)
		 values ($1::uuid, $2, true, 'host', '', false, null)`, appID, host); err != nil {
		t.Fatalf("seed domain: %v", err)
	}
	rec = call(t, router, http.MethodGet, "/api/v1/domains", org, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), host) {
		t.Fatalf("populated list status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateDomainValidationTable(t *testing.T) {
	fake := &recordingSwarm{}
	router, org, appID := fixture(t, fake)
	unknown := "11111111-1111-4111-8111-111111111111"

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"missing hostname", map[string]any{"applicationId": appID}, 400},
		{"bad hostname", map[string]any{"applicationId": appID, "hostname": "not a host"}, 400},
		{"wildcard without star", map[string]any{"applicationId": appID, "hostname": "a.example.com", "routeType": "wildcard"}, 400},
		{"path without prefix", map[string]any{"applicationId": appID, "hostname": "a.example.com", "routeType": "path"}, 400},
		{"unknown route type", map[string]any{"applicationId": appID, "hostname": "a.example.com", "routeType": "regex"}, 400},
		{"negative priority", map[string]any{"applicationId": appID, "hostname": "a.example.com", "priority": -1}, 400},
		{"unknown application", map[string]any{"applicationId": unknown, "hostname": "a.example.com"}, 400},
	}
	for _, tc := range cases {
		rec := call(t, router, http.MethodPost, "/api/v1/domains", org, tc.body)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d body=%s want %d", tc.name, rec.Code, rec.Body.String(), tc.want)
		}
	}
	if rec := callRaw(t, router, http.MethodPost, "/api/v1/domains", org, `{invalid`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed payload status = %d", rec.Code)
	}

	// Happy path: hostname normalization (scheme/port/case stripped) and a
	// 201 with the generated id.
	rec := call(t, router, http.MethodPost, "/api/v1/domains", org,
		map[string]any{"applicationId": appID, "hostname": "HTTPS://App.Example.com:443/", "tlsEnabled": true})
	if rec.Code != http.StatusCreated {
		t.Fatalf("happy create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("create response = %q (%v)", rec.Body.String(), err)
	}
}

func TestGetDomainFoundAndMissing(t *testing.T) {
	fake := &recordingSwarm{}
	router, org, appID := fixture(t, fake)
	id := seedDomain(t, appID, "get.example.com")

	rec := call(t, router, http.MethodGet, "/api/v1/domains/"+id, org, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "get.example.com") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = call(t, router, http.MethodGet, "/api/v1/domains/00000000-0000-4000-8000-00000000000f", org, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing domain status = %d", rec.Code)
	}
}

func TestUpdateDomainMergesAndReapplies(t *testing.T) {
	fake := &recordingSwarm{}
	router, org, appID := fixture(t, fake)
	id := seedDomain(t, appID, "old.example.com")
	fake.services = []swarm.Service{appService(appID, nil)}

	rec := call(t, router, http.MethodPut, "/api/v1/domains/"+id, org,
		map[string]any{"hostname": "new.example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(fake.updateSpec) == 0 {
		t.Fatal("expected service update after hostname change")
	}

	rec = call(t, router, http.MethodPut, "/api/v1/domains/00000000-0000-4000-8000-00000000000e", org,
		map[string]any{"hostname": "x.example.com"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing update status = %d", rec.Code)
	}
	rec = call(t, router, http.MethodPut, "/api/v1/domains/"+id, org,
		map[string]any{"hostname": "plain.example.com", "routeType": "wildcard"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid merge status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := callRaw(t, router, http.MethodPut, "/api/v1/domains/"+id, org, `{oops`); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed update status = %d", rec.Code)
	}
}

func TestDeleteDomainRemovesRouteAndRow(t *testing.T) {
	fake := &recordingSwarm{}
	router, org, appID := fixture(t, fake)
	fake.services = []swarm.Service{appService(appID, nil)}
	id := seedDomain(t, appID, "del.example.com")

	rec := call(t, router, http.MethodDelete, "/api/v1/domains/"+id, org, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = call(t, router, http.MethodDelete, "/api/v1/domains/"+id, org, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", rec.Code)
	}
}

func TestApplyDomainsForAppLockedRoutesAndPorts(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "web", "https://github.com/acme/web", nil)
	fake := &recordingSwarm{services: []swarm.Service{
		appService(appID, map[string]string{"hive.app.port": "8080"}),
	}}
	h := NewHandler(pool, fake)
	if err := h.applyDomainsForAppLocked(context.Background(), appID); err != nil {
		t.Fatalf("applyDomainsForAppLocked with no rows: %v", err)
	}
	if len(fake.updateSpec) != 0 {
		t.Fatal("no domain rows must mean no service updates")
	}

	for _, d := range []struct{ host, rt, prefix string }{
		{"app.example.com", "host", ""},
		{"*.wild.example.com", "wildcard", ""},
		{"path.example.com", "path", "/api"},
	} {
		if _, err := pool.Exec(context.Background(),
			`insert into domains(application_id, hostname, route_type, path_prefix) values ($1::uuid,$2,$3,$4)`,
			appID, d.host, d.rt, d.prefix); err != nil {
			t.Fatalf("seed %s: %v", d.host, err)
		}
	}
	if err := h.applyDomainsForAppLocked(context.Background(), appID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(fake.updateSpec) != 3 {
		t.Fatalf("expected one update per domain row, got %d", len(fake.updateSpec))
	}
	last := fake.updateSpec[len(fake.updateSpec)-1].Labels
	if got := last["traefik.http.services.app-path-example-com.loadbalancer.server.port"]; got != "8080" {
		t.Fatalf("port label = %q, want 8080 from hive.app.port", got)
	}

	// No matching service is a no-op.
	if err := h.applyDomainsForAppLocked(context.Background(), "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"); err != nil {
		t.Fatalf("no-service apply should be nil, got %v", err)
	}
	// Swarm listing failure surfaces.
	fake.listErr = swarmListErr{}
	if err := h.applyDomainsForAppLocked(context.Background(), appID); err == nil {
		t.Fatal("expected swarm failure to surface")
	}
}

type swarmListErr struct{}

func (swarmListErr) Error() string { return "swarm list failed" }

func TestRemoveDomainRouteBranches(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "web", "https://github.com/acme/web", nil)
	fake := &recordingSwarm{services: []swarm.Service{appService(appID, nil)}}
	h := NewHandler(pool, fake)

	if err := h.removeDomainRoute(context.Background(), appID, "del.example.com"); err != nil {
		t.Fatalf("removeDomainRoute: %v", err)
	}
	if len(fake.updateSpec) != 1 {
		t.Fatalf("expected exactly one removal update, got %d", len(fake.updateSpec))
	}

	// Service without the application label: no-op.
	unlabeled := &recordingSwarm{services: []swarm.Service{appService("other", nil)}}
	if err := NewHandler(pool, unlabeled).removeDomainRoute(context.Background(), appID, "del.example.com"); err != nil {
		t.Fatalf("unlabeled service: %v", err)
	}
	if len(unlabeled.updateSpec) != 0 {
		t.Fatal("no update expected for unlabeled services")
	}

	// Swarm failure surfaces through the app lock.
	fake.listErr = swarmListErr{}
	if err := h.removeDomainRoute(context.Background(), appID, "del.example.com"); err == nil {
		t.Fatal("expected swarm failure to surface")
	}
}

func TestEndpointsRequireAuthorization(t *testing.T) {
	fake := &recordingSwarm{}
	router := newDomainsRouter(t, fake)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "web", "https://github.com/acme/web", nil)
	_ = seedDomain(t, appID, "x.example.com")

	reqs := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/domains"},
		{http.MethodPost, "/api/v1/domains"},
		{http.MethodGet, "/api/v1/domains/some-id"},
		{http.MethodPut, "/api/v1/domains/some-id"},
		{http.MethodDelete, "/api/v1/domains/some-id"},
	}
	// No auth at all.
	for _, r := range reqs {
		req := httptest.NewRequest(r.method, r.path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s unauthenticated: status %d, want 401", r.method, r.path, rec.Code)
		}
	}
}

func TestCreateDomainAppliesRoutesAndSurfacesApplyFailure(t *testing.T) {
	fake := &recordingSwarm{}
	router, org, appID := fixture(t, fake)
	fake.services = []swarm.Service{appService(appID, nil)}

	rec := call(t, router, http.MethodPost, "/api/v1/domains", org,
		map[string]any{"applicationId": appID, "hostname": "live.example.com"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(fake.updateSpec) == 0 {
		t.Fatal("expected routing labels applied right after create")
	}

	// A failing swarm update must fail the request after the row was written.
	fake.updateErr = swarmListErr{}
	fake.updateSpec = nil
	rec = call(t, router, http.MethodPost, "/api/v1/domains", org,
		map[string]any{"applicationId": appID, "hostname": "broken.example.com"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create with failing apply status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateDomainMergesEveryFieldAndRemovesOldRoute(t *testing.T) {
	fake := &recordingSwarm{}
	router, org, appID := fixture(t, fake)
	id := seedDomain(t, appID, "before.example.com")
	fake.services = []swarm.Service{appService(appID, map[string]string{
		"traefik.http.routers.app-before-example-com.priority": "7",
	})}

	rec := call(t, router, http.MethodPut, "/api/v1/domains/"+id, org, map[string]any{
		"hostname":    "after.example.com",
		"tlsEnabled":  true,
		"routeType":   "path",
		"pathPrefix":  "api",
		"stripPrefix": true,
		"priority":    42,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("merge-update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		TLSEnabled  bool   `json:"tlsEnabled"`
		RouteType   string `json:"routeType"`
		PathPrefix  string `json:"pathPrefix"`
		StripPrefix bool   `json:"stripPrefix"`
		Priority    *int   `json:"priority"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.TLSEnabled || out.RouteType != "path" || out.PathPrefix != "/api" || !out.StripPrefix || out.Priority == nil || *out.Priority != 42 {
		t.Fatalf("merged domain = %+v", out)
	}
	if len(fake.updateSpec) < 2 {
		t.Fatalf("expected old-route removal + re-apply updates, got %d", len(fake.updateSpec))
	}
	stalePriorityCleared := true
	for _, spec := range fake.updateSpec {
		if _, ok := spec.Labels["traefik.http.routers.app-before-example-com.priority"]; ok {
			stalePriorityCleared = false
		}
	}
	if !stalePriorityCleared {
		t.Fatal("old router labels must be removed when the hostname changes")
	}
}

// TestEndpointsDenyNonMembers covers the RequireOrgAccess short-circuit of
// every endpoint: an authenticated user without org membership gets 403
// (unauthenticated requests already get 401 from the middleware).
func TestEndpointsDenyNonMembers(t *testing.T) {
	router := newDomainsRouter(t, &recordingSwarm{})
	org := testdb.SeedOrg(t)

	ctx := context.Background()
	authSvc := testdb.Auth(t)
	email := fmt.Sprintf("outsider-%s@test.local", strings.ReplaceAll(org.OrgID, "-", "")[:12])
	if _, err := authSvc.Register(ctx, email, "sup3rsecret!", "Outsider"); err != nil {
		t.Fatalf("register outsider: %v", err)
	}
	token, _, err := authSvc.Login(ctx, email, "sup3rsecret!")
	if err != nil {
		t.Fatalf("login outsider: %v", err)
	}

	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/domains"},
		{http.MethodPost, "/api/v1/domains"},
		{http.MethodGet, "/api/v1/domains/some-id"},
		{http.MethodPut, "/api/v1/domains/some-id"},
		{http.MethodDelete, "/api/v1/domains/some-id"},
	} {
		req := httptest.NewRequest(r.method, r.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Organization-Id", org.OrgID)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want 403 (body=%s)", r.method, r.path, rec.Code, rec.Body.String())
		}
	}
}

// TestUpdateDomainCoalescesWithStoredRow covers the partial-field merge:
// omitted fields keep their stored values (including a non-null priority)
// and an invalid replacement hostname is rejected.
func TestUpdateDomainCoalescesWithStoredRow(t *testing.T) {
	fake := &recordingSwarm{}
	router, org, appID := fixture(t, fake)
	fake.services = []swarm.Service{appService(appID, nil)}

	var id string
	if err := testdb.Get(t).QueryRow(context.Background(),
		`insert into domains(application_id, hostname, tls_enabled, route_type, path_prefix, strip_prefix, priority)
		 values ($1::uuid, 'p.example.com', false, 'path', '/api', true, 5) returning id::text`,
		appID).Scan(&id); err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	rec := call(t, router, http.MethodPut, "/api/v1/domains/"+id, org,
		map[string]any{"tlsEnabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("coalesce update status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Priority    *int   `json:"priority"`
		StripPrefix bool   `json:"stripPrefix"`
		RouteType   string `json:"routeType"`
		PathPrefix  string `json:"pathPrefix"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Priority == nil || *out.Priority != 5 || !out.StripPrefix ||
		out.RouteType != "path" || out.PathPrefix != "/api" {
		t.Fatalf("stored fields not preserved: %+v", out)
	}

	rec = call(t, router, http.MethodPut, "/api/v1/domains/"+id, org,
		map[string]any{"hostname": "bad_.example.com"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid hostname update status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestQueryFailuresWhenDomainsTableUnavailable forces the domains queries to
// fail by renaming the table away and pointing the handler at a fresh pool
// (the shared pool's prepared-statement cache would keep the old OID alive).
func TestQueryFailuresWhenDomainsTableUnavailable(t *testing.T) {
	pool := testdb.Get(t)
	fake := &recordingSwarm{}
	_, org, appID := fixture(t, fake)
	_ = seedDomain(t, appID, "x.example.com")

	if _, err := pool.Exec(context.Background(), `alter table domains rename to domains_cov_bak`); err != nil {
		t.Fatalf("rename domains: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `alter table domains_cov_bak rename to domains`)
	})

	fresh, err := pgxpool.New(context.Background(), pool.Config().ConnString())
	if err != nil {
		t.Fatalf("open fresh pool: %v", err)
	}
	defer fresh.Close()
	h := NewHandler(fresh, fake)
	gr := chi.NewRouter()
	gr.Use(apimiddleware.WithAuth(testdb.Auth(t), fresh))
	gr.Get("/api/v1/domains", h.ListDomains)
	gr.Put("/api/v1/domains/{id}", h.UpdateDomain)
	gr.Delete("/api/v1/domains/{id}", h.DeleteDomain)

	if rec := call(t, gr, http.MethodGet, "/api/v1/domains", org, nil); rec.Code != http.StatusInternalServerError {
		t.Errorf("list with failing query status = %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec := call(t, gr, http.MethodPut, "/api/v1/domains/some-id", org, map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Errorf("update with failing exec status = %d, want 400", rec.Code)
	}
	if rec := call(t, gr, http.MethodDelete, "/api/v1/domains/some-id", org, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("delete with failing exec status = %d, want 400", rec.Code)
	}
}
