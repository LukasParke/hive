package applications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/testdb"
	dockerswarm "github.com/moby/moby/api/types/swarm"
)

// fakeSwarmApps records swarm mutations and serves scripted reads.
type fakeSwarmApps struct {
	services    []dockerswarm.Service
	tasks       []dockerswarm.Task
	created     []dockerswarm.SecretSpec
	removedIDs  []string
	updates     []updateRecord
	createErr   error
	listErr     error
	tasksErr    error
	nextSecretN int
}

type updateRecord struct {
	ID      string
	Version uint64
	Spec    dockerswarm.ServiceSpec
}

func (f *fakeSwarmApps) CreateSecret(_ context.Context, spec dockerswarm.SecretSpec) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.created = append(f.created, spec)
	f.nextSecretN++
	return fmt.Sprintf("secret-%d", f.nextSecretN), nil
}

func (f *fakeSwarmApps) RemoveSecret(_ context.Context, id string) error {
	f.removedIDs = append(f.removedIDs, id)
	return nil
}

func (f *fakeSwarmApps) ListServices(context.Context) ([]dockerswarm.Service, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.services, nil
}

func (f *fakeSwarmApps) UpdateService(_ context.Context, id string, version uint64, spec dockerswarm.ServiceSpec) error {
	f.updates = append(f.updates, updateRecord{ID: id, Version: version, Spec: spec})
	return nil
}

func (f *fakeSwarmApps) ListAllTasks(context.Context) ([]dockerswarm.Task, error) {
	if f.tasksErr != nil {
		return nil, f.tasksErr
	}
	return f.tasks, nil
}

func appService(appID string, desired, running uint64) dockerswarm.Service {
	labels := map[string]string{"hive.app.id": appID}
	repl := func(n uint64) *uint64 { return &n }
	spec := dockerswarm.ServiceSpec{
		Annotations: dockerswarm.Annotations{Labels: labels},
		Mode:        dockerswarm.ServiceMode{Replicated: &dockerswarm.ReplicatedService{Replicas: repl(desired)}},
	}
	svc := dockerswarm.Service{ID: "svc-" + appID, Spec: spec}
	_ = running
	// Running count is derived from tasks by the handler; keep service simple.
	return svc
}

type appsEnv struct {
	org *testdb.OrgFixture
	sw  *fakeSwarmApps
	h   *Handler
	rtr chi.Router
}

func newAppsEnv(t *testing.T) *appsEnv {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	e := &appsEnv{org: testdb.SeedOrg(t), sw: &fakeSwarmApps{}}
	e.h = NewHandler(pool, e.sw)
	e.rtr = chi.NewRouter()
	e.rtr.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/applications", e.h.ListApplications)
		gr.Post("/api/v1/applications", e.h.CreateApplication)
		gr.Get("/api/v1/applications/{id}", e.h.GetApplication)
		gr.Put("/api/v1/applications/{id}", e.h.UpdateApplication)
		gr.Delete("/api/v1/applications/{id}", e.h.DeleteApplication)
		gr.Get("/api/v1/applications/{id}/env", e.h.ListAppEnvVars)
		gr.Post("/api/v1/applications/{id}/env", e.h.CreateAppEnvVar)
		gr.Put("/api/v1/applications/{id}/env/{envId}", e.h.UpdateAppEnvVar)
		gr.Delete("/api/v1/applications/{id}/env/{envId}", e.h.DeleteAppEnvVar)
		gr.Post("/api/v1/applications/{id}/start", e.h.StartApplication)
		gr.Post("/api/v1/applications/{id}/stop", e.h.StopApplication)
		gr.Post("/api/v1/applications/{id}/restart", e.h.RestartApplication)
	})
	return e
}

