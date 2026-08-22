package stacks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	dockernet "github.com/moby/moby/api/types/network"
	dockerswarm "github.com/moby/moby/api/types/swarm"

	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/deploy"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// fakeSwarm is an in-memory deploy.SwarmStack double recording every mutation.
type fakeSwarm struct {
	listSvcErr    error
	createSvcErr  error
	updateSvcErr  error
	removeSvcErr  error
	listNetErr    error
	createNetErr  error
	listSecretErr error
	createSecErr  error
	listCfgErr    error
	createCfgErr  error

	services    []dockerswarm.Service
	networks    map[string]string
	secretsByID map[string]dockerswarm.Secret
	configsByID map[string]dockerswarm.Config
	nextID      int

	createdServices   []dockerswarm.ServiceSpec
	updatedServiceIDs []string
	removedServiceIDs []string
}

func (f *fakeSwarm) ListServices(context.Context) ([]dockerswarm.Service, error) {
	if f.listSvcErr != nil {
		return nil, f.listSvcErr
	}
	return f.services, nil
}

func (f *fakeSwarm) CreateService(_ context.Context, spec dockerswarm.ServiceSpec) (string, error) {
	if f.createSvcErr != nil {
		return "", f.createSvcErr
	}
	f.createdServices = append(f.createdServices, spec)
	f.nextID++
	id := fmt.Sprintf("svc-new-%d", f.nextID)
	f.services = append(f.services, dockerswarm.Service{ID: id, Spec: spec})
	return id, nil
}

func (f *fakeSwarm) UpdateService(_ context.Context, id string, version uint64, spec dockerswarm.ServiceSpec) error {
	if f.updateSvcErr != nil {
		return f.updateSvcErr
	}
	f.updatedServiceIDs = append(f.updatedServiceIDs, id)
	for i := range f.services {
		if f.services[i].ID == id {
			f.services[i].Spec = spec
			f.services[i].Meta.Version.Index = version + 1 //nolint:staticcheck // test fixture
		}
	}
	return nil
}

func (f *fakeSwarm) RemoveService(_ context.Context, id string) error {
	if f.removeSvcErr != nil {
		return f.removeSvcErr
	}
	f.removedServiceIDs = append(f.removedServiceIDs, id)
	out := f.services[:0]
	for _, s := range f.services {
		if s.ID != id {
			out = append(out, s)
		}
	}
	f.services = out
	return nil
}

func (f *fakeSwarm) ListNetworks(context.Context) ([]dockernet.Summary, error) {
	if f.listNetErr != nil {
		return nil, f.listNetErr
	}
	out := make([]dockernet.Summary, 0, len(f.networks))
	for name, id := range f.networks {
		out = append(out, dockernet.Summary{Network: dockernet.Network{ID: id, Name: name}})
	}
	return out, nil
}

func (f *fakeSwarm) CreateNetwork(_ context.Context, name string) (string, error) {
	if f.createNetErr != nil {
		return "", f.createNetErr
	}
	if f.networks == nil {
		f.networks = map[string]string{}
	}
	id := name + "-id"
	f.networks[name] = id
	return id, nil
}

