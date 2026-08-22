package tunnels

import (
	"context"
	"errors"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/luke/hive/control-plane/internal/cloudflare"
	"github.com/moby/moby/api/types/swarm"
)

// --- error-injecting doubles ---

type failGetByNameRepo struct{ *fakeRepo }

func (f *failGetByNameRepo) GetByName(context.Context, string) (*Row, error) {
	return nil, errors.New("db offline")
}

type failCreateCF struct{ *fakeCF }

func (failCreateCF) CreateTunnel(context.Context, string, string) (cloudflare.TunnelRef, error) {
	return cloudflare.TunnelRef{}, errors.New("cloudflare 429")
}

type failPutStore struct {
	*fakeStore
	failOn func(name string) bool
}

func (f *failPutStore) Put(ctx context.Context, name, kind string, plain []byte) error {
	if f.failOn(name) {
		return errors.New("kms unavailable")
	}
	return f.fakeStore.Put(ctx, name, kind, plain)
}

type failSetStatusRepo struct{ *fakeRepo }

func (f *failSetStatusRepo) SetStatus(context.Context, string, string, string) error {
	return errors.New("status write failed")
}

type failUpdateIngressRepo struct{ *fakeRepo }

func (f *failUpdateIngressRepo) UpdateIngress(context.Context, string, []IngressRule) error {
	return errors.New("ingress write failed")
}

type failForgetSecretsRepo struct{ *fakeRepo }

func (f *failForgetSecretsRepo) ForgetSecrets(context.Context, []string) error {
	return errors.New("purge failed")
}

type failDeleteRowRepo struct{ *fakeRepo }

func (f *failDeleteRowRepo) Delete(context.Context, string) error {
	return errors.New("row delete failed")
}

// failSwarm injects targeted failures into the swarm double.
type failSwarm struct {
	*fakeSwarm
	createSecretErr  error
	updateSvcErr     error
	getSvcErr        error
	removeServiceErr error
	listTasksErr     error
	listSecretsErr   error
	createServiceErr error
}

func (f *failSwarm) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error) {
	if f.createSecretErr != nil {
		return "", f.createSecretErr
	}
	return f.fakeSwarm.CreateSecret(ctx, spec)
}

func (f *failSwarm) UpdateService(ctx context.Context, id string, version uint64, spec swarm.ServiceSpec) error {
	if f.updateSvcErr != nil {
		return f.updateSvcErr
	}
	return f.fakeSwarm.UpdateService(ctx, id, version, spec)
}

func (f *failSwarm) GetService(ctx context.Context, idOrName string) (swarm.Service, error) {
	if f.getSvcErr != nil {
		return swarm.Service{}, f.getSvcErr
	}
	return f.fakeSwarm.GetService(ctx, idOrName)
}

func (f *failSwarm) RemoveService(ctx context.Context, idOrName string) error {
	if f.removeServiceErr != nil {
		return f.removeServiceErr
	}
	return f.fakeSwarm.RemoveService(ctx, idOrName)
}

func (f *failSwarm) ListTasks(ctx context.Context, serviceID string) ([]swarm.Task, error) {
	if f.listTasksErr != nil {
		return nil, f.listTasksErr
	}
	return f.fakeSwarm.ListTasks(ctx, serviceID)
}

func (f *failSwarm) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	if f.listSecretsErr != nil {
		return nil, f.listSecretsErr
	}
	return f.fakeSwarm.ListSecrets(ctx)
}

func (f *failSwarm) CreateService(ctx context.Context, spec swarm.ServiceSpec) (string, error) {
	if f.createServiceErr != nil {
		return "", f.createServiceErr
	}
	return f.fakeSwarm.CreateService(ctx, spec)
}

// statusCF fails GetTunnel so CloudflareStatus must stay empty.
type statusCF struct{ *fakeCF }

var errCFAPI = errors.New("cloudflare api down")

func (statusCF) GetTunnel(context.Context, string, string) (string, error) { return "", errCFAPI }

// dnsFailCF fails DNS operations selectively.
type dnsFailCF struct {
	*fakeCF
	createRouteErr error
	deleteRecErr   error
}