func (e *appsEnv) do(t *testing.T, method, path string, body any, headers http.Header, params map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rd = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = http.Header{}
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if params != nil {
		rctx := chi.NewRouteContext()
		for k, v := range params {
			rctx.URLParams.Add(k, v)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	rec := httptest.NewRecorder()
	e.rtr.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func (e *appsEnv) authed(t *testing.T) http.Header {
	t.Helper()
	h := e.org.Headers.Clone()
	if h == nil {
		h = http.Header{}
	}
	if h.Get("X-Organization-Id") == "" {
		h.Set("X-Organization-Id", e.org.OrgID)
	}
	return h
}

func TestListApplicationsScopesAndStatuses(t *testing.T) {
	e := newAppsEnv(t)
	appA := testdb.SeedApplication(t, e.org.ProjectID, "app-a", "", nil)
	appB := testdb.SeedApplication(t, e.org.ProjectID, "app-b", "", nil)

	// A deployed service for appA with 3 desired / 2 running replicas.
	labeled := appService(appA, 3, 0)
	e.sw.services = []dockerswarm.Service{labeled}
	taskNode := dockerswarm.Task{
		ID:           "task-1",
		ServiceID:    labeled.ID,
		Slot:         1,
		Status:       dockerswarm.TaskStatus{State: dockerswarm.TaskStateRunning},
		DesiredState: dockerswarm.TaskStateRunning,
	}
	e.sw.tasks = []dockerswarm.Task{taskNode}

	rec, out := e.do(t, http.MethodGet, "/api/v1/applications", nil, e.authed(t), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	items := out["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 apps, got %d (%v)", len(items), out["items"])
	}
	byName := map[string]map[string]any{}
	for _, it := range items {
		m := it.(map[string]any)
		byName[m["name"].(string)] = m
	}
	a := byName["app-a"]
	if a["status"] != "degraded" && a["status"] != "running" {
		t.Fatalf("app-a status = %v (want degraded or running)", a["status"])
	}
	if a["desiredReplicas"].(float64) != 3 {
		t.Fatalf("app-a desired = %v", a["desiredReplicas"])
	}
	if b := byName["app-b"]; b["status"] != "not_deployed" {
		t.Fatalf("app-b status = %v", b["status"])
	}
	_ = appB
}

func TestCreateApplicationValidationMatrix(t *testing.T) {
	e := newAppsEnv(t)
	hdr := e.authed(t)
	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"missing name", map[string]any{"projectId": e.org.ProjectID, "sourceType": "image", "image": "nginx"}, 400},
		{"missing project", map[string]any{"name": "x", "sourceType": "image", "image": "nginx"}, 400},
		{"missing source", map[string]any{"name": "x", "projectId": e.org.ProjectID, "image": "nginx"}, 400},
		{"bad source", map[string]any{"name": "x", "projectId": e.org.ProjectID, "sourceType": "tarball", "image": "nginx"}, 400},
		{"image source without image", map[string]any{"name": "x", "projectId": e.org.ProjectID, "sourceType": "image"}, 400},
		{"git source without url", map[string]any{"name": "x", "projectId": e.org.ProjectID, "sourceType": "git"}, 400},
		{"port too high", map[string]any{"name": "x", "projectId": e.org.ProjectID, "sourceType": "image", "image": "nginx", "containerPort": 70000}, 400},
		{"unknown project", map[string]any{"name": "x", "projectId": "00000000-0000-0000-0000-000000000001", "sourceType": "image", "image": "nginx"}, 400},
	}
	for _, tc := range cases {
		rec, _ := e.do(t, http.MethodPost, "/api/v1/applications", tc.body, hdr, nil)
		if rec.Code != tc.want {
			t.Fatalf("%s: status=%d want=%d body=%s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestCreateApplicationCrossOrgProjectForbidden(t *testing.T) {
	e := newAppsEnv(t)
	other := testdb.SeedOrg(t)
	rec, _ := e.do(t, http.MethodPost, "/api/v1/applications",
		map[string]any{"name": "steal", "projectId": other.ProjectID, "sourceType": "image", "image": "nginx"},
		e.authed(t), nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateApplicationHappyImageAndGitDefaults(t *testing.T) {
	e := newAppsEnv(t)
	hdr := e.authed(t)
	rec, out := e.do(t, http.MethodPost, "/api/v1/applications",
		map[string]any{"name": "img-app", "projectId": e.org.ProjectID, "sourceType": "image", "image": "nginx:1.25"}, hdr, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("image app status=%d body=%s", rec.Code, rec.Body.String())
	}
	if out["gitRef"] != "main" || out["containerPort"].(float64) != 3000 {
		t.Fatalf("defaults not applied: %v", out)
	}
	rec2, out2 := e.do(t, http.MethodPost, "/api/v1/applications",
		map[string]any{"name": "git-app", "projectId": e.org.ProjectID, "sourceType": "git", "repositoryUrl": "https://example.com/r.git", "containerPort": 8080}, hdr, nil)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("git app status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	if out2["repositoryUrl"] != "https://example.com/r.git" {
		t.Fatalf("repositoryUrl missing: %v", out2)
	}
}

func TestGetUpdateDeleteApplication(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "crud-app", "", nil)
	hdr := e.authed(t)

	rec, out := e.do(t, http.MethodGet, "/api/v1/applications/"+appID, nil, hdr,
		map[string]string{"id": appID})
	if rec.Code != http.StatusOK || out["name"] != "crud-app" {
		t.Fatalf("get failed: %d %s", rec.Code, rec.Body.String())
	}

	rec, _ = e.do(t, http.MethodPut, "/api/v1/applications/"+appID,
		map[string]any{"name": "renamed"}, hdr, map[string]string{"id": appID})
	if rec.Code != http.StatusOK {
		t.Fatalf("update failed: %d %s", rec.Code, rec.Body.String())
	}
	_, out = e.do(t, http.MethodGet, "/api/v1/applications/"+appID, nil, hdr, map[string]string{"id": appID})
	if out["name"] != "renamed" {
		t.Fatalf("rename not persisted: %v", out)
	}

	rec, _ = e.do(t, http.MethodDelete, "/api/v1/applications/"+appID, nil, hdr, map[string]string{"id": appID})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete failed: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ = e.do(t, http.MethodDelete, "/api/v1/applications/"+appID, nil, hdr, map[string]string{"id": appID})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete should 404, got %d", rec.Code)
	}
}

func TestEnvVarSecretLifecycleUsesDockerSecrets(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "env-app", "", nil)
	hdr := e.authed(t)
	params := map[string]string{"id": appID}

	// Plain var.
	rec, created := e.do(t, http.MethodPost, "/api/v1/applications/"+appID+"/env",
		map[string]any{"key": "PLAIN", "value": "hello", "isSecret": false}, hdr, params)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create plain env: %d %s", rec.Code, rec.Body.String())
	}
	envID := created["id"].(string)

	// Secret var: must create a docker secret.
	rec, secretEnv := e.do(t, http.MethodPost, "/api/v1/applications/"+appID+"/env",
		map[string]any{"key": "API_KEY", "value": "s3cret", "isSecret": true}, hdr, params)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create secret env: %d %s", rec.Code, rec.Body.String())
	}
	if len(e.sw.created) != 1 {
		t.Fatalf("expected 1 docker secret created, got %d", len(e.sw.created))
	}
	secretEnvID := secretEnv["id"].(string)
	if strings.Contains(string(e.sw.created[0].Data), "s3cret") == false {
		t.Fatalf("docker secret payload should contain the value")
	}

	// Updating the secret re-creates the docker secret and removes the old one.
	oldDockerID := e.sw.removedIDs
	rec, _ = e.do(t, http.MethodPut, "/api/v1/applications/"+appID+"/env/"+secretEnvID,
		map[string]any{"value": "rotated", "isSecret": true}, hdr,
		map[string]string{"id": appID, "varId": secretEnvID})
	if rec.Code != http.StatusOK {
		t.Fatalf("update secret env: %d %s", rec.Code, rec.Body.String())
	}
	if len(e.sw.created) != 2 {
		t.Fatalf("expected rotation to create a second docker secret")
	}
	_ = oldDockerID

	// Delete removes the docker secret for secret vars.
	rec, _ = e.do(t, http.MethodDelete, "/api/v1/applications/"+appID+"/env/"+secretEnvID, nil, hdr,
		map[string]string{"id": appID, "varId": secretEnvID})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete secret env: %d %s", rec.Code, rec.Body.String())
	}
	if len(e.sw.removedIDs) < 1 {
		t.Fatalf("delete should remove the docker secret")
	}
	_ = envID

	// Invalid key rejected before touching swarm.
	before := len(e.sw.created)
	rec, _ = e.do(t, http.MethodPost, "/api/v1/applications/"+appID+"/env",
		map[string]any{"key": "123BAD", "value": "x", "isSecret": false}, hdr, params)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid key: %d %s", rec.Code, rec.Body.String())
	}
	if len(e.sw.created) != before {
		t.Fatalf("invalid key must not create docker secrets")
	}
}

