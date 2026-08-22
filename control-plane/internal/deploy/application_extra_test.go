package deploy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	dockernet "github.com/moby/moby/api/types/network"
	dockerswarm "github.com/moby/moby/api/types/swarm"

	"github.com/luke/hive/control-plane/internal/proxy"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// fakeSwarm is an in-memory SwarmStack double recording every mutation so
// tests can assert on the exact calls the production code makes.
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
	updateCfgErr  error

	services    []dockerswarm.Service
	networks    map[string]string // name -> id
	secretsByID map[string]dockerswarm.Secret
	configsByID map[string]dockerswarm.Config
	nextID      int

	createdServices   []dockerswarm.ServiceSpec
	updatedServices   []svcUpdate
	removedServiceIDs []string
	createdNetworks   []string
	createdSecrets    []dockerswarm.SecretSpec
	createdConfigs    []dockerswarm.ConfigSpec
	updatedConfigs    []cfgUpdate
}

type svcUpdate struct {
	id      string
	version uint64
	spec    dockerswarm.ServiceSpec
}

type cfgUpdate struct {
	id      string
	version uint64
	spec    dockerswarm.ConfigSpec
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
	f.updatedServices = append(f.updatedServices, svcUpdate{id: id, version: version, spec: spec})
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
	f.createdNetworks = append(f.createdNetworks, name)
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
	f.createdSecrets = append(f.createdSecrets, spec)
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
	f.createdConfigs = append(f.createdConfigs, spec)
	f.nextID++
	id := fmt.Sprintf("cfg-new-%d", f.nextID)
	if f.configsByID == nil {
		f.configsByID = map[string]dockerswarm.Config{}
	}
	f.configsByID[id] = dockerswarm.Config{ID: id, Spec: spec}
	return id, nil
}

func (f *fakeSwarm) UpdateConfig(_ context.Context, id string, version uint64, spec dockerswarm.ConfigSpec) error {
	if f.updateCfgErr != nil {
		return f.updateCfgErr
	}
	f.updatedConfigs = append(f.updatedConfigs, cfgUpdate{id: id, version: version, spec: spec})
	if c, ok := f.configsByID[id]; ok {
		c.Spec = spec
		c.Meta.Version.Index = version + 1 //nolint:staticcheck // test fixture
		f.configsByID[id] = c
	}
	return nil
}

const testAppID = "12345678-1234-1234-1234-123456789abc" // shortID: 12345678

func TestApplicationCreate(t *testing.T) {
	fake := &fakeSwarm{}
	spec := ApplicationSpec{
		AppID:         testAppID,
		ServiceName:   "My App",
		Image:         "reg.example/app:1",
		ContainerPort: 8080,
		EnvVars: []EnvVar{
			{Key: "PLAIN", Value: "v1"},
			{Key: "TOKEN", IsSecret: true, SecretName: "hive.123456781234.TOKEN.v3"},
		},
	}
	if err := Application(context.Background(), fake, spec); err != nil {
		t.Fatalf("Application: %v", err)
	}
	if len(fake.createdServices) != 1 {
		t.Fatalf("created %d services, want 1", len(fake.createdServices))
	}
	got := fake.createdServices[0]
	if got.Name != "my-app-12345678" {
		t.Fatalf("service name = %q", got.Name)
	}
	for _, labels := range []map[string]string{got.Labels, got.TaskTemplate.ContainerSpec.Labels} {
		if labels["hive.app.id"] != testAppID || labels["hive.app.port"] != "8080" {
			t.Fatalf("labels = %v", labels)
		}
	}
	if got.TaskTemplate.ContainerSpec.Image != "reg.example/app:1" {
		t.Fatalf("image = %q", got.TaskTemplate.ContainerSpec.Image)
	}
	env := got.TaskTemplate.ContainerSpec.Env
	if len(env) != 1 || env[0] != "PLAIN=v1" {
		t.Fatalf("env = %v", env)
	}
	secrets := got.TaskTemplate.ContainerSpec.Secrets
	if len(secrets) != 1 {
		t.Fatalf("secrets = %d, want 1", len(secrets))
	}
	sr := secrets[0]
	if sr.SecretName != "hive.123456781234.TOKEN.v3" || sr.File == nil ||
		sr.File.Name != "TOKEN" || sr.File.UID != "0" || sr.File.GID != "0" || sr.File.Mode != 0o400 {
		t.Fatalf("secret ref = %+v", sr)
	}
	replicated := got.Mode.Replicated
	if replicated == nil || replicated.Replicas == nil || *replicated.Replicas != 1 {
		t.Fatalf("mode = %+v, want replicated with 1 replica", got.Mode)
	}
	if got.UpdateConfig == nil || got.UpdateConfig.Order != "start-first" || got.UpdateConfig.FailureAction != "rollback" {
		t.Fatalf("update config = %+v", got.UpdateConfig)
	}
	if len(got.TaskTemplate.Networks) != 0 {
		t.Fatalf("expected no networks, got %v", got.TaskTemplate.Networks)
	}
	if len(fake.updatedServices)+len(fake.removedServiceIDs) != 0 {
		t.Fatal("unexpected mutations on create path")
	}
}

