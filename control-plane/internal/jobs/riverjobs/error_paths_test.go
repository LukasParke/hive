package riverjobs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	dockerswarm "github.com/moby/moby/api/types/swarm"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	buildruntime "github.com/luke/hive/control-plane/internal/build"
	"github.com/luke/hive/control-plane/internal/notify"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// freshPoolForTest returns an extra live pool on the shared database so a
// test can close it mid-flight without disturbing other tests.
func freshPoolForTest(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(testdb.Get(t).Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatalf("parse conn string: %v", err)
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	return p
}

// seedGhostRegistry inserts a registry whose credentials cannot be resolved.
func seedGhostRegistry(t *testing.T) string {
	t.Helper()
	p := testdb.Get(t)
	var regID string
	if err := p.QueryRow(context.Background(), `
		insert into registries(name, url, secret_name) values ($1, 'https://missing.registry.invalid', 'ghost-registry-secret')
		returning id::text
	`, "ghost-"+uuid.NewString()[:8]).Scan(&regID); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	return regID
}

func TestDomainLookupErrorBranches(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	lookup := domainLookup(context.Background(), pool, "")
	hosts, err := lookup(context.Background(), "not-a-uuid")
	if err != nil || hosts != nil {
		t.Fatalf("unparseable id = (%v, %v), want (nil, nil)", hosts, err)
	}

	deadLookup := domainLookup(context.Background(), deadPoolForTest(t), "")
	if _, err := deadLookup(context.Background(), uuid.NewString()); err == nil {
		t.Fatal("expected query error against closed pool")
	}
}

func TestEnqueueBuildInsertError(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	_, err := EnqueueBuild(context.Background(), nil, deadPoolForTest(t), uuid.NewString(), "manual", "")
	if err == nil || !strings.Contains(err.Error(), "insert build job") {
		t.Fatalf("expected insert failure, got %v", err)
	}
}

func TestPreviewCleanupWorkerExecErrorLogged(t *testing.T) {
	w := &PreviewCleanupWorker{Pool: deadPoolForTest(t), Swarm: newFakeSwarm()}
	if err := w.Work(context.Background(), &river.Job[PreviewCleanupJobArgs]{}); err != nil {
		t.Fatalf("Work = %v, want nil (errors are logged)", err)
	}
}

func TestCleanupWorkerPruneAndListErrors(t *testing.T) {
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "errpaths", "", nil)
	expired := seedPreview(t, fixture.OrgID, appID, "now() - interval '72 hours'")

	// Closed pool: prune and orphan listing both fail and are logged.
	w := &CleanupWorker{Pool: deadPoolForTest(t), Swarm: newFakeSwarm()}
	if err := w.Work(context.Background(), &river.Job[CleanupJobArgs]{}); err != nil {
		t.Fatalf("Work = %v, want nil", err)
	}

	// ListServices failure inside service removal is swallowed.
	sw := &listErrSwarm{fakeSwarm: newFakeSwarm(), err: errors.New("list down")}
	w2 := &CleanupWorker{Pool: testdb.Get(t), Swarm: sw}
	if err := w2.Work(context.Background(), &river.Job[CleanupJobArgs]{}); err != nil {
		t.Fatalf("Work = %v, want nil", err)
	}
	if got := testdb.QueryCount(t, `select count(*) from preview_deployments where id=$1::uuid`, expired); got != 0 {
		t.Fatalf("orphan preview must still be deleted when listing fails")
	}
}

func TestCleanupWorkerDeleteErrorViaClosedPool(t *testing.T) {
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "delfail", "", nil)
	previewID := seedPreview(t, fixture.OrgID, appID, "now() - interval '72 hours'")

	p2 := freshPoolForTest(t)
	swarm := newFakeSwarm()
	var once bool
	swarm.onRemove = func() {
		if !once {
			once = true
			p2.Close()
		}
	}
	swarm.addService(map[string]string{"hive.app.id": previewID})
	w := &CleanupWorker{Pool: p2, Swarm: swarm}
	if err := w.Work(context.Background(), &river.Job[CleanupJobArgs]{}); err != nil {
		t.Fatalf("Work = %v, want nil (delete error is logged)", err)
	}
}

func TestDeployWorkerMarkDeployedError(t *testing.T) {
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "markdeployed", "", nil)
	depID := seedDeployment(t, appID, "img", "pending", "manual")

	p2 := freshPoolForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	swarm := newFakeSwarm()
	swarm.onUpdate = cancel // cancel after the service update succeeds
	// Pre-existing service with the app label: Application takes the update path.
	svcID := swarm.addService(map[string]string{"hive.app.id": appID})
	_ = svcID

	w := &DeployWorker{Pool: p2, Swarm: swarm}
	err := w.Work(ctx, &river.Job[DeployJobArgs]{Args: DeployJobArgs{DeploymentID: depID}})
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected mark-deployed failure, got %v", err)
	}
	cancel()
	p2.Close()
}

func TestPreviewDeployWorkerBuildkitFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplicationNoAutoDeploy(t, fixture.ProjectID, "prevbkfail", "")
	previewID := seedPreview(t, fixture.OrgID, appID, "now() + interval '7 days'")

	w := &PreviewDeployWorker{
		Pool:         pool,
		RegistryHost: "r:5000",
		Swarm:        newFakeSwarm(),
		Buildkit:     &fakeBuilder{err: errors.New("buildkit exploded")},
	}
	err := w.Work(context.Background(), &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: previewID, ApplicationID: appID},
	})
	if err == nil || !strings.Contains(err.Error(), "buildkit exploded") {
		t.Fatalf("expected buildkit failure, got %v", err)
	}
}

func TestDeployWorkerLoadDeploymentError(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	w := &DeployWorker{Pool: deadPoolForTest(t), Swarm: newFakeSwarm()}
	err := w.Work(workCtx(t), &river.Job[DeployJobArgs]{Args: DeployJobArgs{DeploymentID: uuid.NewString()}})
	if err == nil || !strings.Contains(err.Error(), "load deployment") {
		t.Fatalf("expected load failure, got %v", err)
	}
}

func TestDeployWorkerProjectLoadErrorAfterCreate(t *testing.T) {
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "ctxkill-deploy", "", nil)
	depID := seedDeployment(t, appID, "img", "pending", "manual")

	p2 := freshPoolForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	swarm := newFakeSwarm()
	swarm.onCreate = cancel
	w := &DeployWorker{Pool: p2, Swarm: swarm}
	err := w.Work(ctx, &river.Job[DeployJobArgs]{Args: DeployJobArgs{DeploymentID: depID}})
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected post-create failure, got %v", err)
	}
	cancel()
	p2.Close()
}

func TestPreviewDeployWorkerCloneSuccessWithKey(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	repoDir := initLocalRepo(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "prevclone", repoDir, nil)
	linkSSHKey(t, appID, "prev-key", "PREVMATERIAL")
	previewID := seedPreview(t, fixture.OrgID, appID, "now() + interval '7 days'")

	builder := &fakeBuilder{}
	w := &PreviewDeployWorker{
		Pool:         pool,
		RegistryHost: "127.0.0.1:5000",
		Swarm:        newFakeSwarm(),
		Buildkit:     builder,
	}
	if err := w.Work(context.Background(), &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: previewID, ApplicationID: appID, Branch: "main"},
	}); err != nil {
		t.Fatalf("Work: %v", err)
	}
	req := builder.last(t)
	if !strings.Contains(req.ContextPath, "hive-build") {
		t.Fatalf("context path = %q, want cloned workdir", req.ContextPath)
	}
}

func TestPreviewDeployWorkerRegistryFailureNotifies(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	regID := seedGhostRegistry(t)
	appID := rawGitAppWithRegistry(t, fixture.ProjectID, "prevbadreg", "", regID)
	previewID := seedPreview(t, fixture.OrgID, appID, "now() + interval '7 days'")

	w := &PreviewDeployWorker{
		Pool:         pool,
		RegistryHost: "127.0.0.1:5000",
		Swarm:        newFakeSwarm(),
		Buildkit:     &fakeBuilder{},
		Notifier:     notify.NewDispatcher(pool),
	}
	err := w.Work(context.Background(), &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: previewID, ApplicationID: appID},
	})
	if err == nil || !strings.Contains(err.Error(), "references secret") {
		t.Fatalf("expected registry resolution failure, got %v", err)
	}
}

func TestPreviewDeployWorkerKeyMaterialFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "prevkeyfail", "/nonexistent/repo", nil)
	linkSSHKey(t, appID, "ghost-prev-key", "")
	previewID := seedPreview(t, fixture.OrgID, appID, "now() + interval '7 days'")

	w := &PreviewDeployWorker{Pool: pool, RegistryHost: "r:5000", Swarm: newFakeSwarm(), Buildkit: &fakeBuilder{}}
	err := w.Work(context.Background(), &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: previewID, ApplicationID: appID},
	})
	if err == nil || !strings.Contains(err.Error(), "no private key material") {
		t.Fatalf("expected ssh key failure, got %v", err)
	}
}