func (f *fakeSwarm) ListSecrets(context.Context) ([]dockerswarm.Secret, error) {
	if f.listSecretErr != nil {
		return nil, f.listSecretErr
	}
	out := make([]dockerswarm.Secret, 0, len(f.secretsByID))
	for _, s := range f.secretsByID {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSwarm) CreateSecret(_ context.Context, spec dockerswarm.SecretSpec) (string, error) {
	if f.createSecErr != nil {
		return "", f.createSecErr
	}
	f.nextID++
	id := fmt.Sprintf("sec-new-%d", f.nextID)
	if f.secretsByID == nil {
		f.secretsByID = map[string]dockerswarm.Secret{}
	}
	f.secretsByID[id] = dockerswarm.Secret{ID: id, Spec: spec}
	return id, nil
}

func (f *fakeSwarm) ListConfigs(context.Context) ([]dockerswarm.Config, error) {
	if f.listCfgErr != nil {
		return nil, f.listCfgErr
	}
	out := make([]dockerswarm.Config, 0, len(f.configsByID))
	for _, c := range f.configsByID {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeSwarm) CreateConfig(_ context.Context, spec dockerswarm.ConfigSpec) (string, error) {
	if f.createCfgErr != nil {
		return "", f.createCfgErr
	}
	f.nextID++
	id := fmt.Sprintf("cfg-new-%d", f.nextID)
	if f.configsByID == nil {
		f.configsByID = map[string]dockerswarm.Config{}
	}
	f.configsByID[id] = dockerswarm.Config{ID: id, Spec: spec}
	return id, nil
}

func (f *fakeSwarm) UpdateConfig(context.Context, string, uint64, dockerswarm.ConfigSpec) error {
	return nil
}

var _ deploy.SwarmStack = (*fakeSwarm)(nil)

const validCompose = "services:\n  web:\n    image: nginx:1.27\n"

// stackService builds a swarm service labeled as belonging to the given stack.
func stackService(id, namespace string) dockerswarm.Service {
	return dockerswarm.Service{
		ID: id,
		Spec: dockerswarm.ServiceSpec{
			Annotations: dockerswarm.Annotations{
				Name:   id,
				Labels: map[string]string{"com.docker.stack.namespace": namespace},
			},
		},
		Meta: dockerswarm.Meta{Version: dockerswarm.Version{Index: 3}},
	}
}

func newStacksRouter(t *testing.T, fake *fakeSwarm) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool, fake)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/stacks", h.ListStacks)
		gr.Post("/api/v1/stacks", h.CreateStack)
		gr.Get("/api/v1/stacks/{id}", h.GetStack)
		gr.Put("/api/v1/stacks/{id}", h.UpdateStack)
		gr.Delete("/api/v1/stacks/{id}", h.DeleteStack)
		gr.Post("/api/v1/stacks/{id}/deploy", h.DeployStack)
		gr.Post("/api/v1/stacks/{id}/start", h.StartStack)
		gr.Post("/api/v1/stacks/{id}/stop", h.StopStack)
		gr.Post("/api/v1/stacks/{id}/restart", h.RestartStack)
	})
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

func seedStack(t *testing.T, projectID, name, compose string) string {
	t.Helper()
	var id string
	if err := testdb.Get(t).QueryRow(context.Background(), `
		insert into stacks(project_id, name, compose_content) values ($1::uuid, $2, $3) returning id::text
	`, projectID, name, compose).Scan(&id); err != nil {
		t.Fatalf("seed stack: %v", err)
	}
	return id
}