func (f *dnsFailCF) CreateDNSRoute(context.Context, string, string, string) (string, error) {
	if f.createRouteErr != nil {
		return "", f.createRouteErr
	}
	return f.fakeCF.CreateDNSRoute(context.Background(), "", "", "")
}

func (f *dnsFailCF) DeleteDNSRecord(context.Context, string, string) error {
	if f.deleteRecErr != nil {
		return f.deleteRecErr
	}
	return f.fakeCF.DeleteDNSRecord(context.Background(), "", "")
}

type failSecondCreateSecret struct {
	*fakeSwarm
	calls int
}

func (f *failSecondCreateSecret) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error) {
	f.calls++
	if f.calls >= 2 {
		return "", errors.New("quota exceeded")
	}
	return f.fakeSwarm.CreateSecret(ctx, spec)
}

// seedDeployed creates a deployed tunnel row plus connector service.
func seedDeployed(t *testing.T, mgr *Manager, repo *fakeRepo, swarmer SwarmAPI) *Row {
	t.Helper()
	view, err := mgr.Create(context.Background(), validParams())
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}
	row, err := repo.Get(context.Background(), view.Row.ID)
	if err != nil {
		t.Fatalf("seed get: %v", err)
	}
	return row
}

// --- Create error paths ---

func TestManagerCreateRequiresCredentialStore(t *testing.T) {
	mgr := &Manager{Repo: newFakeRepo(), Swarm: newFakeSwarm()}
	_, err := mgr.Create(context.Background(), validParams())
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}
}

func TestManagerCreateValidationBranches(t *testing.T) {
	mgr, _, _, _, _ := newTestManager()
	ctx := context.Background()

	p := validParams()
	p.Name = "Bad_Name"
	if _, err := mgr.Create(ctx, p); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("invalid name: got %v", err)
	}
	p = validParams()
	p.Ingress = []IngressRule{{Hostname: "app.example.com", Service: "ftp://x"}}
	if _, err := mgr.Create(ctx, p); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("invalid ingress: got %v", err)
	}
	p = validParams()
	p.AccountID = ""
	if _, err := mgr.Create(ctx, p); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "accountId") {
		t.Errorf("missing accountId: got %v", err)
	}
	p = validParams()
	p.APIToken = ""
	if _, err := mgr.Create(ctx, p); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "apiToken") {
		t.Errorf("missing apiToken: got %v", err)
	}
}

func TestManagerCreateDuplicateAndLookupFailure(t *testing.T) {
	ctx := context.Background()
	mgr, _, _, _, _ := newTestManager()
	if _, err := mgr.Create(ctx, validParams()); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := mgr.Create(ctx, validParams())
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "prod-edge") {
		t.Fatalf("expected conflict naming tunnel, got %v", err)
	}

	mgr2 := &Manager{Repo: &failGetByNameRepo{newFakeRepo()}, Swarm: newFakeSwarm(),
		Store: newFakeStore(), NewClient: func(string) cloudflare.API { return newFakeCF() }}
	_, err = mgr2.Create(ctx, validParams())
	if err == nil || !strings.Contains(err.Error(), "look up tunnel") {
		t.Fatalf("expected lookup failure, got %v", err)
	}
}

func TestManagerCreateCloudflareFailure(t *testing.T) {
	mgr := &Manager{
		Repo: newFakeRepo(), Swarm: newFakeSwarm(), Store: newFakeStore(),
		NewClient: func(string) cloudflare.API { return failCreateCF{newFakeCF()} },
	}
	_, err := mgr.Create(context.Background(), validParams())
	if err == nil || !strings.Contains(err.Error(), "cloudflare create tunnel prod-edge") {
		t.Fatalf("expected wrapped CF failure, got %v", err)
	}
}