func TestPreviewDeployWorkerSwarmCreateFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplicationNoAutoDeploy(t, fixture.ProjectID, "prevswarmfail", "")
	previewID := seedPreview(t, fixture.OrgID, appID, "now() + interval '7 days'")

	swarm := newFakeSwarm()
	swarm.createErr = errors.New("swarm down")
	w := &PreviewDeployWorker{Pool: pool, RegistryHost: "r:5000", Swarm: swarm, Buildkit: &fakeBuilder{}}
	err := w.Work(context.Background(), &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: previewID, ApplicationID: appID},
	})
	if err == nil || !strings.Contains(err.Error(), "swarm down") {
		t.Fatalf("expected swarm failure, got %v", err)
	}
}

func TestPreviewDeployWorkerLoadEnvVarsFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplicationNoAutoDeploy(t, fixture.ProjectID, "prevenvfail", "")
	previewID := seedPreview(t, fixture.OrgID, appID, "now() + interval '7 days'")

	ctx, cancel := context.WithCancel(context.Background())
	builder := &fakeBuilder{hook: func(buildruntime.Request) { cancel() }}
	w := &PreviewDeployWorker{Pool: pool, RegistryHost: "r:5000", Swarm: newFakeSwarm(), Buildkit: builder}
	err := w.Work(ctx, &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: previewID, ApplicationID: appID},
	})
	if err == nil {
		t.Fatal("expected env var load failure after ctx cancel")
	}
	cancel()
}

func TestPreviewDeployWorkerResolveAppServiceErrorStillSucceeds(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplicationNoAutoDeploy(t, fixture.ProjectID, "prevsvc", "")
	previewID := seedPreview(t, fixture.OrgID, appID, "now() + interval '7 days'")

	// ListServices succeeds while the application service is created, then
	// fails when domain routing resolves the same service again.
	sw := &failLaterSwarm{fakeSwarm: newFakeSwarm()}
	w := &PreviewDeployWorker{Pool: pool, RegistryHost: "r:5000", Swarm: sw, Buildkit: &fakeBuilder{}}
	if err := w.Work(context.Background(), &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: previewID, ApplicationID: appID},
	}); err != nil {
		t.Fatalf("Work: %v", err)
	}
	var status string
	if err := pool.QueryRow(context.Background(),
		`select status from preview_deployments where id=$1::uuid`, previewID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "ready" {
		t.Fatalf("preview status = %q, want ready despite domain apply skip", status)
	}
}

func TestLookupAppSSHKeyLoadError(t *testing.T) {
	testdb.Get(t)
	_, _, err := lookupAppSSHKey(context.Background(), deadPoolForTest(t), uuid.NewString())
	if err == nil || !strings.Contains(err.Error(), "load ssh key") {
		t.Fatalf("expected load failure, got %v", err)
	}
}

func TestWriteSSHKeyFileMkdirTempFailure(t *testing.T) {
	t.Setenv("TMPDIR", "/nonexistent-hive-tmp-dir")
	_, err := writeSSHKeyFile(context.Background(), "k", "material")
	if err == nil || !strings.Contains(err.Error(), "create ssh key temp dir") {
		t.Fatalf("expected temp dir failure, got %v", err)
	}
}

// failLaterSwarm fails ListServices from its second call onward.
type failLaterSwarm struct {
	*fakeSwarm
	calls int
}

func (f *failLaterSwarm) ListServices(ctx context.Context) ([]dockerswarm.Service, error) {
	f.calls++
	if f.calls >= 2 {
		return nil, errors.New("list down")
	}
	return f.fakeSwarm.ListServices(ctx)
}

func TestPreviewDeployWorkerFinalUpdateError(t *testing.T) {
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplicationNoAutoDeploy(t, fixture.ProjectID, "prevfinalfail", "")
	previewID := seedPreview(t, fixture.OrgID, appID, "now() + interval '7 days'")

	pool := testdb.Get(t)
	ctx, cancel := context.WithCancel(context.Background())
	swarm := newFakeSwarm()
	swarm.onCreate = cancel // service created fine; the final flip sees a dead ctx
	w := &PreviewDeployWorker{Pool: pool, RegistryHost: "r:5000", Swarm: swarm, Buildkit: &fakeBuilder{}}
	err := w.Work(ctx, &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: previewID, ApplicationID: appID},
	})
	if err == nil {
		t.Fatal("expected final update failure, got nil")
	}
	cancel()
}

func TestBuildWorkerLoadBuildError(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	w := &BuildWorker{Pool: deadPoolForTest(t)}
	err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
		Args:   BuildJobArgs{BuildID: uuid.NewString()},
		JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 3},
	})
	if err == nil || !strings.Contains(err.Error(), "load build") {
		t.Fatalf("expected load failure, got %v", err)
	}
}