func TestCreateStackHappyPathDeploysServices(t *testing.T) {
	fake := &fakeSwarm{}
	router := newStacksRouter(t, fake)
	org := testdb.SeedOrg(t)

	body := fmt.Sprintf(`{"projectId":%q,"name":"My Web!","composeContent":%q}`, org.ProjectID, validCompose)
	rec := doJSON(t, router, http.MethodPost, "/api/v1/stacks", org.Headers, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("decode id: %v %s", err, rec.Body.String())
	}

	// Row persisted with a normalized name.
	var name string
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select name from stacks where id=$1::uuid`, resp.ID).Scan(&name); err != nil {
		t.Fatalf("stack row missing: %v", err)
	}
	if name != "my-web" {
		t.Fatalf("name = %q, want my-web", name)
	}
	// Deploy happy path created the default network and one service.
	if len(fake.createdServices) != 1 || fake.createdServices[0].Annotations.Name != "my-web_web" { //nolint:staticcheck // test fixture
		svcNames := make([]string, 0, len(fake.createdServices))
		for _, s := range fake.createdServices {
			svcNames = append(svcNames, s.Annotations.Name) //nolint:staticcheck // test fixture
		}
		t.Fatalf("created services = %v, want [my-web_web]", svcNames)
	}
	if len(fake.networks) == 0 {
		t.Fatal("no networks created for stack")
	}
}

func TestCreateStackValidationAndDeployFailure(t *testing.T) {
	router := newStacksRouter(t, &fakeSwarm{})
	org := testdb.SeedOrg(t)

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"malformed json", `{broken`, http.StatusBadRequest, "invalid payload"},
		{"missing fields", `{"projectId":"","name":"","composeContent":""}`, http.StatusBadRequest, "required"},
		{"foreign project", fmt.Sprintf(`{"projectId":"%s","name":"x","composeContent":"services: {}"}`,
			testdb.SeedOrg(t).ProjectID), http.StatusBadRequest, ""},
		{"compose parse failure", `{"projectId":"PROJECT","name":"broken","composeContent":":\nnot: [valid"}`,
			http.StatusBadRequest, "parse compose file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.ReplaceAll(tc.body, "PROJECT", org.ProjectID)
			rec := doJSON(t, router, http.MethodPost, "/api/v1/stacks", org.Headers, body)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), tc.wantStatus)
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body = %s, want substring %q", rec.Body.String(), tc.wantBody)
			}
		})
	}
	if n := testdb.QueryCount(t, `select count(*) from stacks`); n > 1 {
		t.Fatalf("stacks rows = %d, expected at most the foreign-project case to fail cleanly", n)
	}
}

func TestListStacksEmptyPopulatedAndIsolated(t *testing.T) {
	router := newStacksRouter(t, &fakeSwarm{})
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/stacks", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":null`) {
		t.Fatalf("empty list status = %d body=%s", rec.Code, rec.Body.String())
	}

	first := seedStack(t, org.ProjectID, "one", validCompose)
	second := seedStack(t, org.ProjectID, "two", validCompose)
	seedStack(t, testdb.SeedOrg(t).ProjectID, "other-org", validCompose)

	rec = doJSON(t, router, http.MethodGet, "/api/v1/stacks", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2 (%s)", len(resp.Items), rec.Body.String())
	}
	if resp.Items[0].ID != second || resp.Items[1].ID != first {
		t.Fatalf("order wrong: %s", rec.Body.String())
	}
}