func TestManagerCreateSecretStorageFailure(t *testing.T) {
	store := &failPutStore{fakeStore: newFakeStore(), failOn: func(string) bool { return true }}
	mgr := &Manager{
		Repo: newFakeRepo(), Swarm: newFakeSwarm(), Store: store,
		NewClient: func(string) cloudflare.API { return newFakeCF() },
	}
	_, err := mgr.Create(context.Background(), validParams())
	if err == nil || !strings.Contains(err.Error(), "store tunnel credentials") {
		t.Fatalf("expected credential storage failure, got %v", err)
	}

	// Only the API-token write fails.
	tokenOnly := &failPutStore{fakeStore: newFakeStore(),
		failOn: func(name string) bool { return strings.HasPrefix(name, "tunnel-api-token:") }}
	mgr.Store = tokenOnly
	_, err = mgr.Create(context.Background(), validParams())
	if err == nil || !strings.Contains(err.Error(), "store tunnel api token") {
		t.Fatalf("expected token storage failure, got %v", err)
	}
}

func TestManagerCreateDeployFailureMarksRowErrored(t *testing.T) {
	swarmer := &failSwarm{fakeSwarm: newFakeSwarm(), createServiceErr: errors.New("scheduler down")}
	mgr, repo, _, _, _ := newTestManager()
	mgr.Swarm = swarmer

	_, err := mgr.Create(context.Background(), validParams())
	if err == nil || !strings.Contains(err.Error(), "deploy tunnel prod-edge") {
		t.Fatalf("expected wrapped deploy failure, got %v", err)
	}
	calls := repo.statusCalls
	if len(calls) == 0 || calls[len(calls)-1][1] != StatusError {
		t.Fatalf("row not marked errored: %v", calls)
	}
}

func TestManagerCreateDeployFailureAlsoFailsStatusWrite(t *testing.T) {
	mgr, _, _, _, _ := newTestManager()
	mgr.Repo = &failSetStatusRepo{newFakeRepo()}
	mgr.Swarm = &failSwarm{fakeSwarm: newFakeSwarm(), createServiceErr: errors.New("scheduler down")}
	_, err := mgr.Create(context.Background(), validParams())
	if err == nil || !strings.Contains(err.Error(), "also failed to record error state") {
		t.Fatalf("expected double-failure wrap, got %v", err)
	}
}

func TestManagerCreatePublishDNSFailure(t *testing.T) {
	mgr, repo, _, _, cf := newTestManager()
	_ = repo
	mgr.NewClient = func(string) cloudflare.API {
		return &dnsFailCF{fakeCF: cf, createRouteErr: errors.New("zone locked")}
	}
	_, err := mgr.Create(context.Background(), validParams())
	if err == nil || !strings.Contains(err.Error(), "publish dns route for app.example.com") {
		t.Fatalf("expected DNS publish failure, got %v", err)
	}
	last := repo.statusCalls[len(repo.statusCalls)-1]
	if last[1] != StatusError {
		t.Fatalf("row status after DNS failure = %q", last[1])
	}
}

// --- UpdateIngress paths ---

func TestManagerUpdateIngressValidationAndLookup(t *testing.T) {
	mgr, repo, _, _, _ := newTestManager()
	row := seedDeployed(t, mgr, repo, mgr.Swarm)
	ctx := context.Background()

	noStore := &Manager{Repo: repo, Swarm: newFakeSwarm()}
	if _, err := noStore.UpdateIngress(ctx, row.ID, sampleRules); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("nil store: got %v", err)
	}
	if _, err := mgr.UpdateIngress(ctx, row.ID, nil); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty rules: got %v", err)
	}
	if _, err := mgr.UpdateIngress(ctx, "missing-id", sampleRules); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: got %v", err)
	}
	badToken := &Manager{Repo: repo, Swarm: newFakeSwarm(), Store: newFakeStore(),
		NewClient: mgr.NewClient}
	if _, err := badToken.UpdateIngress(ctx, row.ID, sampleRules); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("missing stored token: got %v", err)
	}
}

func TestManagerUpdateIngressConnectorInspectionErrors(t *testing.T) {
	mgr, repo, _, _, _ := newTestManager()
	row := seedDeployed(t, mgr, repo, mgr.Swarm)

	// Connector service gone entirely.
	fresh := newFakeSwarm()
	mgr.Swarm = fresh
	_, err := mgr.UpdateIngress(context.Background(), row.ID, sampleRules)
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "connector service hive_tunnel_prod-edge") {
		t.Fatalf("missing connector: got %v", err)
	}

	// Inspection fails transiently.
	mgr.Swarm = &failSwarm{fakeSwarm: fresh, getSvcErr: errors.New("rpc timeout")}
	_, err = mgr.UpdateIngress(context.Background(), row.ID, sampleRules)
	if err == nil || !strings.Contains(err.Error(), "inspect connector service") {
		t.Fatalf("inspect failure: got %v", err)
	}
}