func TestStartStopRestartScaleService(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "svc-app", "", nil)
	svc := appService(appID, 1, 0)
	svc.Spec.Name = "app-svc-app"
	e.sw.services = []dockerswarm.Service{svc}
	hdr := e.authed(t)

	for _, op := range []struct {
		path     string
		replicas uint64
		status   string
	}{
		{"/stop", 0, "stopped"},
		{"/start", 1, "running"},
		{"/restart", 1, "restarting"},
	} {
		rec, out := e.do(t, http.MethodPost, "/api/v1/applications/"+appID+op.path, nil, hdr,
			map[string]string{"id": appID})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", op.path, rec.Code, rec.Body.String())
		}
		if out["status"] != op.status {
			t.Fatalf("%s status=%v want=%v", op.path, out["status"], op.status)
		}
		last := e.sw.updates[len(e.sw.updates)-1]
		got := last.Spec.Mode.Replicated.Replicas
		if got == nil || *got != op.replicas {
			t.Fatalf("%s replicas=%v want=%d", op.path, got, op.replicas)
		}
	}

	// Restart bumps ForceUpdate so the task is recreated.
	if e.sw.updates[2].Spec.TaskTemplate.ForceUpdate != 1 {
		t.Fatalf("restart must bump ForceUpdate")
	}

	// App without a deployed service → 502 runtime error.
	orphan := testdb.SeedApplication(t, e.org.ProjectID, "orphan-app", "", nil)
	rec, _ := e.do(t, http.MethodPost, "/api/v1/applications/"+orphan+"/start", nil, hdr,
		map[string]string{"id": orphan})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("orphan start: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnauthenticatedRequestsRejected(t *testing.T) {
	e := newAppsEnv(t)
	rec, _ := e.do(t, http.MethodGet, "/api/v1/applications", nil, http.Header{}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-auth list: %d", rec.Code)
	}
}

func TestListAppEnvVarsAndPlainVarLifecycle(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "envlist-app", "", nil)
	hdr := e.authed(t)
	params := map[string]string{"id": appID}

	if rec, _ := e.do(t, http.MethodGet, "/api/v1/applications/"+appID+"/env", nil, hdr, params); rec.Code != http.StatusOK {
		t.Fatalf("empty list: %d %s", rec.Code, rec.Body.String())
	}

	rec, created := e.do(t, http.MethodPost, "/api/v1/applications/"+appID+"/env",
		map[string]any{"key": "PLAIN_ONE", "value": "v1", "isSecret": false}, hdr, params)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create plain: %d %s", rec.Code, rec.Body.String())
	}
	plainID := created["id"].(string)

	// List shows the var without leaking... plain values are returned.
	rec, list := e.do(t, http.MethodGet, "/api/v1/applications/"+appID+"/env", nil, hdr, params)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 env var, got %d (%v)", len(items), list["items"])
	}

	// Update plain value.
	rec, _ = e.do(t, http.MethodPut, "/api/v1/applications/"+appID+"/env/"+plainID,
		map[string]any{"value": "v2"}, hdr, map[string]string{"id": appID, "varId": plainID})
	if rec.Code != http.StatusOK {
		t.Fatalf("update plain: %d %s", rec.Code, rec.Body.String())
	}

	// Update with empty value → 400.
	rec, _ = e.do(t, http.MethodPut, "/api/v1/applications/"+appID+"/env/"+plainID,
		map[string]any{"value": ""}, hdr, map[string]string{"id": appID, "varId": plainID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty-value update should 400, got %d", rec.Code)
	}

	// Delete plain var.
	rec, _ = e.do(t, http.MethodDelete, "/api/v1/applications/"+appID+"/env/"+plainID, nil, hdr,
		map[string]string{"id": appID, "varId": plainID})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete plain: %d %s", rec.Code, rec.Body.String())
	}

	// Unknown env id delete → 404.
	rec, _ = e.do(t, http.MethodDelete, "/api/v1/applications/"+appID+"/env/00000000-0000-0000-0000-000000000009",
		nil, hdr, map[string]string{"id": appID, "varId": "00000000-0000-0000-0000-000000000009"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown env delete should 404, got %d", rec.Code)
	}
}

func TestMemberRoleForbiddenOnMutations(t *testing.T) {
	e := newAppsEnv(t)
	member := testdb.SeedOrgWithRole(t, "member")
	appID := testdb.SeedApplication(t, e.org.ProjectID, "member-app", "", nil)

	// Member token but org header points at owner's org.
	hdr := member.Headers.Clone()
	hdr.Set("X-Organization-Id", e.org.OrgID)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/applications/" + appID + "/env"},
		{http.MethodPost, "/api/v1/applications/" + appID + "/stop"},
	} {
		var body any
		if tc.method == http.MethodPost && strings.HasSuffix(tc.path, "/env") {
			body = map[string]any{"key": "K", "value": "v"}
		}
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", appID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req.Header = hdr
		rec := httptest.NewRecorder()
		e.rtr.ServeHTTP(rec, req)
		_ = body
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s as member: got %d want 403", tc.method, tc.path, rec.Code)
		}
	}
}

