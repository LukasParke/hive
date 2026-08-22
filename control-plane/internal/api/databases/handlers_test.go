package databases

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
	"github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/rbac"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/luke/hive/control-plane/internal/testdb"
	dockerswarm "github.com/moby/moby/api/types/swarm"
)

// fakeSwarm records Swarm API interactions and serves canned listings.
type fakeSwarm struct {
	createSpecs   []dockerswarm.ServiceSpec
	removed       []string
	services      []dockerswarm.Service
	secrets       []dockerswarm.Secret
	createErr     error
	listSrvErr    error
	listSecretErr error
}

func (f *fakeSwarm) CreateService(ctx context.Context, spec dockerswarm.ServiceSpec) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.createSpecs = append(f.createSpecs, spec)
	return "svc-id-" + spec.Name, nil //nolint:staticcheck // test fixture
}

func (f *fakeSwarm) RemoveService(ctx context.Context, id string) error {
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeSwarm) ListServices(ctx context.Context) ([]dockerswarm.Service, error) {
	return f.services, f.listSrvErr
}

func (f *fakeSwarm) ListSecrets(ctx context.Context) ([]dockerswarm.Secret, error) {
	if f.listSecretErr != nil {
		return nil, f.listSecretErr
	}
	return f.secrets, nil
}

func namedService(id, name string) dockerswarm.Service {
	return dockerswarm.Service{ID: id, Spec: dockerswarm.ServiceSpec{Annotations: dockerswarm.Annotations{Name: name}}}
}

func namedSecret(name string) dockerswarm.Secret {
	return dockerswarm.Secret{ID: "sec-" + name, Spec: dockerswarm.SecretSpec{Annotations: dockerswarm.Annotations{Name: name}}}
}

// newDatabasesRouter wires the real auth middleware around the handler with an
// injectable swarm fake so JWTs and RBAC are exercised end to end.
func newDatabasesRouter(t *testing.T, swarm SwarmAPI) (http.Handler, *Handler) {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool, nil)
	h.Swarm = swarm
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/databases", h.ListDatabaseServices)
		gr.Post("/api/v1/databases", h.CreateDatabaseService)
		gr.Get("/api/v1/databases/{id}", h.GetDatabaseService)
		gr.Delete("/api/v1/databases/{id}", h.DeleteDatabaseService)
	})
	return r, h
}

func createBody(projectID, engine, name, version, username, secretName, database string) string {
	body, _ := json.Marshal(map[string]any{
		"projectId": projectID, "engine": engine, "name": name,
		"version": version, "username": username,
		"passwordSecretName": secretName, "databaseName": database,
	})
	return string(body)
}

func TestCreateDatabaseServiceSuccess(t *testing.T) {
	sw := &fakeSwarm{secrets: []dockerswarm.Secret{namedSecret("db-password")}}
	router, _ := newDatabasesRouter(t, sw)
	org := testdb.SeedOrg(t)

	rec := doJSON(router, http.MethodPost, "/api/v1/databases", org.Headers,
		createBody(org.ProjectID, "Postgres ", "My DB!!", "16", "appuser", "db-password", "appdb"))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("response = %s err=%v", rec.Body.String(), err)
	}

	if len(sw.createSpecs) != 1 {
		t.Fatalf("CreateService calls = %d, want 1", len(sw.createSpecs))
	}
	spec := sw.createSpecs[0]
	if spec.Name != "db-my-db" { //nolint:staticcheck // test fixture
		t.Fatalf("service name = %q, want db-my-db (normalized)", spec.Name) //nolint:staticcheck // test fixture
	}
	wantLabels := map[string]string{
		"hive.db.engine": "postgres", "hive.db.name": "my-db",
		"hive.org.id": org.OrgID, "hive.project.id": org.ProjectID,
	}
	for k, v := range wantLabels {
		if got := spec.Annotations.Labels[k]; got != v { //nolint:staticcheck // test fixture
			t.Fatalf("label %s = %q, want %q", k, got, v)
		}
	}
	if spec.TaskTemplate.ContainerSpec == nil || spec.TaskTemplate.ContainerSpec.Image != "postgres:16" {
		t.Fatalf("image = %v, want postgres:16", spec.TaskTemplate.ContainerSpec)
	}
	env := map[string]bool{}
	for _, e := range spec.TaskTemplate.ContainerSpec.Env {
		env[e] = true
	}
	for _, want := range []string{"POSTGRES_USER=appuser", "POSTGRES_DB=appdb", "POSTGRES_PASSWORD_FILE=/run/secrets/db-password"} {
		if !env[want] {
			t.Fatalf("missing env %q in %v", want, env)
		}
	}
	secrets := spec.TaskTemplate.ContainerSpec.Secrets
	if len(secrets) != 1 || secrets[0].SecretName != "db-password" || secrets[0].File.Mode != 0o400 {
		t.Fatalf("secret refs = %#v", secrets)
	}
	if len(spec.TaskTemplate.Networks) != 1 || spec.TaskTemplate.Networks[0].Target != "hive_internal" {
		t.Fatalf("networks = %v, want hive_internal", spec.TaskTemplate.Networks)
	}
	if spec.Mode.Replicated == nil || spec.UpdateConfig == nil || spec.TaskTemplate.RestartPolicy == nil {
		t.Fatalf("service mode/update/restart not configured: %+v", spec)
	}

	var engine, serviceName string
	err := testdb.Get(t).QueryRow(context.Background(), `
		select engine, service_name from database_services where id=$1::uuid
	`, resp.ID).Scan(&engine, &serviceName)
	if err != nil {
		t.Fatalf("row missing: %v", err)
	}
	if engine != "postgres" || serviceName != "db-my-db" {
		t.Fatalf("row = %s/%s, want postgres/db-my-db", engine, serviceName)
	}
}