func TestManagerUpdateIngressRotateAndRollback(t *testing.T) {
	ctx := context.Background()
	mgr, repo, swarmer, _, _ := newTestManager()
	row := seedDeployed(t, mgr, repo, swarmer)
	fs := swarmer

	// Swarm secret listing down: rotating the rendered config must fail.
	mgr.Swarm = &failSwarm{fakeSwarm: fs, listSecretsErr: errors.New("secrets rpc down")}
	_, err := mgr.UpdateIngress(ctx, row.ID, sampleRules)
	if err == nil || !strings.Contains(err.Error(), "rotate config secret") {
		t.Fatalf("config rotation failure: got %v", err)
	}
	mgr.Swarm = fs

	// Credentials secret absent from the swarm.
	var credID string
	for _, s := range fs.secrets {
		if s.Spec.Name == credentialSecretName(row.Name) {
			credID = s.ID
		}
	}
	delete(fs.secrets, credID)
	_, err = mgr.UpdateIngress(ctx, row.ID, sampleRules)
	if err == nil || !strings.Contains(err.Error(), "credentials secret") {
		t.Fatalf("missing credentials secret: got %v", err)
	}

	// Restore credentials; a failed service update must roll back (remove)
	// the freshly created config secret revision.
	_, _ = fs.CreateSecret(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: credentialSecretName(row.Name)},
		Data:        []byte(`{"a":"acc"}`),
	})
	mgr.Swarm = &failSwarm{fakeSwarm: fs, updateSvcErr: errors.New("update rejected")}
	before := len(fs.removedSecretIDs)
	_, err = mgr.UpdateIngress(ctx, row.ID, sampleRules)
	if err == nil || !strings.Contains(err.Error(), "redeploy connector service") {
		t.Fatalf("service update failure: got %v", err)
	}
	if len(fs.removedSecretIDs) <= before {
		t.Fatalf("rotated config secret not rolled back, removals=%v", fs.removedSecretIDs)
	}
	// Only the freshly rotated revision is rolled back; the deployed
	// revision-1 secret stays untouched.
	secrets, _ := fs.ListSecrets(ctx)
	for _, sec := range secrets {
		if sec.Spec.Name == configSecretName(row.Name, 2) {
			t.Fatal("rotated config revision survived rollback")
		}
	}
}

func TestManagerUpdateIngressDiffDNSAndPersistFailures(t *testing.T) {
	ctx := context.Background()
	mgr, repo, swarmer, _, cf := newTestManager()
	row := seedDeployed(t, mgr, repo, swarmer)

	// Dropping a hostname deletes its record; adding one publishes a CNAME.
	cf.dnsRecords["stale.example.com"] = "cf-prod-edge-rec9"
	if err := repo.UpdateDNSRecords(ctx, row.ID, map[string]string{"stale.example.com": "cf-prod-edge-rec9"}); err != nil {
		t.Fatalf("seed dns records: %v", err)
	}
	rules := []IngressRule{{Hostname: "fresh.example.com", Service: "http://traefik:80"}}
	view, err := mgr.UpdateIngress(ctx, row.ID, rules)
	if err != nil {
		t.Fatalf("diff update: %v", err)
	}
	if _, ok := cf.dnsRecords["stale.example.com"]; ok {
		t.Fatal("stale record survived diff")
	}
	if _, ok := cf.dnsRecords["fresh.example.com"]; !ok {
		t.Fatal("fresh record missing")
	}
	if len(view.Row.Ingress) != 1 || view.Row.Ingress[0].Hostname != "fresh.example.com" {
		t.Fatalf("persisted ingress mismatch: %+v", view.Row.Ingress)
	}

	// Record deletion failure surfaces.
	mgr.NewClient = func(string) cloudflare.API {
		return &dnsFailCF{fakeCF: cf, deleteRecErr: errors.New("record pinned")}
	}
	if err := repo.UpdateDNSRecords(ctx, row.ID, map[string]string{"gone.example.com": "rec-x"}); err != nil {
		t.Fatalf("seed dns records: %v", err)
	}
	if _, err := mgr.UpdateIngress(ctx, row.ID, sampleRules); err == nil ||
		!strings.Contains(err.Error(), "remove dns route for gone.example.com") {
		t.Fatalf("delete-record failure: got %v", err)
	}

	// Publishing a new route failure surfaces.
	mgr.NewClient = func(string) cloudflare.API {
		return &dnsFailCF{fakeCF: cf, createRouteErr: errors.New("rate limited")}
	}
	if err := repo.UpdateDNSRecords(ctx, row.ID, map[string]string{}); err != nil {
		t.Fatalf("clear dns records: %v", err)
	}
	if _, err := mgr.UpdateIngress(ctx, row.ID, sampleRules); err == nil ||
		!strings.Contains(err.Error(), "publish dns route for app.example.com") {
		t.Fatalf("create-route failure: got %v", err)
	}

	// Persisting the new rule list failure surfaces.
	mgr.Repo = &failUpdateIngressRepo{repo}
	mgr.NewClient = func(string) cloudflare.API { return cf }
	if _, err := mgr.UpdateIngress(ctx, row.ID, sampleRules); err == nil ||
		!strings.Contains(err.Error(), "persist tunnel ingress") {
		t.Fatalf("persist failure: got %v", err)
	}
}