func TestApplicationUpdate(t *testing.T) {
	existing := dockerswarm.Service{
		ID: "svc-1",
		Meta: dockerswarm.Meta{
			Version: dockerswarm.Version{Index: 7},
		},
		Spec: dockerswarm.ServiceSpec{
			Annotations: dockerswarm.Annotations{
				Name:   "custom-name",
				Labels: map[string]string{"hive.app.id": testAppID},
			},
		},
	}
	fake := &fakeSwarm{services: []dockerswarm.Service{existing}}
	spec := ApplicationSpec{AppID: testAppID, ServiceName: "whatever", Image: "img:2"}
	if err := Application(context.Background(), fake, spec); err != nil {
		t.Fatalf("Application: %v", err)
	}
	if len(fake.createdServices) != 0 {
		t.Fatal("create path must not run on update")
	}
	if len(fake.updatedServices) != 1 {
		t.Fatalf("updates = %d, want 1", len(fake.updatedServices))
	}
	up := fake.updatedServices[0]
	if up.id != "svc-1" || up.version != 7 {
		t.Fatalf("update call = (%q, %d), want (svc-1, 7)", up.id, up.version)
	}
	if up.spec.Name != "custom-name" {
		t.Fatalf("spec name = %q, want existing custom-name", up.spec.Name)
	}
	if up.spec.TaskTemplate.ContainerSpec.Image != "img:2" {
		t.Fatal("update did not carry desired image")
	}
}

func TestApplicationListError(t *testing.T) {
	fake := &fakeSwarm{listSvcErr: errors.New("swarm down")}
	err := Application(context.Background(), fake, ApplicationSpec{AppID: testAppID})
	if err == nil || !strings.Contains(err.Error(), "swarm down") {
		t.Fatalf("err = %v, want swarm down propagated", err)
	}
}

func TestApplicationNetworkAttachment(t *testing.T) {
	fake := &fakeSwarm{}
	spec := ApplicationSpec{
		AppID:       testAppID,
		ServiceName: "web",
		Image:       "img",
		ProjectSlug: "My Shop",
		DomainLookup: func(context.Context, string) ([]string, error) {
			return []string{"a.example.com"}, nil
		},
	}
	if err := Application(context.Background(), fake, spec); err != nil {
		t.Fatalf("Application: %v", err)
	}
	created := append([]string(nil), fake.createdNetworks...)
	sort.Strings(created)
	want := []string{"hive_project_my-shop", "hive_proxy"}
	if len(created) != 2 || created[0] != want[0] || created[1] != want[1] {
		t.Fatalf("created networks = %v, want %v", created, want)
	}
	nets := fake.createdServices[0].TaskTemplate.Networks
	targets := make([]string, 0, len(nets))
	for _, n := range nets {
		targets = append(targets, n.Target)
	}
	sort.Strings(targets)
	if len(targets) != 2 || targets[0] != "hive_project_my-shop-id" || targets[1] != "hive_proxy-id" {
		t.Fatalf("network targets = %v", targets)
	}
}

func TestApplicationDomainLookupError(t *testing.T) {
	fake := &fakeSwarm{}
	spec := ApplicationSpec{
		AppID: testAppID,
		DomainLookup: func(context.Context, string) ([]string, error) {
			return nil, errors.New("db down")
		},
	}
	err := Application(context.Background(), fake, spec)
	if err == nil || !strings.Contains(err.Error(), "lookup domains") {
		t.Fatalf("err = %v, want lookup domains failure", err)
	}
}

func TestApplicationEnsureNetworkError(t *testing.T) {
	fake := &fakeSwarm{createNetErr: errors.New("no overlay")}
	spec := ApplicationSpec{AppID: testAppID, ProjectSlug: "shop"}
	err := Application(context.Background(), fake, spec)
	if err == nil || !strings.Contains(err.Error(), "ensure network") {
		t.Fatalf("err = %v, want ensure network failure", err)
	}
}