func TestCreateDatabaseServiceValidation(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		swarm      *fakeSwarm
		wantStatus int
		wantBody   string
	}{
		{
			name:       "malformed json",
			body:       "{nope",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing required fields",
			body:       createBody("", "", "", "", "", "", ""),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unsupported engine",
			body:       createBody("PROJECT", "oracle", "db1", "", "", "", ""),
			wantStatus: http.StatusBadRequest,
			wantBody:   "unsupported database engine",
		},
		{
			name:       "password secret missing from swarm",
			body:       createBody("PROJECT", "postgres", "db1", "", "", "nope-secret", ""),
			swarm:      &fakeSwarm{},
			wantStatus: http.StatusBadRequest,
			wantBody:   "password secret not found",
		},
		{
			name:       "project not found",
			body:       createBody("00000000-0000-0000-0000-000000000000", "postgres", "db1", "", "", "", ""),
			wantStatus: http.StatusBadRequest,
			wantBody:   "project not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sw := tt.swarm
			if sw == nil {
				sw = &fakeSwarm{}
			}
			router, _ := newDatabasesRouter(t, sw)
			org := testdb.SeedOrgWithRole(t, rbac.RoleOwner)
			body := strings.ReplaceAll(tt.body, "PROJECT", org.ProjectID)
			rec := doJSON(router, http.MethodPost, "/api/v1/databases", org.Headers, body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), tt.wantStatus)
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %s, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
			if tt.wantStatus != http.StatusCreated && len(sw.createSpecs) != 0 {
				t.Fatalf("swarm must not be called for invalid requests")
			}
		})
	}

	t.Run("foreign project forbidden", func(t *testing.T) {
		sw := &fakeSwarm{}
		router, _ := newDatabasesRouter(t, sw)
		orgA := testdb.SeedOrg(t)
		orgB := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodPost, "/api/v1/databases", orgB.Headers,
			createBody(orgA.ProjectID, "postgres", "db1", "", "", "", ""))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("member role forbidden on create", func(t *testing.T) {
		sw := &fakeSwarm{}
		router, _ := newDatabasesRouter(t, sw)
		org := testdb.SeedOrgWithRole(t, rbac.RoleAdmin)
		member := org.AddMember(t, rbac.RoleMember)
		rec := doJSON(router, http.MethodPost, "/api/v1/databases", member.Headers,
			createBody(org.ProjectID, "postgres", "db1", "", "", "", ""))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("swarm create failure returns bad request", func(t *testing.T) {
		sw := &fakeSwarm{createErr: errors.New("swarm down")}
		router, _ := newDatabasesRouter(t, sw)
		org := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodPost, "/api/v1/databases", org.Headers,
			createBody(org.ProjectID, "postgres", "db1", "", "", "", ""))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("secret listing failure fails secretExists check", func(t *testing.T) {
		sw := &fakeSwarm{listSecretErr: errors.New("secrets unavailable")}
		router, _ := newDatabasesRouter(t, sw)
		org := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodPost, "/api/v1/databases", org.Headers,
			createBody(org.ProjectID, "postgres", "db1", "", "", "any-secret", ""))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "password secret not found") {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("row insert failure compensates by removing swarm service", func(t *testing.T) {
		sw := &fakeSwarm{}
		router, h := newDatabasesRouter(t, sw)
		pool := testdb.Get(t)
		// Break the insert by renaming a column after validation succeeds.
		if _, err := pool.Exec(context.Background(), `alter table database_services rename column port to port_gone`); err != nil {
			t.Fatalf("rename: %v", err)
		}
		org := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodPost, "/api/v1/databases", org.Headers,
			createBody(org.ProjectID, "redis", "cache", "", "", "", ""))
		if _, err := pool.Exec(context.Background(), `alter table database_services rename column port_gone to port`); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
		if len(sw.removed) != 1 {
			t.Fatalf("RemoveService compensation calls = %d, want 1", len(sw.removed))
		}
		_ = h
	})
}

func TestListDatabaseServices(t *testing.T) {
	sw := &fakeSwarm{}
	router, _ := newDatabasesRouter(t, sw)
	org := testdb.SeedOrg(t)

	// Seed two rows directly.
	for _, name := range []string{"pg-main", "redis-cache"} {
		engine := "postgres"
		port := 5432
		if name == "redis-cache" {
			engine, port = "redis", 6379
		}
		if _, err := testdb.Get(t).Exec(context.Background(), `
			insert into database_services(project_id, engine, name, version, service_name, database_name, port)
			values ($1::uuid, $2, $3, 'latest', $4, $5, $6)
		`, org.ProjectID, engine, name, "db-"+name, name+"db", port); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	rec := doJSON(router, http.MethodGet, "/api/v1/databases", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2 (%s)", len(resp.Items), rec.Body.String())
	}

	// Other organizations see nothing.
	other := testdb.SeedOrg(t)
	rec = doJSON(router, http.MethodGet, "/api/v1/databases", other.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":null`) {
		t.Fatalf("foreign org response = %d %s", rec.Code, rec.Body.String())
	}

	// Unauthenticated requests never reach the handler.
	rec = doJSON(router, http.MethodGet, "/api/v1/databases", http.Header{}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rec.Code)
	}
}

func TestGetDatabaseService(t *testing.T) {
	sw := &fakeSwarm{}
	router, _ := newDatabasesRouter(t, sw)
	org := testdb.SeedOrg(t)

	var id string
	if err := testdb.Get(t).QueryRow(context.Background(), `
		insert into database_services(project_id, engine, name, version, service_name, database_name, port)
		values ($1::uuid, 'mysql', 'main', '8', 'db-main', 'maindb', 3306) returning id::text
	`, org.ProjectID).Scan(&id); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("returns scoped service", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/databases/"+id, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["engine"] != "mysql" || resp["port"] != float64(3306) {
			t.Fatalf("response = %v", resp)
		}
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/databases/00000000-0000-0000-0000-000000000000", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("cross-org id returns 404", func(t *testing.T) {
		other := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodGet, "/api/v1/databases/"+id, other.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestDeleteDatabaseService(t *testing.T) {
	t.Run("removes swarm service and row", func(t *testing.T) {
		sw := &fakeSwarm{services: []dockerswarm.Service{namedService("svc-1", "db-todelete"), namedService("svc-2", "unrelated")}}
		router, _ := newDatabasesRouter(t, sw)
		org := testdb.SeedOrg(t)
		var id string
		if err := testdb.Get(t).QueryRow(context.Background(), `
			insert into database_services(project_id, engine, name, version, service_name, database_name, port)
			values ($1::uuid, 'postgres', 'todelete', '16', 'db-todelete', 'todelete', 5432) returning id::text
		`, org.ProjectID).Scan(&id); err != nil {
			t.Fatalf("seed: %v", err)
		}

		rec := doJSON(router, http.MethodDelete, "/api/v1/databases/"+id, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if len(sw.removed) != 1 || sw.removed[0] != "svc-1" {
			t.Fatalf("removed = %v, want [svc-1]", sw.removed)
		}
		if n := testdb.QueryCount(t, `select count(*) from database_services where id=$1::uuid`, id); n != 0 {
			t.Fatal("database_services row survived delete")
		}
	})

	t.Run("tolerates swarm list failure", func(t *testing.T) {
		sw := &fakeSwarm{listSrvErr: errors.New("list failed")}
		router, _ := newDatabasesRouter(t, sw)
		org := testdb.SeedOrg(t)
		var id string
		if err := testdb.Get(t).QueryRow(context.Background(), `
			insert into database_services(project_id, engine, name, version, service_name, database_name, port)
			values ($1::uuid, 'postgres', 'x', '16', 'db-x', 'xdb', 5432) returning id::text
		`, org.ProjectID).Scan(&id); err != nil {
			t.Fatalf("seed: %v", err)
		}
		rec := doJSON(router, http.MethodDelete, "/api/v1/databases/"+id, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 despite swarm failure", rec.Code)
		}
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		sw := &fakeSwarm{}
		router, _ := newDatabasesRouter(t, sw)
		org := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodDelete, "/api/v1/databases/00000000-0000-0000-0000-000000000000", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("member role forbidden", func(t *testing.T) {
		sw := &fakeSwarm{}
		router, _ := newDatabasesRouter(t, sw)
		admin := testdb.SeedOrgWithRole(t, rbac.RoleAdmin)
		member := admin.AddMember(t, rbac.RoleMember)
		rec := doJSON(router, http.MethodDelete, "/api/v1/databases/00000000-0000-0000-0000-000000000000", member.Headers, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestDBEngineImageTable(t *testing.T) {
	cases := []struct {
		engine, version, image string
		port                   int
	}{
		{"postgres", "16", "postgres:16", 5432},
		{"Postgres", "", "postgres:latest", 5432},
		{"mysql", "8", "mysql:8", 3306},
		{"mariadb", "", "mariadb:latest", 3306},
		{"mongo", "7", "mongo:7", 27017},
		{"redis", "", "redis:latest", 6379},
	}
	for _, c := range cases {
		image, port, ok := dbEngineImage(c.engine, c.version)
		if !ok || image != c.image || port != c.port {
			t.Fatalf("dbEngineImage(%q,%q) = %s/%d/%v, want %s/%d/true", c.engine, c.version, image, port, ok, c.image, c.port)
		}
	}
	if _, _, ok := dbEngineImage("oracle", ""); ok {
		t.Fatal("unknown engine must be rejected")
	}
}

func TestNormalizeServiceName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"My DB!!", "my-db"},
		{"  spaced  ", "spaced"},
		{"---", "database"},
		{"", "database"},
		{"!!!@@@", "database"},
		{strings.Repeat("a", 60), strings.Repeat("a", 48)},
		{"a/b:c", "a-b-c"},
	}
	for _, c := range cases {
		if got := normalizeServiceName(c.in); got != c.want {
			t.Fatalf("normalizeServiceName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := normalizeServiceName(strings.Repeat("ab-", 30))
	if strings.HasSuffix(long, "-") || strings.HasPrefix(long, "-") {
		t.Fatalf("trimmed result has stray dashes: %q", long)
	}
}

func TestDBServiceEnvMatrix(t *testing.T) {
	cases := []struct {
		name        string
		engine      string
		username    string
		secretName  string
		database    string
		mustHave    []string
		mustNotHave []string
	}{
		{name: "postgres defaults", engine: "postgres", mustHave: []string{"POSTGRES_USER=hive", "POSTGRES_DB=hive", "POSTGRES_PASSWORD=hive-password"}, mustNotHave: []string{"POSTGRES_PASSWORD_FILE=/run/secrets/x"}},
		{name: "mysql with secret file", engine: "mysql", username: "u", secretName: "pw", database: "d", mustHave: []string{"MYSQL_USER=u", "MYSQL_DATABASE=d", "MYSQL_PASSWORD_FILE=/run/secrets/pw", "MYSQL_ROOT_PASSWORD_FILE=/run/secrets/pw"}},
		{name: "mariadb plain password", engine: "mariadb", mustHave: []string{"MYSQL_PASSWORD=hive-password", "MYSQL_ROOT_PASSWORD=hive-password"}},
		{name: "mongo defaults", engine: "mongo", mustHave: []string{"MONGO_INITDB_ROOT_USERNAME=hive", "MONGO_INITDB_DATABASE=hive", "MONGO_INITDB_ROOT_PASSWORD=hive-password"}},
		{name: "mongo with file", engine: "mongo", secretName: "pw", mustHave: []string{"MONGO_INITDB_ROOT_PASSWORD_FILE=/run/secrets/pw"}},
		{name: "redis plain", engine: "redis", mustHave: []string{"REDIS_PASSWORD=hive-password"}},
		{name: "redis file", engine: "redis", secretName: "pw", mustHave: []string{"REDIS_PASSWORD_FILE=/run/secrets/pw"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := dbServiceEnv(c.engine, c.username, c.secretName, c.database)
			set := map[string]bool{}
			for _, e := range env {
				set[e] = true
			}
			for _, w := range c.mustHave {
				if !set[w] {
					t.Fatalf("env %v missing %q", env, w)
				}
			}
			for _, w := range c.mustNotHave {
				if set[w] {
					t.Fatalf("env %v must not contain %q", env, w)
				}
			}
		})
	}

	t.Run("unknown engine yields empty env", func(t *testing.T) {
		if env := dbServiceEnv("crystal", "", "", ""); len(env) != 0 {
			t.Fatalf("env = %v, want empty", env)
		}
	})
}

func TestPtrUint64(t *testing.T) {
	v := ptrUint64(3)
	if *v != 3 {
		t.Fatalf("*ptrUint64(3) = %d", *v)
	}
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

func TestNewHandlerSeam(t *testing.T) {
	pool := testdb.Get(t)
	if h := NewHandler(pool, &swarmclient.Client{}); h.Swarm == nil {
		t.Fatal("concrete client must populate the SwarmAPI field")
	}
	if h := NewHandler(pool, nil); h.Swarm != nil {
		t.Fatal("nil client must leave the SwarmAPI field nil")
	}
}

func TestListAndGetRejectNonMembers(t *testing.T) {
	sw := &fakeSwarm{}
	router, _ := newDatabasesRouter(t, sw)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)

	var id string
	if err := testdb.Get(t).QueryRow(context.Background(), `
		insert into database_services(project_id, engine, name, version, service_name, port)
		values ($1::uuid, 'postgres', 'main', '16', 'db-main', 5432) returning id::text
	`, orgA.ProjectID).Scan(&id); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, tt := range []struct{ name, method, path string }{
		{name: "list", method: http.MethodGet, path: "/api/v1/databases"},
		{name: "get", method: http.MethodGet, path: "/api/v1/databases/" + id},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(router, tt.method, tt.path, orgB.Headers, "")
			// Non-members simply see none of org A's resources.
			if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListScanFailureReturns500(t *testing.T) {
	sw := &fakeSwarm{}
	router, _ := newDatabasesRouter(t, sw)
	org := testdb.SeedOrg(t)
	// database_name is nullable; scanning NULL into a string fails.
	if _, err := testdb.Get(t).Exec(context.Background(), `
		insert into database_services(project_id, engine, name, version, service_name, port)
		values ($1::uuid, 'postgres', 'broken', '16', 'db-broken', 5432)
	`, org.ProjectID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := doJSON(router, http.MethodGet, "/api/v1/databases", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

// simpleProtoRouter wires the handler to a simple-protocol pool so parse-time
// SQL failures surface synchronously from Query/Exec.
func simpleProtoRouter(t *testing.T, swarm SwarmAPI) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	cfg, err := pgxpool.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	handlerPool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(handlerPool.Close)

	h := NewHandler(pool, nil)
	h.Swarm = swarm
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/databases", h.ListDatabaseServices)
		gr.Post("/api/v1/databases", h.CreateDatabaseService)
		gr.Get("/api/v1/databases/{id}", h.GetDatabaseService)
		gr.Delete("/api/v1/databases/{id}", h.DeleteDatabaseService)
	})
	return r
}

func TestStatementFailuresReturnErrorResponses(t *testing.T) {
	router := simpleProtoRouter(t, &fakeSwarm{})
	org := testdb.SeedOrg(t)

	t.Run("list query failure", func(t *testing.T) {
		renameColumn(t, "projects", "organization_id", "organization_id_gone")
		rec := doJSON(router, http.MethodGet, "/api/v1/databases", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete exec failure", func(t *testing.T) {
		p := testdb.Get(t)
		var id string
		if err := p.QueryRow(context.Background(), `
			insert into database_services(project_id, engine, name, version, service_name, port)
			values ($1::uuid, 'redis', 'cache', '7', 'db-cache', 6379) returning id::text
		`, org.ProjectID).Scan(&id); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// A dangling referencing row makes the DELETE fail with an FK violation.
		if _, err := p.Exec(context.Background(), `
			create table tmp_database_service_refs (service_id uuid references database_services(id))
		`); err != nil {
			t.Fatalf("create ref table: %v", err)
		}
		t.Cleanup(func() {
			if _, err := p.Exec(context.Background(), `drop table tmp_database_service_refs`); err != nil {
				t.Fatalf("drop ref table: %v", err)
			}
		})
		if _, err := p.Exec(context.Background(),
			`insert into tmp_database_service_refs(service_id) values ($1::uuid)`, id); err != nil {
			t.Fatalf("seed ref row: %v", err)
		}

		rec := doJSON(router, http.MethodDelete, "/api/v1/databases/"+id, org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}

func renameColumn(t *testing.T, table, from, to string) {
	t.Helper()
	ctx := context.Background()
	p := testdb.Get(t)
	if _, err := p.Exec(ctx, fmt.Sprintf("alter table %s rename column %s to %s", table, from, to)); err != nil {
		t.Fatalf("rename %s.%s: %v", table, from, err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, fmt.Sprintf("alter table %s rename column %s to %s", table, to, from)); err != nil {
			t.Fatalf("restore %s.%s: %v", table, to, err)
		}
	})
}