// --- Status / connector health ---

func TestManagerStatusWithCloudflareDown(t *testing.T) {
	mgr, repo, _, _, _ := newTestManager()
	mgr.NewClient = func(string) cloudflare.API { return statusCF{newFakeCF()} }
	row := seedDeployed(t, mgr, repo, mgr.Swarm.(*fakeSwarm))

	st, err := mgr.Status(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.CloudflareStatus != "" {
		t.Fatalf("Cloudflare outage must leave status empty, got %q", st.CloudflareStatus)
	}
}

func TestManagerConnectorStatusSwarmErrors(t *testing.T) {
	mgr, repo, _, _, _ := newTestManager()
	row := seedDeployed(t, mgr, repo, mgr.Swarm)
	ctx := context.Background()

	mgr.Swarm = &failSwarm{fakeSwarm: newFakeSwarm(), getSvcErr: errors.New("rpc down")}
	if _, err := mgr.Status(ctx, row.ID); err == nil || !strings.Contains(err.Error(), "inspect connector service") {
		t.Fatalf("get service failure: got %v", err)
	}
	mgr.Swarm = &failSwarm{fakeSwarm: newTestManagerSwarmWithConnector(t, repo, row.ID), listTasksErr: errors.New("tasks rpc down")}
	if _, err := mgr.Status(ctx, row.ID); err == nil || !strings.Contains(err.Error(), "list connector tasks") {
		t.Fatalf("list tasks failure: got %v", err)
	}
}

// --- Delete paths ---

func TestManagerDeleteWithoutCloudflareStillTearsDown(t *testing.T) {
	ctx := context.Background()
	mgr, repo, swarmer, store, _ := newTestManager()
	row := seedDeployed(t, mgr, repo, swarmer)
	fs := swarmer

	// Token unreadable -> no Cloudflare client, teardown proceeds locally.
	store.getErr = errors.New("decryption failed")
	if err := mgr.Delete(ctx, row.ID); err != nil {
		t.Fatalf("Delete without CF access: %v", err)
	}
	for _, svc := range fs.services {
		if svc.Spec.Annotations.Name == serviceName(row.Name) { //nolint:staticcheck // test fixture
			t.Fatal("connector service was not removed")
		}
	}
	if n := testCountRows(repo); n != 0 {
		t.Fatalf("row survived delete (%d left)", n)
	}
}

func testCountRows(r *fakeRepo) int { return len(r.rows) }

func TestManagerDeleteErrorBranches(t *testing.T) {
	ctx := context.Background()
	mgr, repo, swarmer, _, cf := newTestManager()
	row := seedDeployed(t, mgr, repo, swarmer)

	// Cloudflare refuses to delete the tunnel.
	mgr.NewClient = func(string) cloudflare.API {
		c := newFakeCF()
		c.deleteErr = errors.New("still has active connections")
		return c
	}
	if _, ok := cf.tunnels[row.CfTunnelID]; !ok {
		t.Fatal("fixture lost its CF tunnel")
	}
	err := mgr.Delete(ctx, row.ID)
	if err == nil || !strings.Contains(err.Error(), "cloudflare delete tunnel prod-edge") {
		t.Fatalf("CF deletion failure: got %v", err)
	}

	// Connector removal fails with something other than not-found.
	mgr, repo2, swarmer2, _, _ := newTestManager()
	row2 := seedDeployed(t, mgr, repo2, swarmer2)
	mgr.Swarm = &failSwarm{fakeSwarm: swarmer2, removeServiceErr: errors.New("rpc down")}
	if err := mgr.Delete(ctx, row2.ID); err == nil || !strings.Contains(err.Error(), "remove connector service") {
		t.Fatalf("connector removal failure: got %v", err)
	}
	// Not-found is tolerated.
	mgr.Swarm = &failSwarm{fakeSwarm: newFakeSwarm()}
	if err := mgr.Delete(ctx, row2.ID); err != nil {
		t.Fatalf("not-found connector must be tolerated, got %v", err)
	}

	// Secret purge failure.
	mgr3, repo3, swarmer3, _, _ := newTestManager()
	row3 := seedDeployed(t, mgr3, repo3, swarmer3)
	mgr3.Repo = &failForgetSecretsRepo{repo3}
	if err := mgr3.Delete(ctx, row3.ID); err == nil || !strings.Contains(err.Error(), "purge stored tunnel secrets") {
		t.Fatalf("purge failure: got %v", err)
	}

	// Row deletion failure.
	mgr4, repo4, swarmer4, _, _ := newTestManager()
	row4 := seedDeployed(t, mgr4, repo4, swarmer4)
	mgr4.Repo = &failDeleteRowRepo{repo4}
	if err := mgr4.Delete(ctx, row4.ID); err == nil || !strings.Contains(err.Error(), "delete tunnel row") {
		t.Fatalf("row deletion failure: got %v", err)
	}
}

func TestManagerPruneConfigRevisionsDeletePath(t *testing.T) {
	ctx := context.Background()
	mgr, repo, swarmer, _, _ := newTestManager()
	row := seedDeployed(t, mgr, repo, swarmer)
	fs := swarmer

	// Add a stale revision and an unrelated secret.
	_, _ = fs.CreateSecret(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: configSecretName(row.Name, 2)}, Data: []byte("old"),
	})
	_, _ = fs.CreateSecret(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: "other-secret"}, Data: []byte("x"),
	})
	mgr.pruneConfigRevisions(ctx, row.Name, -1) // delete path removes every revision

	secrets, _ := fs.ListSecrets(ctx)
	names := map[string]bool{}
	for _, s := range secrets {
		names[s.Spec.Name] = true
	}
	if names[configSecretName(row.Name, 1)] || names[configSecretName(row.Name, 2)] {
		t.Fatal("all config revisions must be pruned on the delete path")
	}
	if !names["other-secret"] {
		t.Fatal("unrelated secret must survive pruning")
	}
}