func TestLoadEnvVars(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "envapp", "https://example.com/r.git", nil)

	ctx := context.Background()
	insert := func(key string, value any, secret bool, version int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			insert into app_env_vars (application_id, key, value, is_secret, secret_version)
			values ($1::uuid, $2, $3, $4, $5)`, appID, key, value, secret, version); err != nil {
			t.Fatalf("insert env var %s: %v", key, err)
		}
	}
	insert("PLAIN", "plain-value", false, 1)
	insert("SECRET_TOKEN", nil, true, 3)

	envVars, err := LoadEnvVars(ctx, pool, appID)
	if err != nil {
		t.Fatalf("LoadEnvVars: %v", err)
	}
	if len(envVars) != 2 {
		t.Fatalf("env vars = %d, want 2 (%+v)", len(envVars), envVars)
	}
	plain := envVars[0]
	if plain.Key != "PLAIN" || plain.Value != "plain-value" || plain.IsSecret || plain.SecretName != "" {
		t.Fatalf("plain var = %+v", plain)
	}
	secret := envVars[1]
	wantName := fmt.Sprintf("hive.%s.SECRET_TOKEN.v3", appID[:12])
	if secret.Key != "SECRET_TOKEN" || !secret.IsSecret || secret.Value != "" || secret.SecretName != wantName {
		t.Fatalf("secret var = %+v, want SecretName %q", secret, wantName)
	}
}

func TestLoadEnvVarsQueryError(t *testing.T) {
	badPool, err := pgxpool.New(context.Background(), "postgres://hive:hive@127.0.0.1:1/hive")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer badPool.Close()
	if _, err := LoadEnvVars(context.Background(), badPool, testAppID); err == nil || !strings.Contains(err.Error(), "load env vars") {
		t.Fatalf("err = %v, want load env vars failure", err)
	}
}

func TestRunDeployment(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "deploy-app", "https://example.com/r.git", nil)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		insert into app_env_vars (application_id, key, value, is_secret, secret_version)
		values ($1::uuid, 'PORT', '8080', false, 1)`, appID); err != nil {
		t.Fatalf("insert env var: %v", err)
	}

	fake := &fakeSwarm{}
	deps := Deps{Pool: pool, Swarm: fake}
	spec := ApplicationSpec{AppID: appID, ServiceName: "deploy-app", Image: "reg.example/a:1"}
	if err := RunDeployment(ctx, deps, spec); err != nil {
		t.Fatalf("RunDeployment: %v", err)
	}
	if len(fake.createdServices) != 1 {
		t.Fatalf("created services = %d, want 1", len(fake.createdServices))
	}
	env := fake.createdServices[0].TaskTemplate.ContainerSpec.Env
	if len(env) != 1 || env[0] != "PORT=8080" {
		t.Fatalf("env from DB not applied: %v", env)
	}
}

func TestRunDeploymentLoadError(t *testing.T) {
	badPool, err := pgxpool.New(context.Background(), "postgres://hive:hive@127.0.0.1:1/hive")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer badPool.Close()
	deps := Deps{Pool: badPool, Swarm: &fakeSwarm{}}
	err = RunDeployment(context.Background(), deps, ApplicationSpec{AppID: testAppID})
	if err == nil {
		t.Fatal("expected LoadEnvVars failure to propagate")
	}
}

func TestResolveAppService(t *testing.T) {
	ctx := context.Background()

	found := &fakeSwarm{services: []dockerswarm.Service{{
		ID: "svc-x",
		Spec: dockerswarm.ServiceSpec{Annotations: dockerswarm.Annotations{
			Name:   "app-svc",
			Labels: map[string]string{"hive.app.id": testAppID},
		}},
	}}}
	id, err := ResolveAppService(ctx, found, testAppID)
	if err != nil || id != "svc-x" {
		t.Fatalf("ResolveAppService = (%q, %v), want svc-x", id, err)
	}

	missing := &fakeSwarm{}
	id, err = ResolveAppService(ctx, missing, testAppID)
	if err != nil || id != "" {
		t.Fatalf("missing service = (%q, %v), want empty+nil", id, err)
	}

	broken := &fakeSwarm{listSvcErr: errors.New("boom")}
	if _, err := ResolveAppService(ctx, broken, testAppID); err == nil || !strings.Contains(err.Error(), "list services") {
		t.Fatalf("err = %v, want list failure wrapped", err)
	}
}