func TestGetStackFoundMissingAndCrossOrg(t *testing.T) {
	router := newStacksRouter(t, &fakeSwarm{})
	org := testdb.SeedOrg(t)
	id := seedStack(t, org.ProjectID, "main", validCompose)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/stacks/"+id, org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"composeContent":"services:`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		name   string
		path   string
		status int
	}{
		{"unknown id", "00000000-0000-0000-0000-000000000000", http.StatusNotFound},
		{"invalid uuid", "junk", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodGet, "/api/v1/stacks/"+tc.path, org.Headers, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}

	other := testdb.SeedOrg(t)
	rec = doJSON(t, router, http.MethodGet, "/api/v1/stacks/"+id, other.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org status = %d, want 404", rec.Code)
	}
}

func TestUpdateStackRedeploysWithNewCompose(t *testing.T) {
	fake := &fakeSwarm{}
	router := newStacksRouter(t, fake)
	org := testdb.SeedOrg(t)
	id := seedStack(t, org.ProjectID, "upd", validCompose)

	newCompose := "services:\n  api:\n    image: redis:7\n"
	rec := doJSON(t, router, http.MethodPut, "/api/v1/stacks/"+id, org.Headers,
		fmt.Sprintf(`{"composeContent":%q}`, newCompose))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var compose string
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select compose_content from stacks where id=$1::uuid`, id).Scan(&compose); err != nil {
		t.Fatalf("row missing: %v", err)
	}
	if !strings.Contains(compose, "redis:7") {
		t.Fatalf("compose not updated: %q", compose)
	}
	if len(fake.createdServices) != 1 {
		t.Fatalf("redeploy created services = %d, want 1", len(fake.createdServices))
	}

	// Validation failures.
	rec = doJSON(t, router, http.MethodPut, "/api/v1/stacks/"+id, org.Headers, `{broken`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, router, http.MethodPut, "/api/v1/stacks/"+id, org.Headers, `{"composeContent":"   "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty compose status = %d, want 400", rec.Code)
	}
	// Unknown stack.
	rec = doJSON(t, router, http.MethodPut, "/api/v1/stacks/00000000-0000-0000-0000-000000000000",
		org.Headers, fmt.Sprintf(`{"composeContent":%q}`, newCompose))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown stack status = %d, want 404", rec.Code)
	}
	// Parse failure on redeploy.
	rec = doJSON(t, router, http.MethodPut, "/api/v1/stacks/"+id, org.Headers, `{"composeContent":":\nnot: [valid"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "parse compose file") {
		t.Fatalf("parse failure status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeleteStackRemovesLabeledServicesAndRow(t *testing.T) {
	fake := &fakeSwarm{services: []dockerswarm.Service{
		stackService("svc-mine", "delstack"),
		stackService("svc-other", "otherstack"),
	}}
	router := newStacksRouter(t, fake)
	org := testdb.SeedOrg(t)

	// Unknown stack 404s and never touches swarm.
	rec := doJSON(t, router, http.MethodDelete, "/api/v1/stacks/00000000-0000-0000-0000-000000000000", org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown delete status = %d, want 404", rec.Code)
	}
	if len(fake.removedServiceIDs) != 0 {
		t.Fatalf("unknown delete touched swarm: %v", fake.removedServiceIDs)
	}

	id := seedStack(t, org.ProjectID, "delstack", validCompose)
	rec = doJSON(t, router, http.MethodDelete, "/api/v1/stacks/"+id, org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(fake.removedServiceIDs) != 1 || fake.removedServiceIDs[0] != "svc-mine" {
		t.Fatalf("removed = %v, want [svc-mine]", fake.removedServiceIDs)
	}
	if n := testdb.QueryCount(t, `select count(*) from stacks where id=$1::uuid`, id); n != 0 {
		t.Fatal("stack row not deleted")
	}
}

func TestDeleteStackToleratesSwarmListFailure(t *testing.T) {
	fake := &fakeSwarm{listSvcErr: errors.New("swarm down")}
	router := newStacksRouter(t, fake)
	org := testdb.SeedOrg(t)
	id := seedStack(t, org.ProjectID, "tolerant", validCompose)

	rec := doJSON(t, router, http.MethodDelete, "/api/v1/stacks/"+id, org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when list fails", rec.Code)
	}
	if n := testdb.QueryCount(t, `select count(*) from stacks where id=$1::uuid`, id); n != 0 {
		t.Fatal("row must still be deleted when listing fails")
	}
}

func TestDeployStackAcceptedAndParseFailure(t *testing.T) {
	router := newStacksRouter(t, &fakeSwarm{})
	org := testdb.SeedOrg(t)
	id := seedStack(t, org.ProjectID, "redeploy", validCompose)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/stacks/"+id+"/deploy", org.Headers, "")
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "accepted") {
		t.Fatalf("status = %d body=%s, want 202 accepted", rec.Code, rec.Body.String())
	}

	broken := seedStack(t, org.ProjectID, "broken-stack", ":\nnot: [valid")
	rec = doJSON(t, router, http.MethodPost, "/api/v1/stacks/"+broken+"/deploy", org.Headers, "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "parse compose file") {
		t.Fatalf("parse failure status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Unknown stack → deployStackByID select fails → 400.
	rec = doJSON(t, router, http.MethodPost, "/api/v1/stacks/00000000-0000-0000-0000-000000000000/deploy", org.Headers, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown stack status = %d, want 400", rec.Code)
	}
}

func TestScaleStackStartStopRestart(t *testing.T) {
	fake := &fakeSwarm{services: []dockerswarm.Service{
		stackService("svc-a", "scaled"),
		stackService("svc-b", "scaled"),
		stackService("svc-c", "unrelated"),
	}}
	router := newStacksRouter(t, fake)
	org := testdb.SeedOrg(t)
	id := seedStack(t, org.ProjectID, "scaled", validCompose)

	// Stop scales matching services to zero.
	rec := doJSON(t, router, http.MethodPost, "/api/v1/stacks/"+id+"/stop", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stop status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status   string `json:"status"`
		Replicas uint64 `json:"replicas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Replicas != 0 {
		t.Fatalf("replicas = %d, want 0", resp.Replicas)
	}
	if len(fake.updatedServiceIDs) != 2 {
		t.Fatalf("updated services = %v, want both stack services", fake.updatedServiceIDs)
	}

	// Start and restart scale back up to one replica each.
	fake.updatedServiceIDs = nil
	for _, action := range []string{"start", "restart"} {
		rec = doJSON(t, router, http.MethodPost, "/api/v1/stacks/"+id+"/"+action, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", action, rec.Code)
		}
		if len(fake.updatedServiceIDs) != 2 {
			t.Fatalf("%s updated services = %v, want 2", action, fake.updatedServiceIDs)
		}
		fake.updatedServiceIDs = nil
	}

	// Unknown stack → 404 JSON error.
	rec = doJSON(t, router, http.MethodPost, "/api/v1/stacks/00000000-0000-0000-0000-000000000000/start", org.Headers, "")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not_found") {
		t.Fatalf("unknown scale status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestScaleStackSwarmFailuresReturnBadGateway(t *testing.T) {
	cases := []struct {
		name     string
		fake     *fakeSwarm
		stack    string
		action   string
		wantBody string
	}{
		{
			name:     "list services fails",
			fake:     &fakeSwarm{listSvcErr: errors.New("swarm down")},
			stack:    "lister",
			action:   "stop",
			wantBody: "failed to list services",
		},
		{
			name: "update service fails",
			fake: &fakeSwarm{
				updateSvcErr: errors.New("update rejected"),
				services:     []dockerswarm.Service{stackService("svc-x", "updstack")},
			},
			stack:    "updstack",
			action:   "start",
			wantBody: "failed to update stack services",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newStacksRouter(t, tc.fake)
			org := testdb.SeedOrg(t)
			id := seedStack(t, org.ProjectID, tc.stack, validCompose)
			rec := doJSON(t, router, http.MethodPost, "/api/v1/stacks/"+id+"/"+tc.action, org.Headers, "")
			if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("status = %d body=%s, want 502 %q", rec.Code, rec.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestStackRBACRoleGating(t *testing.T) {
	fake := &fakeSwarm{services: []dockerswarm.Service{stackService("svc-rbac", "rbacstack")}}
	router := newStacksRouter(t, fake)
	org := testdb.SeedOrgWithRole(t, rbac.RoleOwner)
	member := org.AddMember(t, rbac.RoleMember)
	id := seedStack(t, org.ProjectID, "rbacstack", validCompose)

	// Members may read and create.
	rec := doJSON(t, router, http.MethodGet, "/api/v1/stacks", member.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("member list status = %d", rec.Code)
	}

	// Only owner/admin may mutate or scale.
	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/stacks/" + id},
		{http.MethodDelete, "/api/v1/stacks/" + id},
		{http.MethodPost, "/api/v1/stacks/" + id + "/deploy"},
		{http.MethodPost, "/api/v1/stacks/" + id + "/start"},
		{http.MethodPost, "/api/v1/stacks/" + id + "/stop"},
		{http.MethodPost, "/api/v1/stacks/" + id + "/restart"},
	} {
		rec := doJSON(t, router, tc.method, tc.path, member.Headers, `{"composeContent":"x"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s member status = %d body=%s, want 403", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestNormalizeStackName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"My Web App", "my-web-app"},
		{"  Trimmed--Name  ", "trimmed-name"},
		{"UPPER_case_99", "upper-case-99"},
		{"!!!", "stack"},
		{"", "stack"},
		{"---", "stack"},
		{strings.Repeat("a", 100), strings.TrimRight(strings.Repeat("a", 48), "-")},
		{strings.Repeat("a", 47) + "-", strings.Repeat("a", 47)},
		// Trimming to 48 chars can still leave only separators.
		{strings.Repeat("!", 100), "stack"},
	}
	for _, tc := range cases {
		if got := normalizeStackName(tc.in); got != tc.want {
			t.Errorf("normalizeStackName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStacksListCreateGetRequireAuthentication(t *testing.T) {
	router := newStacksRouter(t, &fakeSwarm{})
	testdb.SeedOrg(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/stacks"},
		{http.MethodPost, "/api/v1/stacks"},
		{http.MethodGet, "/api/v1/stacks/00000000-0000-0000-0000-000000000000"},
	} {
		rec := doJSON(t, router, tc.method, tc.path, http.Header{}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s unauthenticated status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// newStacksRouterOnPool wires the routes against an arbitrary pool without
// resetting tables, so DDL-based failure injection keeps its seeded rows.
func newStacksRouterOnPool(t *testing.T, pool *pgxpool.Pool, fake *fakeSwarm) http.Handler {
	t.Helper()
	h := NewHandler(pool, fake)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/stacks", h.ListStacks)
		gr.Put("/api/v1/stacks/{id}", h.UpdateStack)
	})
	return r
}

// TestListStacksDBFailures injects schema breakage for the list handler's
// query-error and row-scan-error branches on a fresh (uncached) pool.
func TestListStacksDBFailures(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	id := seedStack(t, org.ProjectID, "brokenlist", validCompose)

	cfg, err := pgxpool.ParseConfig(pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatalf("parse conn string: %v", err)
	}

	t.Run("query failure", func(t *testing.T) {
		if _, err := pool.Exec(context.Background(), `alter table stacks drop column created_at`); err != nil {
			t.Fatalf("drop: %v", err)
		}
		fresh, err := pgxpool.NewWithConfig(context.Background(), cfg)
		if err != nil {
			t.Fatalf("open pool: %v", err)
		}
		defer fresh.Close()
		router := newStacksRouterOnPool(t, fresh, &fakeSwarm{})
		rec := doJSON(t, router, http.MethodGet, "/api/v1/stacks", org.Headers, "")
		if _, err := pool.Exec(context.Background(),
			`alter table stacks add column created_at timestamptz not null default now()`); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("query failure status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})

	t.Run("scan failure", func(t *testing.T) {
		if _, err := pool.Exec(context.Background(),
			`alter table stacks alter column created_at drop default,
			 alter column created_at type text[] using array['x']`); err != nil {
			t.Fatalf("alter: %v", err)
		}
		fresh, err := pgxpool.NewWithConfig(context.Background(), cfg)
		if err != nil {
			t.Fatalf("open pool: %v", err)
		}
		defer fresh.Close()
		router := newStacksRouterOnPool(t, fresh, &fakeSwarm{})
		rec := doJSON(t, router, http.MethodGet, "/api/v1/stacks", org.Headers, "")
		if _, err := pool.Exec(context.Background(),
			`alter table stacks alter column created_at type timestamptz using now(),
			 alter column created_at set default now()`); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("scan failure status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})
	_ = id
}

// TestUpdateStackExecFailure renames the backing table so the UPDATE fails at
// prepare time on a fresh (uncached) pool.
func TestUpdateStackExecFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	id := seedStack(t, org.ProjectID, "brokenupd", validCompose)

	if _, err := pool.Exec(context.Background(), `alter table stacks rename to stacks_gone`); err != nil {
		t.Fatalf("rename: %v", err)
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
	router := newStacksRouterOnPool(t, fresh, &fakeSwarm{})
	rec := doJSON(t, router, http.MethodPut, "/api/v1/stacks/"+id, org.Headers,
		fmt.Sprintf(`{"composeContent":%q}`, validCompose))
	if _, err := pool.Exec(context.Background(), `alter table stacks_gone rename to stacks`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update exec failure status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}