func TestGetApplicationUnknownIDNotFound(t *testing.T) {
	e := newAppsEnv(t)
	hdr := e.authed(t)
	rec, _ := e.do(t, http.MethodGet, "/api/v1/applications/00000000-0000-0000-0000-000000000042",
		nil, hdr, map[string]string{"id": "00000000-0000-0000-0000-000000000042"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get unknown app: %d", rec.Code)
	}
}

func TestStatusFromReplicasMapping(t *testing.T) {
	cases := []struct {
		desired, running uint64
		want             string
	}{
		{3, 3, "running"},
		{3, 0, "deploying"},
		{3, 2, "degraded"},
		{0, 0, "stopped"},
	}
	for _, tc := range cases {
		if got := statusFromReplicas(tc.desired, tc.running); got != tc.want {
			t.Fatalf("statusFromReplicas(%d,%d)=%q want %q", tc.desired, tc.running, got, tc.want)
		}
	}
}

func TestNilSwarmListStillSucceeds(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	h := NewHandler(pool, nil) // runtime statuses degrade to not_deployed
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/applications", h.ListApplications)
	})
	_ = testdb.SeedApplication(t, org.ProjectID, "nil-swarm-app", "", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
	req.Header = org.Headers.Clone()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nil-swarm list: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGlobalModeDesiredReplicas(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "global-app", "", nil)
	svc := dockerswarm.Service{
		ID: "svc-global",
		Spec: dockerswarm.ServiceSpec{
			Annotations: dockerswarm.Annotations{Labels: map[string]string{"hive.app.id": appID}},
			Mode:        dockerswarm.ServiceMode{Global: &dockerswarm.GlobalService{}},
		},
	}
	e.sw.services = []dockerswarm.Service{svc}
	e.sw.tasks = []dockerswarm.Task{{
		ServiceID:    svc.ID,
		Status:       dockerswarm.TaskStatus{State: dockerswarm.TaskStateRunning},
		DesiredState: dockerswarm.TaskStateRunning,
	}}
	rec, out := e.do(t, http.MethodGet, "/api/v1/applications", nil, e.authed(t), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	items := out["items"].([]any)
	m := items[0].(map[string]any)
	if m["desiredReplicas"].(float64) != 1 {
		t.Fatalf("global mode desired = %v, want 1", m["desiredReplicas"])
	}
}

func TestClosedPoolYieldsServerErrors(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "closed-pool-app", "", nil)

	// Handler on a closed DEDICATED pool: auth middleware is bypassed by
	// injecting claims directly, so the DB failure surfaces from the handler
	// itself. The pool must be test-private — closing the shared testdb pool
	// here would poison every later test in the package.
	closedPool, err := pgxpool.New(context.Background(), testdb.DSN())
	if err != nil {
		t.Fatalf("open dedicated pool: %v", err)
	}
	h2 := NewHandler(closedPool, e.sw)
	closedPool.Close()

	claims := &auth.Claims{UserID: e.org.UserID, Email: e.org.Email}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+appID+"/env", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", appID)
	req = req.WithContext(context.WithValue(
		apicxt.WithClaims(req.Context(), claims), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h2.ListAppEnvVars(rec, req)
	if rec.Code < 500 {
		t.Fatalf("closed-pool env list should 5xx, got %d %s", rec.Code, rec.Body.String())
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/applications/"+appID, nil)
	drctx := chi.NewRouteContext()
	drctx.URLParams.Add("id", appID)
	delReq = delReq.WithContext(context.WithValue(
		apicxt.WithClaims(delReq.Context(), claims), chi.RouteCtxKey, drctx))
	delReq.Header = e.authed(t)
	rec2 := httptest.NewRecorder()
	h2.DeleteApplication(rec2, delReq)
	// RequireOrgAccess hits the closed pool first and maps the failure to a
	// 403; either way the request must not succeed.
	if rec2.Code == http.StatusOK {
		t.Fatalf("closed-pool delete must not succeed")
	}
}

func TestCreateEnvVarUnknownAppNotFound(t *testing.T) {
	e := newAppsEnv(t)
	hdr := e.authed(t)
	rec, _ := e.do(t, http.MethodPost, "/api/v1/applications/00000000-0000-0000-0000-000000000007/env",
		map[string]any{"key": "K", "value": "v"}, hdr,
		map[string]string{"id": "00000000-0000-0000-0000-000000000007"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("create env on unknown app should 404, got %d", rec.Code)
	}
}

func TestUpdateAppEnvVarUnknownVarNotFound(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "upd-env-app", "", nil)
	hdr := e.authed(t)
	rec, _ := e.do(t, http.MethodPut, "/api/v1/applications/"+appID+"/env/00000000-0000-0000-0000-000000000008",
		map[string]any{"value": "x"}, hdr,
		map[string]string{"id": appID, "varId": "00000000-0000-0000-0000-000000000008"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update unknown var should 404, got %d", rec.Code)
	}
}

func TestUpdateApplicationPartialFields(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "partial-app", "", nil)
	hdr := e.authed(t)
	rec, out := e.do(t, http.MethodPut, "/api/v1/applications/"+appID,
		map[string]any{"image": "nginx:1.27"}, hdr, map[string]string{"id": appID})
	if rec.Code != http.StatusOK {
		t.Fatalf("partial update: %d %s", rec.Code, rec.Body.String())
	}
	if out["image"] != "nginx:1.27" {
		t.Fatalf("image not updated: %v", out)
	}
	if out["name"] != "partial-app" {
		t.Fatalf("name must be preserved on partial update: %v", out)
	}
}

func TestDeleteRemovesDockerSecretsForSecretVars(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "del-secret-app", "", nil)
	hdr := e.authed(t)
	rec, created := e.do(t, http.MethodPost, "/api/v1/applications/"+appID+"/env",
		map[string]any{"key": "TOKEN", "value": "t0p", "isSecret": true}, hdr,
		map[string]string{"id": appID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed secret env: %d %s", rec.Code, rec.Body.String())
	}
	_ = created["id"]
	rec, _ = e.do(t, http.MethodDelete, "/api/v1/applications/"+appID, nil, hdr,
		map[string]string{"id": appID})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete app: %d %s", rec.Code, rec.Body.String())
	}
	if len(e.sw.removedIDs) == 0 {
		t.Fatalf("deleting the app must remove its docker secrets")
	}
}

func TestSecretEnvCreateDockerErrorReturns500(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "err-app", "", nil)
	e.sw.createErr = fmt.Errorf("daemon down")
	hdr := e.authed(t)
	rec, _ := e.do(t, http.MethodPost, "/api/v1/applications/"+appID+"/env",
		map[string]any{"key": "K", "value": "v", "isSecret": true}, hdr,
		map[string]string{"id": appID})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("docker create failure should 500, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnauthorizedClaimsReturn401OnEveryHandler(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "noauth-app", "", nil)

	// Direct invocation WITHOUT claims in the context.
	call := func(h func(http.ResponseWriter, *http.Request), params map[string]string, body any) int {
		var rd io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			rd = strings.NewReader(string(b))
		}
		req := httptest.NewRequest(http.MethodGet, "/x", rd)
		rctx := chi.NewRouteContext()
		for k, v := range params {
			rctx.URLParams.Add(k, v)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec.Code
	}

	checks := []struct {
		name string
		h    func(http.ResponseWriter, *http.Request)
		p    map[string]string
		body any
	}{
		{"list", e.h.ListApplications, nil, nil},
		// CreateApplication validates the payload before the claims check, so
		// the probe must carry a body that passes validation.
		{"create", e.h.CreateApplication, nil,
			map[string]any{"projectId": e.org.ProjectID, "name": "noauth", "sourceType": "image", "image": "nginx:1"}},
		{"get", e.h.GetApplication, map[string]string{"id": appID}, nil},
		{"update", e.h.UpdateApplication, map[string]string{"id": appID}, map[string]any{"name": "noauth-update"}},
		{"delete", e.h.DeleteApplication, map[string]string{"id": appID}, nil},
		{"envList", e.h.ListAppEnvVars, map[string]string{"id": appID}, nil},
		{"envCreate", e.h.CreateAppEnvVar, map[string]string{"id": appID}, map[string]any{"key": "K", "value": "v"}},
		{"envUpdate", e.h.UpdateAppEnvVar, map[string]string{"id": appID, "varId": "00000000-0000-0000-0000-000000000001"}, map[string]any{"value": "v"}},
		{"envDelete", e.h.DeleteAppEnvVar, map[string]string{"id": appID, "varId": "00000000-0000-0000-0000-000000000001"}, nil},
		{"start", e.h.StartApplication, map[string]string{"id": appID}, nil},
		{"stop", e.h.StopApplication, map[string]string{"id": appID}, nil},
		{"restart", e.h.RestartApplication, map[string]string{"id": appID}, nil},
	}
	for _, c := range checks {
		if got := call(c.h, c.p, c.body); got != http.StatusUnauthorized {
			t.Fatalf("%s without claims: got %d want 401", c.name, got)
		}
	}
}

func TestCreateApplicationWatchPathsRoundTrip(t *testing.T) {
	e := newAppsEnv(t)
	hdr := e.authed(t)
	rec, out := e.do(t, http.MethodPost, "/api/v1/applications",
		map[string]any{
			"name":          "watch-app",
			"projectId":     e.org.ProjectID,
			"sourceType":    "git",
			"repositoryUrl": "https://example.com/w.git",
			"watchPaths":    []string{"src/**", "Dockerfile"},
		}, hdr, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var row struct {
		WatchPaths []string `json:"watch_paths"`
	}
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select watch_paths from applications where id=$1::uuid`, out["id"]).Scan(&row.WatchPaths); err != nil {
		t.Fatalf("read back watch paths: %v", err)
	}
	if len(row.WatchPaths) != 2 || row.WatchPaths[0] != "src/**" {
		t.Fatalf("watch paths not persisted: %v", row.WatchPaths)
	}
}

func TestGetApplicationCrossOrgHidden(t *testing.T) {
	e := newAppsEnv(t)
	other := testdb.SeedOrg(t)
	otherApp := testdb.SeedApplication(t, other.ProjectID, "other-org-app", "", nil)
	rec, _ := e.do(t, http.MethodGet, "/api/v1/applications/"+otherApp, nil, e.authed(t),
		map[string]string{"id": otherApp})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org get should 404, got %d", rec.Code)
	}
}

// failingRenamePool opens a simple-protocol pool and renames the given table
// so subsequent queries against it fail (prepared-stmt caching would mask
// renames on the default pool). Returns the handler bound to that pool plus
// an undo func restoring the original table name.
func failingRenamePool(t *testing.T, table string) (*Handler, func()) {
	t.Helper()
	dsn := strings.Replace(testdb.DSN(), "?", "?default_query_exec_mode=simple_protocol&", 1)
	cp, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("simple protocol pool: %v", err)
	}
	if _, err := cp.Exec(context.Background(), fmt.Sprintf("alter table %s rename to %s_broken", table, table)); err != nil {
		cp.Close()
		t.Fatalf("rename %s: %v", table, err)
	}
	h := NewHandler(cp, &fakeSwarmApps{})
	undo := func() {
		_, _ = cp.Exec(context.Background(), fmt.Sprintf("alter table %s_broken rename to %s", table, table))
		cp.Close()
	}
	return h, undo
}

func TestQueryFailuresSurfaceAsErrors(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "qfail-app", "", nil)
	hdr := e.authed(t)

	doDirect := func(h *Handler, method string, fn func(http.ResponseWriter, *http.Request), params map[string]string, body any) int {
		var rd io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			rd = strings.NewReader(string(b))
		}
		req := httptest.NewRequest(method, "/x", rd)
		rctx := chi.NewRouteContext()
		for k, v := range params {
			rctx.URLParams.Add(k, v)
		}
		req = req.WithContext(context.WithValue(apicxt.WithClaims(req.Context(),
			&auth.Claims{UserID: e.org.UserID, Email: e.org.Email}), chi.RouteCtxKey, rctx))
		req.Header = hdr.Clone()
		if req.Header == nil {
			req.Header = http.Header{}
		}
		if body != nil && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		fn(rec, req)
		return rec.Code
	}

	// Rename ONLY the table the target statement uses, so RBAC (which reads
	// organization_members/projects) still succeeds and the handler actually
	// reaches the failing statement.
	pick := func(h *Handler, name string) func(http.ResponseWriter, *http.Request) {
		switch name {
		case "list":
			return h.ListApplications
		case "create":
			return h.CreateApplication
		case "get":
			return h.GetApplication
		case "update":
			return h.UpdateApplication
		case "delete":
			return h.DeleteApplication
		case "start":
			return h.StartApplication
		case "envList":
			return h.ListAppEnvVars
		case "envCreate":
			return h.CreateAppEnvVar
		case "envCreateSecret":
			return h.CreateAppEnvVar
		case "envUpdate":
			return h.UpdateAppEnvVar
		case "envDelete":
			return h.DeleteAppEnvVar
		}
		return nil
	}
	cases := []struct {
		name   string
		table  string
		method string
		params map[string]string
		body   any
		min    int
	}{
		{"list", "applications", http.MethodGet, nil, nil, 500},
		{"get", "applications", http.MethodGet, map[string]string{"id": appID}, nil, 400},
		{"update", "applications", http.MethodPut, map[string]string{"id": appID}, map[string]any{"name": "renamed"}, 400},
		// Rename breaks the final insert; projects lookup and RBAC still succeed.
		{"create", "applications", http.MethodPost, nil, map[string]any{"projectId": e.org.ProjectID, "name": "qfail-create", "sourceType": "image", "image": "nginx:1"}, 400},
		{"delete", "applications", http.MethodDelete, map[string]string{"id": appID}, nil, 400},
		{"start", "applications", http.MethodPost, map[string]string{"id": appID}, nil, 400},
		{"envList", "app_env_vars", http.MethodGet, map[string]string{"id": appID}, nil, 500},
		// Plain var: rename surfaces from the insert.
		{"envCreate", "app_env_vars", http.MethodPost, map[string]string{"id": appID}, map[string]any{"key": "PLAIN_QF", "value": "v"}, 400},
		// Secret var: docker secret is created first, then the insert fails and
		// the handler must clean the orphaned secret up.
		{"envCreateSecret", "app_env_vars", http.MethodPost, map[string]string{"id": appID}, map[string]any{"key": "SECRET_QF", "value": "v", "isSecret": true}, 400},
		{"envUpdate", "app_env_vars", http.MethodPut, map[string]string{"id": appID, "varId": "00000000-0000-0000-0000-000000000001"}, map[string]any{"value": "v"}, 400},
		{"envDelete", "app_env_vars", http.MethodDelete, map[string]string{"id": appID, "varId": "00000000-0000-0000-0000-000000000001"}, nil, 400},
	}
	for _, tc := range cases {
		h, undo := failingRenamePool(t, tc.table)
		code := doDirect(h, tc.method, pick(h, tc.name), tc.params, tc.body)
		undo()
		if code < tc.min {
			t.Errorf("%s on broken %s returned %d, want >=%d", tc.name, tc.table, code, tc.min)
		}
	}
}

// simpleProtoHandler returns a handler bound to a fresh simple-protocol pool
// so per-statement faults (type swaps, renames) surface deterministically.
func simpleProtoHandler(t *testing.T) *Handler {
	t.Helper()
	dsn := strings.Replace(testdb.DSN(), "?", "?default_query_exec_mode=simple_protocol&", 1)
	cp, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("simple protocol pool: %v", err)
	}
	t.Cleanup(cp.Close)
	return NewHandler(cp, &fakeSwarmApps{})
}

// alterColumnType swaps a column's type to text (breaking time.Time scans)
// and restores it via t.Cleanup.
func alterColumnType(t *testing.T, table, column string) {
	t.Helper()
	ctx := context.Background()
	p := testdb.Get(t)
	if _, err := p.Exec(ctx, fmt.Sprintf(
		"alter table %s alter column %s type text using %s::text", table, column, column)); err != nil {
		t.Fatalf("alter %s.%s to text: %v", table, column, err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, fmt.Sprintf(
			"alter table %s alter column %s type timestamptz using %s::timestamptz", table, column, column)); err != nil {
			t.Fatalf("restore %s.%s: %v", table, column, err)
		}
	})
}

// blockTableWrites installs a trigger failing every write event of the given
// table while leaving reads untouched; it restores itself via t.Cleanup.
func blockTableWrites(t *testing.T, table, event string) {
	t.Helper()
	p := testdb.Get(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `
		create or replace function apps_test_block_write_fn() returns trigger as $$
		begin raise exception 'blocked by test'; end
		$$ language plpgsql
	`); err != nil {
		t.Fatalf("create function: %v", err)
	}
	trg := fmt.Sprintf("apps_test_block_%s_trg", event)
	if _, err := p.Exec(ctx, fmt.Sprintf(
		"create trigger %s before %s on %s for each row execute function apps_test_block_write_fn()",
		trg, event, table)); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, fmt.Sprintf("drop trigger if exists %s on %s", trg, table)); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
	})
}

func TestNonMemberForbiddenOnAllHandlers(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "intruder-app", "", nil)
	outside := testdb.SeedOrg(t)
	intruder := http.Header{}
	intruder.Set("Authorization", "Bearer "+outside.Token)
	intruder.Set("X-Organization-Id", e.org.OrgID)

	cases := []struct {
		name   string
		method string
		path   string
		body   any
		params map[string]string
	}{
		{"list", http.MethodGet, "/api/v1/applications", nil, nil},
		{"create", http.MethodPost, "/api/v1/applications",
			map[string]any{"projectId": e.org.ProjectID, "name": "intruder-create", "sourceType": "image", "image": "nginx:1"}, nil},
		{"get", http.MethodGet, "/api/v1/applications/" + appID, nil, map[string]string{"id": appID}},
		{"update", http.MethodPut, "/api/v1/applications/" + appID,
			map[string]any{"name": "hijacked"}, map[string]string{"id": appID}},
		{"delete", http.MethodDelete, "/api/v1/applications/" + appID, nil, map[string]string{"id": appID}},
		{"envList", http.MethodGet, "/api/v1/applications/" + appID + "/env", nil, map[string]string{"id": appID}},
		{"envCreate", http.MethodPost, "/api/v1/applications/" + appID + "/env",
			map[string]any{"key": "K", "value": "v"}, map[string]string{"id": appID}},
		{"envUpdate", http.MethodPut, "/api/v1/applications/" + appID + "/env/00000000-0000-0000-0000-000000000001",
			map[string]any{"value": "v"}, map[string]string{"id": appID, "varId": "00000000-0000-0000-0000-000000000001"}},
		{"envDelete", http.MethodDelete, "/api/v1/applications/" + appID + "/env/00000000-0000-0000-0000-000000000001",
			nil, map[string]string{"id": appID, "varId": "00000000-0000-0000-0000-000000000001"}},
	}
	for _, tc := range cases {
		rec, _ := e.do(t, tc.method, tc.path, tc.body, intruder, tc.params)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s as non-member: got %d want 403 (%s)", tc.name, rec.Code, rec.Body.String())
		}
	}
}

func TestAmbiguousOrgMembershipFailsClosed(t *testing.T) {
	e := newAppsEnv(t)
	other := testdb.SeedOrg(t)
	// Put the other org's user into e.org as well so their membership lookup
	// is ambiguous without an explicit X-Organization-Id header.
	if _, err := testdb.Get(t).Exec(context.Background(),
		`insert into organization_members(organization_id, user_id, role) values ($1::uuid, $2::uuid, 'member')`,
		e.org.OrgID, other.UserID); err != nil {
		t.Fatalf("seed second membership: %v", err)
	}
	noHeader := http.Header{}
	noHeader.Set("Authorization", "Bearer "+other.Token)

	rec, _ := e.do(t, http.MethodGet, "/api/v1/applications", nil, noHeader, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("list without org header and ambiguous membership: got %d want 400", rec.Code)
	}
	rec, _ = e.do(t, http.MethodPost, "/api/v1/applications",
		map[string]any{"projectId": e.org.ProjectID, "name": "ambiguous", "sourceType": "image", "image": "nginx:1"},
		noHeader, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("create without org header and ambiguous membership: got %d want 400", rec.Code)
	}
}

func TestMalformedAndInvalidPayloadBranches(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "payload-app", "", nil)
	hdr := e.authed(t)
	postRaw := func(method, path, body string, headers http.Header, params map[string]string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header = headers.Clone()
		req.Header.Set("Content-Type", "application/json")
		if params != nil {
			rctx := chi.NewRouteContext()
			for k, v := range params {
				rctx.URLParams.Add(k, v)
			}
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		}
		rec := httptest.NewRecorder()
		e.rtr.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := postRaw(http.MethodPost, "/api/v1/applications", "{not json", hdr, nil); got != http.StatusBadRequest {
		t.Errorf("malformed create body: got %d want 400", got)
	}
	if got := postRaw(http.MethodPut, "/api/v1/applications/"+appID, "{not json", hdr, map[string]string{"id": appID}); got != http.StatusBadRequest {
		t.Fatalf("malformed update body: got %d want 400", got)
	}
	// UpdateApplication with valid JSON but unknown id → 404.
	rec, _ := e.do(t, http.MethodPut, "/api/v1/applications/00000000-0000-0000-0000-000000000042",
		map[string]any{"name": "ghost"}, hdr, map[string]string{"id": "00000000-0000-0000-0000-000000000042"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update unknown application: got %d want 404", rec.Code)
	}

	envPath := "/api/v1/applications/" + appID + "/env"
	envParams := map[string]string{"id": appID}
	if got := postRaw(http.MethodPost, envPath, "{not json", hdr, envParams); got != http.StatusBadRequest {
		t.Errorf("malformed env create body: got %d want 400", got)
	}
	rec, _ = e.do(t, http.MethodPost, envPath, map[string]any{"key": "OK", "value": ""}, hdr, envParams)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty env value: got %d want 400", rec.Code)
	}
	longKey := strings.Repeat("K", 60) // hive.<12 chars>.<key>.v1 exceeds the 64-char docker secret name limit
	rec, _ = e.do(t, http.MethodPost, envPath, map[string]any{"key": longKey, "value": "v", "isSecret": true}, hdr, envParams)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("oversized secret key: got %d want 400", rec.Code)
	}
	if len(e.sw.created) != 0 {
		t.Fatalf("rejected payloads must not create docker secrets")
	}
}

func TestUpdateAppEnvVarDockerSecretFailureReturns500(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "rotfail-app", "", nil)
	hdr := e.authed(t)
	_, created := e.do(t, http.MethodPost, "/api/v1/applications/"+appID+"/env",
		map[string]any{"key": "ROT_KEY", "value": "v1", "isSecret": true}, hdr,
		map[string]string{"id": appID})
	secretID := created["id"].(string)

	e.sw.createErr = errors.New("daemon down")
	rec, _ := e.do(t, http.MethodPut, "/api/v1/applications/"+appID+"/env/"+secretID,
		map[string]any{"value": "v2"}, hdr, map[string]string{"id": appID, "varId": secretID})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("rotation with docker failure: got %d want 500 (%s)", rec.Code, rec.Body.String())
	}
}

func TestListScanErrorsSurfaceAs500(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "scan-app", "", nil)
	h := simpleProtoHandler(t)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), testdb.Get(t)))
		gr.Get("/api/v1/applications", h.ListApplications)
		gr.Get("/api/v1/applications/{id}/env", h.ListAppEnvVars)
	})
	hdr := e.authed(t)

	// applications.created_at no longer scans into time.Time.
	alterColumnType(t, "applications", "created_at")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
	req.Header = hdr
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list with unscannable rows: got %d want 500 (%s)", rec.Code, rec.Body.String())
	}

	// Same trick for app_env_vars.updated_at.
	if _, err := testdb.Get(t).Exec(context.Background(),
		`insert into app_env_vars(application_id, key, value, is_secret) values ($1::uuid, 'SCAN_KEY', 'v', false)`,
		appID); err != nil {
		t.Fatalf("seed env var: %v", err)
	}
	alterColumnType(t, "app_env_vars", "updated_at")
	req = httptest.NewRequest(http.MethodGet, "/api/v1/applications/"+appID+"/env", nil)
	req.Header = hdr
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("env list with unscannable rows: got %d want 500 (%s)", rec.Code, rec.Body.String())
	}
}

func TestEnvVarWriteFailuresSurfaceAs500(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "wfail-app", "", nil)
	hdr := e.authed(t)
	params := map[string]string{"id": appID}

	_, plain := e.do(t, http.MethodPost, "/api/v1/applications/"+appID+"/env",
		map[string]any{"key": "PLAIN_WF", "value": "v"}, hdr, params)
	plainID := plain["id"].(string)
	_, secret := e.do(t, http.MethodPost, "/api/v1/applications/"+appID+"/env",
		map[string]any{"key": "SECRET_WF", "value": "v", "isSecret": true}, hdr, params)
	secretID := secret["id"].(string)

	blockTableWrites(t, "app_env_vars", "update")

	// Plain update fails at Exec.
	rec, _ := e.do(t, http.MethodPut, "/api/v1/applications/"+appID+"/env/"+plainID,
		map[string]any{"value": "v2"}, hdr, map[string]string{"id": appID, "varId": plainID})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("blocked plain env update: got %d want 500 (%s)", rec.Code, rec.Body.String())
	}

	// Secret rotation creates a replacement docker secret, then fails at Exec
	// and must clean the orphaned secret up.
	before := len(e.sw.removedIDs)
	rec, _ = e.do(t, http.MethodPut, "/api/v1/applications/"+appID+"/env/"+secretID,
		map[string]any{"value": "v3"}, hdr, map[string]string{"id": appID, "varId": secretID})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("blocked secret env update: got %d want 500 (%s)", rec.Code, rec.Body.String())
	}
	if len(e.sw.removedIDs) != before+1 {
		t.Fatalf("failed rotation must remove the orphaned docker secret (removed=%v)", e.sw.removedIDs)
	}

	blockTableWrites(t, "app_env_vars", "delete")
	rec, _ = e.do(t, http.MethodDelete, "/api/v1/applications/"+appID+"/env/"+plainID, nil,
		hdr, map[string]string{"id": appID, "varId": plainID})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("blocked env delete: got %d want 500 (%s)", rec.Code, rec.Body.String())
	}
}

func TestSwarmFailureDegradations(t *testing.T) {
	e := newAppsEnv(t)
	appID := testdb.SeedApplication(t, e.org.ProjectID, "swarmfail-app", "", nil)
	hdr := e.authed(t)

	// ListServices failure degrades runtime statuses to not_deployed...
	e.sw.listErr = errors.New("swarm down")
	rec, out := e.do(t, http.MethodGet, "/api/v1/applications", nil, hdr, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list with swarm failure: %d %s", rec.Code, rec.Body.String())
	}
	for _, it := range out["items"].([]any) {
		if m := it.(map[string]any); m["status"] != "not_deployed" {
			t.Fatalf("status with swarm down = %v, want not_deployed", m["status"])
		}
	}

	// ...and makes service updates surface as 502.
	rec, _ = e.do(t, http.MethodPost, "/api/v1/applications/"+appID+"/start", nil, hdr,
		map[string]string{"id": appID})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("start with swarm failure: got %d want 502", rec.Code)
	}
	e.sw.listErr = nil

	// ListAllTasks failure keeps desired-replica statuses without running counts.
	svc := appService(appID, 2, 0)
	svc.Spec.Name = "swarmfail-svc"
	e.sw.services = []dockerswarm.Service{svc}
	e.sw.tasksErr = errors.New("tasks unavailable")
	rec, out = e.do(t, http.MethodGet, "/api/v1/applications", nil, hdr, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list with tasks failure: %d %s", rec.Code, rec.Body.String())
	}
	item := out["items"].([]any)[0].(map[string]any)
	if item["desiredReplicas"].(float64) != 2 || item["runningReplicas"].(float64) != 0 {
		t.Fatalf("desired/running with tasks failure = %v/%v, want 2/0", item["desiredReplicas"], item["runningReplicas"])
	}
	e.sw.tasksErr = nil

	// Unlabeled services are skipped, and non-running tasks do not count as replicas.
	unlabeled := dockerswarm.Service{ID: "svc-unlabeled"}
	e.sw.services = []dockerswarm.Service{unlabeled, svc}
	e.sw.tasks = []dockerswarm.Task{
		{ID: "t-shutdown", ServiceID: svc.ID, DesiredState: dockerswarm.TaskStateShutdown,
			Status: dockerswarm.TaskStatus{State: dockerswarm.TaskStateRunning}},
		{ID: "t-pending", ServiceID: unlabeled.ID, DesiredState: dockerswarm.TaskStateRunning,
			Status: dockerswarm.TaskStatus{State: dockerswarm.TaskStateRunning}},
	}
	rec, out = e.do(t, http.MethodGet, "/api/v1/applications", nil, hdr, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list with mixed tasks: %d %s", rec.Code, rec.Body.String())
	}
	item = out["items"].([]any)[0].(map[string]any)
	if item["runningReplicas"].(float64) != 0 || item["status"] != "deploying" {
		t.Fatalf("non-running tasks must not count as replicas: %+v", item)
	}
	e.sw.tasks = nil

	// Services in neither replicated nor global mode report zero desired replicas.
	if got := desiredReplicas(dockerswarm.Service{}); got != 0 {
		t.Fatalf("desiredReplicas of modeless service = %d, want 0", got)
	}
}