func TestApplyApplicationDomains(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "domain-app", "https://example.com/r.git", nil)
	ctx := context.Background()
	insertDomain := func(hostname, routeType string, priority int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			insert into domains (application_id, hostname, tls_enabled, route_type, path_prefix, strip_prefix, priority)
			values ($1::uuid, $2, true, $3, '', false, $4)`,
			appID, hostname, routeType, priority); err != nil {
			t.Fatalf("insert domain: %v", err)
		}
	}

	appService := func() *fakeSwarm {
		return &fakeSwarm{services: []dockerswarm.Service{{
			ID: "svc-dom",
			Spec: dockerswarm.ServiceSpec{Annotations: dockerswarm.Annotations{
				Name:   "domain-app-svc",
				Labels: map[string]string{"hive.app.id": appID},
			}},
		}}}
	}

	// No domain rows → no-op.
	none := &fakeSwarm{}
	if err := ApplyApplicationDomains(ctx, Deps{Pool: pool, Swarm: none}, appID, 8080); err != nil {
		t.Fatalf("no domains: %v", err)
	}
	if len(none.updatedServices) != 0 {
		t.Fatal("no domains must not touch services")
	}

	// Domains but no matching service → no-op.
	insertDomain("orphan.example.com", "host", 0)
	orphan := &fakeSwarm{}
	if err := ApplyApplicationDomains(ctx, Deps{Pool: pool, Swarm: orphan}, appID, 8080); err != nil {
		t.Fatalf("missing service: %v", err)
	}
	if len(orphan.updatedServices) != 0 {
		t.Fatal("missing service must be a no-op")
	}

	// Domains plus matching service → traefik labels applied (one update per
	// domain row, all against the same service).
	insertDomain("app.example.com", "host", 10)
	withSvc := appService()
	if err := ApplyApplicationDomains(ctx, Deps{Pool: pool, Swarm: withSvc}, appID, 9000); err != nil {
		t.Fatalf("with service: %v", err)
	}
	if len(withSvc.updatedServices) != 2 {
		t.Fatalf("updates = %d, want 2 (one per domain)", len(withSvc.updatedServices))
	}
	router := proxy.RouterNameFromHost("app.example.com")
	var labels map[string]string
	for _, up := range withSvc.updatedServices {
		if up.spec.Labels["traefik.http.routers."+router+".rule"] == "Host(`app.example.com`)" {
			labels = up.spec.Labels
		}
	}
	if labels == nil {
		t.Fatal("app.example.com route was not applied")
	}
	if got := labels["traefik.http.routers."+router+".entrypoints"]; got != "websecure" {
		t.Fatalf("entrypoint = %q, want websecure (tls_enabled)", got)
	}
	if labels["traefik.http.routers."+router+".tls"] != "true" {
		t.Fatal("missing tls label")
	}
	if got := labels["traefik.http.routers."+router+".priority"]; got != "10" {
		t.Fatalf("priority = %q", got)
	}
	if got := labels["traefik.http.services."+router+".loadbalancer.server.port"]; got != "9000" {
		t.Fatalf("server port = %q", got)
	}

	// Query error branch: a pool that cannot acquire a connection makes the
	// domains query fail without touching the shared test pool.
	badPool, poolErr := pgxpool.New(ctx, "postgres://hive:hive@127.0.0.1:1/hive")
	if poolErr != nil {
		t.Fatalf("pgxpool.New: %v", poolErr)
	}
	defer badPool.Close()
	err := ApplyApplicationDomains(ctx, Deps{Pool: badPool, Swarm: &fakeSwarm{}}, appID, 8080)
	if err == nil || !strings.Contains(err.Error(), "load domains") {
		t.Fatalf("err = %v, want load domains failure", err)
	}
}

// recordingEmitter captures Emit calls made by NotifyDeployment.
type recordingEmitter struct {
	channel string
	payload string
}

func (r *recordingEmitter) Emit(_ context.Context, channel, payload string) error {
	r.channel = channel
	r.payload = payload
	return nil
}

func TestNotifyDeployment(t *testing.T) {
	NotifyDeployment(context.Background(), nil, testAppID) // must not panic

	rec := &recordingEmitter{}
	NotifyDeployment(context.Background(), rec, testAppID)
	if rec.channel != "deployment:"+testAppID || rec.payload != "deployed" {
		t.Fatalf("emit = (%q, %q)", rec.channel, rec.payload)
	}
}

func TestNormalizeServiceNameSkipsSuffixTruncation(t *testing.T) {
	// A base already ending in -<shortID> skips truncation and can exceed 63
	// chars before the final hard cap trims it.
	base := strings.Repeat("a", 60) + "-12345678"
	got := normalizeServiceName(base, "12345678-abcd")
	if len(got) > 63 {
		t.Fatalf("name length = %d, want <= 63", len(got))
	}
	if !strings.HasPrefix(got, "aaaa") {
		t.Fatalf("name = %q", got)
	}
}
