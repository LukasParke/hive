package riverjobs

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	dockernet "github.com/moby/moby/api/types/network"
	dockerswarm "github.com/moby/moby/api/types/swarm"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertest"

	buildruntime "github.com/luke/hive/control-plane/internal/build"
	"github.com/luke/hive/control-plane/internal/deploy"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// fakeBuilder is a recording buildruntime.Builder fake.
type fakeBuilder struct {
	mu       sync.Mutex
	requests []buildruntime.Request
	hook     func(req buildruntime.Request)
	err      error
}

func (f *fakeBuilder) BuildAndPush(_ context.Context, req buildruntime.Request, _ io.Writer) error {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	hook := f.hook
	err := f.err
	f.mu.Unlock()
	if hook != nil {
		hook(req)
	}
	return err
}

// last returns the most recently recorded request.
func (f *fakeBuilder) last(t *testing.T) buildruntime.Request {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("fakeBuilder: no requests recorded")
	}
	return f.requests[len(f.requests)-1]
}

// fakeFanout is a recording deploy.Emitter fake.
type fakeFanout struct {
	mu       sync.Mutex
	messages [][2]string // channel, payload pairs
}

func (f *fakeFanout) Emit(_ context.Context, channel, payload string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, [2]string{channel, payload})
	return nil
}

func (f *fakeFanout) got() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string(nil), f.messages...)
}

// fakeRenewer is a recording CertRenewer fake.
type fakeRenewer struct {
	calls int
	err   error
}

func (f *fakeRenewer) RenewControlPlaneCert(context.Context) error {
	f.calls++
	return f.err
}

// fakeSwarm is an in-memory deploy.SwarmStack fake: services and networks
// live in slices keyed by generated ids, and every mutating call is recorded.
type fakeSwarm struct {
	mu        sync.Mutex
	services  []dockerswarm.Service
	networks  []dockernet.Summary
	nextID    int
	createErr error
	onCreate  func() // fires inside CreateService before returning
	onUpdate  func() // fires inside UpdateService before returning
	onRemove  func() // fires inside RemoveService before returning

	createdSpecs    []dockerswarm.ServiceSpec
	updatedIDs      []string
	removedIDs      []string
	createdNetworks []string
}

var _ deploy.SwarmStack = (*fakeSwarm)(nil)

func newFakeSwarm() *fakeSwarm { return &fakeSwarm{} }

// addService registers a pre-existing service carrying the given labels.
func (f *fakeSwarm) addService(labels map[string]string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("svc-%d", f.nextID)
	f.services = append(f.services, dockerswarm.Service{
		ID:   id,
		Meta: dockerswarm.Meta{Version: dockerswarm.Version{Index: 1}},
		Spec: dockerswarm.ServiceSpec{
			Annotations: dockerswarm.Annotations{Name: "preexisting-" + id, Labels: labels},
		},
	})
	return id
}

func (f *fakeSwarm) ListServices(context.Context) ([]dockerswarm.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]dockerswarm.Service, len(f.services))
	copy(out, f.services)
	return out, nil
}

func (f *fakeSwarm) CreateService(_ context.Context, spec dockerswarm.ServiceSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return "", f.createErr
	}
	hook := f.onCreate
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	f.nextID++
	id := fmt.Sprintf("svc-%d", f.nextID)
	f.createdSpecs = append(f.createdSpecs, spec)
	f.services = append(f.services, dockerswarm.Service{
		ID:   id,
		Meta: dockerswarm.Meta{Version: dockerswarm.Version{Index: 1}},
		Spec: spec,
	})
	return id, nil
}

func (f *fakeSwarm) UpdateService(_ context.Context, id string, version uint64, spec dockerswarm.ServiceSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.services {
		if f.services[i].ID == id {
			spec.Name = f.services[i].Spec.Name
			f.services[i].Spec = spec
			f.services[i].Version.Index = version + 1
			break
		}
	}
	hook := f.onUpdate
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	f.updatedIDs = append(f.updatedIDs, id)
	return nil
}

func (f *fakeSwarm) RemoveService(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.services {
		if f.services[i].ID == id {
			f.services = append(f.services[:i], f.services[i+1:]...)
			break
		}
	}
	hook := f.onRemove
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	f.removedIDs = append(f.removedIDs, id)
	return nil
}