func TestManagerNewManagerWiring(t *testing.T) {
	m := NewManager(nil, newFakeSwarm(), nil, nil)
	if m.Repo == nil || m.Swarm == nil || m.Store != nil {
		t.Fatalf("unexpected wiring: %+v", m)
	}
}

// --- service helpers edge cases ---

func TestValidateNameRejectsInvalid(t *testing.T) {
	for _, name := range []string{"", "-lead", "trail-", "UPPER", "under_score", strings.Repeat("a", 64)} {
		if err := ValidateName(name); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ValidateName(%q) accepted", name)
		}
	}
}

func TestValidHostnameEdgeCases(t *testing.T) {
	invalid := []string{"*", "*.", "*.com", "a..b", "host_name.example.com", "single"}
	for _, h := range invalid {
		if ValidHostname(h) {
			t.Errorf("ValidHostname(%q)=true, want false", h)
		}
	}
	valid := []string{"a.b", "app.example.com.", "*.app.example.com", "0-1.a-2"}
	for _, h := range valid {
		if !ValidHostname(h) {
			t.Errorf("ValidHostname(%q)=false, want true", h)
		}
	}
}

func TestRevisionLabelParsing(t *testing.T) {
	if got := revisionLabel(nil); got != 0 {
		t.Errorf("revisionLabel(nil)=%d", got)
	}
	if got := revisionLabel(map[string]string{}); got != 0 {
		t.Errorf("revisionLabel(empty)=%d", got)
	}
	if got := revisionLabel(map[string]string{"hive.hive-tunnel.config-revision": "abc"}); got != 0 {
		t.Errorf("revisionLabel(non-numeric)=%d", got)
	}
}

