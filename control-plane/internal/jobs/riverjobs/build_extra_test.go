package riverjobs

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	buildruntime "github.com/luke/hive/control-plane/internal/build"
	"github.com/luke/hive/control-plane/internal/notify"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// rawGitAppWithRegistry inserts a git application pinned to the given
// registry id (possibly nonexistent).
func rawGitAppWithRegistry(t *testing.T, projectID, name, repoURL, registryID string) string {
	t.Helper()
	p := testdb.Get(t)
	var appID string
	if err := p.QueryRow(context.Background(), `
		insert into applications(project_id, name, source_type, repository_url, registry_id)
		values ($1::uuid, $2, 'git', $3, $4::uuid)
		returning id::text
	`, projectID, name, repoURL, registryID).Scan(&appID); err != nil {
		t.Fatalf("rawGitAppWithRegistry: %v", err)
	}
	return appID
}

func TestBuildWorkerRollbackTriggerUsesRequestedTag(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := seedImageApp(t, fixture.ProjectID, "rollbackapp", "current:2")
	p := testdb.Get(t)
	var buildID string
	if err := p.QueryRow(context.Background(), `
		insert into build_jobs(application_id, trigger, image_tag)
		values ($1::uuid, 'rollback', 'previous:1')
		returning id::text
	`, appID).Scan(&buildID); err != nil {
		t.Fatalf("seed rollback build: %v", err)
	}

	w := &BuildWorker{Pool: pool, RegistryHost: "127.0.0.1:5000", Swarm: newFakeSwarm(), Buildkit: &fakeBuilder{}}
	if err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{Args: BuildJobArgs{BuildID: buildID}}); err != nil {
		t.Fatalf("Work: %v", err)
	}

	n := testdb.QueryCount(t,
		`select count(*) from deployments where application_id=$1::uuid and status='pending' and image_tag='previous:1'`, appID)
	if n != 1 {
		t.Fatalf("pending deployments with requested tag = %d, want 1", n)
	}
	var status string
	if err := pool.QueryRow(context.Background(),
		`select status::text from build_jobs where id=$1::uuid`, buildID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "complete" {
		t.Fatalf("build status = %q, want complete", status)
	}
}

func TestBuildWorkerImageSourceNotifiesOnSuccess(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := seedImageApp(t, fixture.ProjectID, "notifyimg", "nginx:1")
	buildID := seedBuildJob(t, appID, "queued", "manual")

	w := &BuildWorker{
		Pool:         pool,
		RegistryHost: "127.0.0.1:5000",
		Swarm:        newFakeSwarm(),
		Buildkit:     &fakeBuilder{},
		Notifier:     notify.NewDispatcher(pool),
	}
	if err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{Args: BuildJobArgs{BuildID: buildID}}); err != nil {
		t.Fatalf("Work: %v", err)
	}
}

func TestBuildWorkerBuildkitFailureMarksFailed(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	repoDir := initLocalRepo(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "bkfail", repoDir, nil)
	buildID := seedBuildJob(t, appID, "queued", "manual")

	w := &BuildWorker{
		Pool:         pool,
		RegistryHost: "127.0.0.1:5000",
		Swarm:        newFakeSwarm(),
		Buildkit:     &fakeBuilder{err: context.DeadlineExceeded},
		Notifier:     notify.NewDispatcher(pool),
	}
	err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
		Args:   BuildJobArgs{BuildID: buildID},
		JobRow: &rivertype.JobRow{Attempt: 3, MaxAttempts: 3},
	})
	if err == nil || !strings.Contains(err.Error(), "build and push") {
		t.Fatalf("expected build-and-push failure, got %v", err)
	}
	var status, errMsg string
	if err := pool.QueryRow(context.Background(),
		`select status::text, coalesce(error_message,'') from build_jobs where id=$1::uuid`, buildID).
		Scan(&status, &errMsg); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.HasPrefix(errMsg, "build: build and push") {
		t.Fatalf("status=%q error=%q, want failed with 'build: build and push' prefix", status, errMsg)
	}
}

func TestBuildWorkerRegistryResolveFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	repoDir := initLocalRepo(t)
	p := testdb.Get(t)
	var regID string
	if err := p.QueryRow(context.Background(), `
		insert into registries(name, url, secret_name) values ($1, 'https://missing.registry.invalid', 'ghost-registry-secret')
		returning id::text
	`, "ghost-"+uuid.NewString()[:8]).Scan(&regID); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	appID := rawGitAppWithRegistry(t, fixture.ProjectID, "badreg", repoDir, regID)
	buildID := seedBuildJob(t, appID, "queued", "manual")

	w := &BuildWorker{Pool: pool, RegistryHost: "127.0.0.1:5000", Swarm: newFakeSwarm(), Buildkit: &fakeBuilder{}}
	err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
		Args:   BuildJobArgs{BuildID: buildID},
		JobRow: &rivertype.JobRow{Attempt: 3, MaxAttempts: 3},
	})
	if err == nil || !strings.Contains(err.Error(), "references secret") {
		t.Fatalf("expected registry resolution failure, got %v", err)
	}
}

func TestBuildWorkerSSHKeyMaterializeFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	repoDir := initLocalRepo(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "keyfail", repoDir, nil)
	linkSSHKey(t, appID, "empty-key", "")
	buildID := seedBuildJob(t, appID, "queued", "manual")

	w := &BuildWorker{Pool: pool, RegistryHost: "127.0.0.1:5000", Swarm: newFakeSwarm(), Buildkit: &fakeBuilder{}}
	err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
		Args:   BuildJobArgs{BuildID: buildID},
		JobRow: &rivertype.JobRow{Attempt: 3, MaxAttempts: 3},
	})
	if err == nil || !strings.Contains(err.Error(), "no private key material") {
		t.Fatalf("expected ssh key failure, got %v", err)
	}
}

func TestBuildWorkerEnqueueDeployJobFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := seedImageApp(t, fixture.ProjectID, "deadq", "nginx:9")
	buildID := seedBuildJob(t, appID, "queued", "manual")

	// A river client on a closed pool makes the in-worker deploy enqueue
	// fail while the shared pool keeps serving everything else.
	deadClient, err := river.NewClient(riverpgxv5.New(deadPoolForTest(t)), &river.Config{})
	if err != nil {
		t.Fatalf("river.NewClient: %v", err)
	}
	ctx := riverWorkContext(t, deadClient)

	w := &BuildWorker{Pool: pool, RegistryHost: "127.0.0.1:5000", Swarm: newFakeSwarm(), Buildkit: &fakeBuilder{}}
	err = w.Work(ctx, &river.Job[BuildJobArgs]{
		Args:   BuildJobArgs{BuildID: buildID},
		JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 3},
	})
	if err == nil || !strings.Contains(err.Error(), "enqueue deploy job") {
		t.Fatalf("expected deploy enqueue failure, got %v", err)
	}
	var status string
	if err := pool.QueryRow(context.Background(),
		`select status::text from build_jobs where id=$1::uuid`, buildID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "building" {
		t.Fatalf("build status = %q, want building (completion never reached)", status)
	}
}

func TestBuildWorkerCheckCancelledErrorBranch(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	repoDir := initLocalRepo(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "ctxkill", repoDir, nil)
	buildID := seedBuildJob(t, appID, "queued", "manual")

	ctx, cancel := context.WithCancel(context.Background())
	builder := &fakeBuilder{hook: func(_ buildruntime.Request) { cancel() }}
	w := &BuildWorker{Pool: pool, RegistryHost: "127.0.0.1:5000", Swarm: newFakeSwarm(), Buildkit: builder}
	err := w.Work(ctx, &river.Job[BuildJobArgs]{
		Args:   BuildJobArgs{BuildID: buildID},
		JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 3},
	})
	if err == nil || !strings.Contains(err.Error(), "check build status") {
		t.Fatalf("expected check-status failure after ctx cancel, got %v", err)
	}
	cancel()
}