func (f *fakeSwarm) ListNetworks(context.Context) ([]dockernet.Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]dockernet.Summary, len(f.networks))
	copy(out, f.networks)
	return out, nil
}

func (f *fakeSwarm) CreateNetwork(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("net-%d", f.nextID)
	f.networks = append(f.networks, dockernet.Summary{Network: dockernet.Network{ID: id, Name: name}})
	f.createdNetworks = append(f.createdNetworks, name)
	return id, nil
}

func (f *fakeSwarm) ListSecrets(context.Context) ([]dockerswarm.Secret, error) {
	return nil, nil
}

func (f *fakeSwarm) CreateSecret(context.Context, dockerswarm.SecretSpec) (string, error) {
	return "", nil
}

func (f *fakeSwarm) ListConfigs(context.Context) ([]dockerswarm.Config, error) {
	return nil, nil
}

func (f *fakeSwarm) CreateConfig(context.Context, dockerswarm.ConfigSpec) (string, error) {
	return "", nil
}

func (f *fakeSwarm) UpdateConfig(context.Context, string, uint64, dockerswarm.ConfigSpec) error {
	return nil
}

// seedImageApp seeds an application whose source is a prebuilt image.
func seedImageApp(t *testing.T, projectID, name, image string) string {
	t.Helper()
	p := testdb.Get(t)
	var appID string
	if err := p.QueryRow(context.Background(), `
		insert into applications(project_id, name, source_type, image)
		values ($1::uuid, $2, 'image', $3)
		returning id::text
	`, projectID, name, image).Scan(&appID); err != nil {
		t.Fatalf("seedImageApp: %v", err)
	}
	return appID
}

// seedBuildJob inserts a build_jobs row and returns its id.
func seedBuildJob(t *testing.T, appID, status, trigger string) string {
	t.Helper()
	p := testdb.Get(t)
	var buildID string
	if err := p.QueryRow(context.Background(), `
		insert into build_jobs(application_id, status, trigger)
		values ($1::uuid, $2, $3)
		returning id::text
	`, appID, status, trigger).Scan(&buildID); err != nil {
		t.Fatalf("seedBuildJob: %v", err)
	}
	return buildID
}

// seedDeployment inserts a deployments row and returns its id.
func seedDeployment(t *testing.T, appID, imageTag, status, trigger string) string {
	t.Helper()
	p := testdb.Get(t)
	var depID string
	if err := p.QueryRow(context.Background(), `
		insert into deployments(application_id, image_tag, status, trigger)
		values ($1::uuid, $2, $3, $4)
		returning id::text
	`, appID, imageTag, status, trigger).Scan(&depID); err != nil {
		t.Fatalf("seedDeployment: %v", err)
	}
	return depID
}

// seedPreview inserts a preview_deployments row with the given expiry SQL
// expression (e.g. "now() + interval '7 days'") and returns its id.
func seedPreview(t *testing.T, orgID, appID, expiresAt string) string {
	t.Helper()
	p := testdb.Get(t)
	var previewID string
	if err := p.QueryRow(context.Background(), `
		insert into preview_deployments(organization_id, application_id, pr_number, branch, status, expires_at)
		values ($1::uuid, $2::uuid, 7, 'feature', 'building', `+expiresAt+`)
		returning id::text
	`, orgID, appID).Scan(&previewID); err != nil {
		t.Fatalf("seedPreview: %v", err)
	}
	return previewID
}

// initLocalRepo creates a throwaway git repository with one commit on main
// so clone-based workers can be exercised without network access.
func initLocalRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...) //nolint:gosec // test fixture
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

// riverWorkContext wraps a client into a worker-style context so Work can
// be invoked directly while rc.Insert paths execute against that client.
func riverWorkContext(t *testing.T, client *river.Client[pgx.Tx]) context.Context {
	t.Helper()
	return rivertest.WorkContext[pgx.Tx](context.Background(), client)
}

// listErrSwarm makes ListServices fail while every other call delegates.
type listErrSwarm struct {
	*fakeSwarm
	err error
}

func (l *listErrSwarm) ListServices(context.Context) ([]dockerswarm.Service, error) {
	return nil, l.err
}