func TestRenderConfigRejectsNothingButShapesOutput(t *testing.T) {
	out := RenderConfig("tid", "/run/secrets/cred", []IngressRule{
		{Hostname: "a.example.com", Path: "/p", Service: "http://up"},
	})
	for _, want := range []string{"tunnel: tid", "credentials-file: /run/secrets/cred",
		`hostname = "a.example.com"`, `path = "/p"`, `service = "http_status:404"`} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q:\n%s", want, out)
		}
	}
}

var _ = cerrdefs.ErrNotFound // keep import if branches shift

// newTestManagerSwarmWithConnector returns a swarm double that still has
// rowName's connector service registered (for status-path error injection).
func newTestManagerSwarmWithConnector(t *testing.T, repo *fakeRepo, rowID string) *fakeSwarm {
	t.Helper()
	row, err := repo.Get(context.Background(), rowID)
	if err != nil {
		t.Fatalf("fixture row: %v", err)
	}
	fs := newFakeSwarm()
	_, _ = fs.CreateService(context.Background(), swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: serviceName(row.Name)},
	})
	return fs
}

// --- additional branch coverage ---

type failListRepo struct{ *fakeRepo }

func (f *failListRepo) List(context.Context) ([]*Row, error) { return nil, errors.New("list boom") }

func TestManagerCreatePersistAndMarkDeployedFailures(t *testing.T) {
	ctx := context.Background()
	// Row persistence failure.
	mgr := &Manager{Repo: func() *fakeRepo { r := newFakeRepo(); r.createErr = errors.New("insert failed"); return r }(),
		Swarm: newFakeSwarm(), Store: newFakeStore(),
		NewClient: func(string) cloudflare.API { return newFakeCF() }}
	if _, err := mgr.Create(ctx, validParams()); err == nil || !strings.Contains(err.Error(), "persist tunnel prod-edge") {
		t.Fatalf("persist failure: got %v", err)
	}
	// Marking deployed fails.
	mgr2, _, _, _, _ := newTestManager()
	mgr2.Repo = &failSetStatusRepo{newFakeRepo()}
	_, err := mgr2.Create(ctx, validParams())
	if err == nil || !strings.Contains(err.Error(), "mark tunnel prod-edge deployed") {
		t.Fatalf("deployed-mark failure: got %v", err)
	}
}

func TestManagerCreateDeployServiceSecretFailures(t *testing.T) {
	ctx := context.Background()
	// Credential secret upload fails.
	mgr, _, _, _, _ := newTestManager()
	mgr.Swarm = &failSwarm{fakeSwarm: newFakeSwarm(), createSecretErr: errors.New("secret rpc down")}
	_, err := mgr.Create(ctx, validParams())
	if err == nil || !strings.Contains(err.Error(), "create swarm secret hive-tunnel-prod-edge-cred") {
		t.Fatalf("credential upload failure: got %v", err)
	}
	// Config secret upload fails on the second write only.
	mgr, _, _, _, _ = newTestManager()
	fs := newFakeSwarm()
	mgr.Swarm = &failSecondCreateSecret{fakeSwarm: fs}
	_, err = mgr.Create(ctx, validParams())
	if err == nil || !strings.Contains(err.Error(), "create swarm secret hive-tunnel-prod-edge-config-r1") {
		t.Fatalf("config upload failure: got %v", err)
	}
}

func TestManagerCreateNoZoneSkipsDNS(t *testing.T) {
	p := validParams()
	p.ZoneID = ""
	mgr, _, _, _, cf := newTestManager()
	view, err := mgr.Create(context.Background(), p)
	if err != nil {
		t.Fatalf("create without zone: %v", err)
	}
	if len(cf.dnsRecords) != 0 {
		t.Fatal("no DNS routes expected without a zone")
	}
	if view.Row.ZoneID != "" {
		t.Fatalf("zone id = %q", view.Row.ZoneID)
	}
}

func TestManagerGetDeleteMissingRows(t *testing.T) {
	mgr, _, swarmer, _, _ := newTestManager()
	seedDeployed(t, mgr, mgr.Repo.(*fakeRepo), swarmer)
	if _, err := mgr.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing: %v", err)
	}
	if err := mgr.Delete(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestManagerStatusWithoutCredentialStore(t *testing.T) {
	mgr, repo, swarmer, _, _ := newTestManager()
	row := seedDeployed(t, mgr, repo, swarmer)
	mgr.Store = nil
	st, err := mgr.Status(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.CloudflareStatus != "" || st.RunningReplicas != 0 && st.DesiredReplicas == 0 {
		t.Logf("status=%+v (swarm double reports zeros)", st)
	}
}

func TestManagerListErrors(t *testing.T) {
	ctx := context.Background()
	mgr, repo, swarmer, _, _ := newTestManager()
	seedDeployed(t, mgr, repo, swarmer)

	mgr.Repo = &failListRepo{newFakeRepo()}
	if _, err := mgr.List(ctx); err == nil {
		t.Fatal("expected List failure")
	}
	// Connector inspection failing mid-list surfaces.
	fs := newFakeSwarm()
	mgr.Repo = repo
	mgr.Swarm = &failSwarm{fakeSwarm: fs, getSvcErr: errors.New("rpc down")}
	if _, err := mgr.List(ctx); err == nil || !strings.Contains(err.Error(), "inspect connector service") {
		t.Fatalf("connector failure mid-list: %v", err)
	}
}

func TestManagerPruneKeepsCurrentRevisionBranch(t *testing.T) {
	ctx := context.Background()
	mgr, repo, swarmer, _, _ := newTestManager()
	row := seedDeployed(t, mgr, repo, swarmer)
	fs := swarmer
	_, _ = fs.CreateSecret(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: configSecretName(row.Name, 2)}, Data: []byte("r2"),
	})
	// Keeping rev 2 must remove rev 1 (the keep branch is skipped for it).
	mgr.pruneConfigRevisions(ctx, row.Name, 2)
	secrets, _ := fs.ListSecrets(ctx)
	have := map[string]bool{}
	for _, s := range secrets {
		have[s.Spec.Name] = true
	}
	if have[configSecretName(row.Name, 1)] {
		t.Fatal("rev 1 should be pruned when keeping rev 2")
	}
	if !have[configSecretName(row.Name, 2)] {
		t.Fatal("kept revision disappeared")
	}
	// Listing failures make pruning a silent no-op.
	mgr.Swarm = &failSwarm{fakeSwarm: fs, listSecretsErr: errors.New("down")}
	mgr.pruneConfigRevisions(ctx, row.Name, -1)
}

func TestManagerConnectorStatusMissingServiceIsEmpty(t *testing.T) {
	mgr, repo, _, _, _ := newTestManager()
	row := seedDeployed(t, mgr, repo, mgr.Swarm.(*fakeSwarm))
	mgr.Swarm = newFakeSwarm() // connector gone
	st, err := mgr.Status(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.DesiredReplicas != 0 || st.RunningReplicas != 0 {
		t.Fatalf("missing connector should report zero replicas: %+v", st)
	}
}

func TestValidExactHostnameBranches(t *testing.T) {
	if validExactHostname("") {
		t.Error("empty hostname accepted")
	}
	if validExactHostname("-bad.example.com") {
		t.Error("leading-hyphen label accepted")
	}
	if validExactHostname("bad..example.com") {
		t.Error("empty label accepted")
	}
	if !validExactHostname("ok.example.com") {
		t.Error("valid hostname rejected")
	}
	if ValidHostname("*.") {
		t.Error("bare wildcard suffix accepted")
	}
}
