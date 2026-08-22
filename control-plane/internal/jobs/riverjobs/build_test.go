package riverjobs

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	buildruntime "github.com/luke/hive/control-plane/internal/build"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// workCtx returns a context carrying a river client so Work can be invoked
// directly (river.ClientFromContext panics without one). The client never
// starts workers; its Insert calls just write river_job rows.
func workCtx(t *testing.T) context.Context {
	t.Helper()
	return rivertest.WorkContext[pgx.Tx](context.Background(), testdb.RiverClient(t))
}

func TestShortTag(t *testing.T) {
	tests := []struct {
		name    string
		sha     string
		buildID string
		want    string
	}{
		{name: "long sha truncated", sha: "abcdef1234567890", buildID: "b", want: "abcdef123456"},
		{name: "short sha kept", sha: "abc123", buildID: "b", want: "abc123"},
		{name: "exactly twelve kept", sha: "abcdef123456", buildID: "b", want: "abcdef123456"},
		{name: "no sha long build id", sha: "", buildID: "deadbeef-rest", want: "deadbeef"},
		{name: "no sha short build id", sha: "", buildID: "ab", want: "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortTag(tt.sha, tt.buildID); got != tt.want {
				t.Fatalf("shortTag(%q, %q) = %q, want %q", tt.sha, tt.buildID, got, tt.want)
			}
		})
	}
}

func TestUUIDRef(t *testing.T) {
	if got := uuidRef(pgtype.UUID{}); got != nil {
		t.Fatalf("uuidRef(invalid) = %v, want nil", *got)
	}
	id := uuid.New()
	got := uuidRef(pgtype.UUID{Bytes: id, Valid: true})
	if got == nil || *got != id.String() {
		t.Fatalf("uuidRef(valid) = %v, want %s", got, id.String())
	}
}

// isJobCancel reports whether err is a river job cancellation.
func isJobCancel(t *testing.T, err error) bool {
	t.Helper()
	var cancelErr *river.JobCancelError
	return errors.As(err, &cancelErr)
}

func TestBuildWorkerInvalidBuildID(t *testing.T) {
	testdb.Get(t)
	w := &BuildWorker{}
	err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
		Args: BuildJobArgs{BuildID: "not-a-uuid"},
	})
	if err == nil || !strings.Contains(err.Error(), `invalid build id "not-a-uuid"`) {
		t.Fatalf("expected invalid build id error, got %v", err)
	}
}

func TestBuildWorkerMissingBuild(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	w := &BuildWorker{Pool: pool}
	err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
		Args: BuildJobArgs{BuildID: uuid.NewString()},
	})
	if !isJobCancel(t, err) {
		t.Fatalf("expected JobCancel for missing build, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestBuildWorkerCancelledBeforeStart(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "", "", nil)
	buildID := seedBuildJob(t, appID, "cancelled", "manual")

	w := &BuildWorker{Pool: pool}
	err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
		Args: BuildJobArgs{BuildID: buildID},
	})
	if !isJobCancel(t, err) {
		t.Fatalf("expected JobCancel for cancelled build, got %v", err)
	}
}

func TestBuildWorkerUnknownSourceType(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)

	seedComposeApp := func(name string) (string, string) {
		p := testdb.Get(t)
		var appID, buildID string
		if err := p.QueryRow(context.Background(), `
			insert into applications(project_id, name, source_type)
			values ($1::uuid, $2, 'compose') returning id::text
		`, fixture.ProjectID, name).Scan(&appID); err != nil {
			t.Fatalf("seed compose app: %v", err)
		}
		buildID = seedBuildJob(t, appID, "queued", "manual")
		return appID, buildID
	}

	status := func(buildID string) string {
		var s string
		if err := pool.QueryRow(context.Background(),
			`select status::text from build_jobs where id=$1::uuid`, buildID).Scan(&s); err != nil {
			t.Fatalf("load build status: %v", err)
		}
		return s
	}

	t.Run("non-final attempt keeps the build running", func(t *testing.T) {
		_, buildID := seedComposeApp("compose-nonfinal")
		w := &BuildWorker{Pool: pool}
		err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
			Args:   BuildJobArgs{BuildID: buildID},
			JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 3},
		})
		if err == nil || !strings.Contains(err.Error(), "not buildable") {
			t.Fatalf("expected not-buildable error, got %v", err)
		}
		if got := status(buildID); got != "building" {
			t.Fatalf("status = %q, want building (untouched by markFailedIfFinal)", got)
		}
	})

	t.Run("final attempt marks failed", func(t *testing.T) {
		_, buildID2 := seedComposeApp("compose-final")
		w := &BuildWorker{Pool: pool}
		err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
			Args:   BuildJobArgs{BuildID: buildID2},
			JobRow: &rivertype.JobRow{Attempt: 3, MaxAttempts: 3},
		})
		if err == nil || !strings.Contains(err.Error(), "not buildable") {
			t.Fatalf("expected not-buildable error, got %v", err)
		}
		if got := status(buildID2); got != "failed" {
			t.Fatalf("status = %q, want failed", got)
		}
		var errMsg string
		if err := pool.QueryRow(context.Background(),
			`select coalesce(error_message,'') from build_jobs where id=$1::uuid`, buildID2).Scan(&errMsg); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(errMsg, "build: ") {
			t.Fatalf("error_message = %q, want 'build: ' prefix", errMsg)
		}
	})
}

func TestBuildWorkerImageSourceDirectDeploy(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := seedImageApp(t, fixture.ProjectID, "imgapp", "nginx:1.27")
	buildID := seedBuildJob(t, appID, "queued", "manual")

	swarm := newFakeSwarm()
	builder := &fakeBuilder{}
	w := &BuildWorker{Pool: pool, RegistryHost: "127.0.0.1:5000", Swarm: swarm, Buildkit: builder}
	err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
		Args: BuildJobArgs{BuildID: buildID},
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}

	var status, imageTag string
	if err := pool.QueryRow(context.Background(),
		`select status::text, coalesce(image_tag,'') from build_jobs where id=$1::uuid`, buildID).
		Scan(&status, &imageTag); err != nil {
		t.Fatal(err)
	}
	if status != "complete" || imageTag != "nginx:1.27" {
		t.Fatalf("build status=%q image_tag=%q, want complete/nginx:1.27", status, imageTag)
	}

	n := testdb.QueryCount(t, `select count(*) from deployments where application_id=$1::uuid and status='pending' and image_tag='nginx:1.27'`, appID)
	if n != 1 {
		t.Fatalf("pending deployments rows = %d, want 1", n)
	}
	if len(builder.requests) != 0 {
		t.Fatalf("image source must skip buildkit, got %d calls", len(builder.requests))
	}
	// The work context carries a river client: the pending deployment is
	// handed off with a deploy job row.
	m := testdb.QueryCount(t, `select count(*) from river_job where kind='deploy'`)
	if m != 1 {
		t.Fatalf("river deploy jobs = %d, want 1", m)
	}
}

func TestBuildWorkerGitHappyPathEndToEnd(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	repoDir := initLocalRepo(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "gitapp", repoDir, nil)
	linkSSHKey(t, appID, "gitapp-key", "E2EMATERIAL")
	buildID := seedBuildJob(t, appID, "queued", "manual")

	builder := &fakeBuilder{}
	bw := &BuildWorker{
		Pool:         pool,
		RegistryHost: "127.0.0.1:5000",
		Swarm:        newFakeSwarm(),
		Buildkit:     builder,
	}
	dw := &DeployWorker{Pool: pool, Swarm: newFakeSwarm()}
	workers := river.NewWorkers()
	river.AddWorker(workers, bw)
	river.AddWorker(workers, dw)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			QueueBuild:  {MaxWorkers: 1},
			QueueDeploy: {MaxWorkers: 2},
		},
		Workers: workers,
	})
	if err != nil {
		t.Fatalf("river.NewClient: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := client.Insert(ctx, BuildJobArgs{BuildID: buildID}, nil); err != nil {
		t.Fatalf("insert build job: %v", err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatalf("client.Start: %v", err)
	}
	defer func() { _ = client.Stop(ctx) }() //nolint:contextcheck // stop uses its own background context below

	deadline := time.Now().Add(30 * time.Second)
	var status, gitSha, imageTag string
	for {
		if err := pool.QueryRow(context.Background(), `
			select status::text, coalesce(git_sha,''), coalesce(image_tag,'')
			from build_jobs where id=$1::uuid`, buildID).Scan(&status, &gitSha, &imageTag); err != nil {
			t.Fatalf("poll build: %v", err)
		}
		if status == "complete" || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status != "complete" {
		t.Fatalf("build did not complete in time; status=%q git_sha=%q image_tag=%q", status, gitSha, imageTag)
	}
	if len(gitSha) < 7 || len(gitSha) > 12 {
		t.Fatalf("git_sha = %q, want a 7-12 char short sha", gitSha)
	}
	wantPrefix := "127.0.0.1:5000/" + fixture.ProjectID + "/gitapp:"
	if !strings.HasPrefix(imageTag, wantPrefix) || !strings.HasSuffix(imageTag, ":"+gitSha) {
		t.Fatalf("image_tag = %q, want %s<sha>", imageTag, wantPrefix)
	}
	req := builder.last(t)
	if req.ImageTag != imageTag {
		t.Fatalf("builder request image tag = %q, want %q", req.ImageTag, imageTag)
	}
	if req.Auth == nil || req.Auth.Host != "127.0.0.1:5000" {
		t.Fatalf("builder request auth = %+v, want host 127.0.0.1:5000", req.Auth)
	}
	// The cloned workdir is cleaned up by the time Work returns; assert the
	// recorded context path was populated and points at a hive build dir.
	if req.ContextPath == "" || !strings.Contains(req.ContextPath, "hive-build") {
		t.Fatalf("builder context path = %q, want a hive-build-* workdir", req.ContextPath)
	}
	n := testdb.QueryCount(t, `select count(*) from deployments where application_id=$1::uuid and image_tag=$2`, appID, imageTag)
	if n != 1 {
		t.Fatalf("deployment rows = %d, want 1", n)
	}
}

func TestBuildWorkerCloneFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)

	newCase := func(name string) string {
		appID := testdb.SeedApplication(t, fixture.ProjectID, name, filepath.Join(t.TempDir(), "does-not-exist"), nil)
		return seedBuildJob(t, appID, "queued", "manual")
	}

	run := func(buildID string, attempt int) error {
		w := &BuildWorker{Pool: pool, RegistryHost: "127.0.0.1:5000", Swarm: newFakeSwarm(), Buildkit: &fakeBuilder{}}
		return w.Work(workCtx(t), &river.Job[BuildJobArgs]{
			Args:   BuildJobArgs{BuildID: buildID},
			JobRow: &rivertype.JobRow{Attempt: attempt, MaxAttempts: 3},
		})
	}

	t.Run("non-final attempt", func(t *testing.T) {
		buildID := newCase("clonefail-nonfinal")
		err := run(buildID, 1)
		if err == nil || !strings.Contains(err.Error(), "clone") {
			t.Fatalf("expected clone error, got %v", err)
		}
		var status string
		if err := pool.QueryRow(context.Background(),
			`select status::text from build_jobs where id=$1::uuid`, buildID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "building" {
			t.Fatalf("status = %q, want building on non-final attempt", status)
		}
	})

	t.Run("final attempt", func(t *testing.T) {
		buildID := newCase("clonefail-final")
		if err := run(buildID, 3); err == nil {
			t.Fatal("expected clone failure")
		}
		var status, errMsg string
		if err := pool.QueryRow(context.Background(),
			`select status::text, coalesce(error_message,'') from build_jobs where id=$1::uuid`, buildID).
			Scan(&status, &errMsg); err != nil {
			t.Fatal(err)
		}
		if status != "failed" || !strings.HasPrefix(errMsg, "build: clone") {
			t.Fatalf("status=%q error=%q, want failed with 'build: clone' prefix", status, errMsg)
		}
	})
}

func TestBuildWorkerCancelledMidway(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	repoDir := initLocalRepo(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "cancelmid", repoDir, nil)
	buildID := seedBuildJob(t, appID, "queued", "manual")

	builder := &fakeBuilder{
		hook: func(_ buildruntime.Request) {
			// Simulate a user cancelling while the build is running.
			if _, err := pool.Exec(context.Background(),
				`update build_jobs set status='cancelled' where id=$1::uuid`, buildID); err != nil {
				t.Errorf("cancel build: %v", err)
			}
		},
	}
	w := &BuildWorker{Pool: pool, RegistryHost: "127.0.0.1:5000", Swarm: newFakeSwarm(), Buildkit: builder}
	err := w.Work(workCtx(t), &river.Job[BuildJobArgs]{
		Args:   BuildJobArgs{BuildID: buildID},
		JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 3},
	})
	if !isJobCancel(t, err) {
		t.Fatalf("expected JobCancel when cancelled mid-build, got %v", err)
	}
}

func TestBuildWorkerPeriodicFlush(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "flushapp", "", nil)
	buildID := seedBuildJob(t, appID, "building", "manual")

	lw := NewThrottledLogWriter(dbgen.New(pool), buildID)
	lw.minInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	stop := periodicFlush(ctx, lw, 10*time.Millisecond)
	if _, err := lw.Write([]byte("hello flush\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // let at least one tick fire Flush
	cancel()
	stop()

	var logs string
	if err := pool.QueryRow(context.Background(),
		`select logs from build_jobs where id=$1::uuid`, buildID).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs, "hello flush") {
		t.Fatalf("logs = %q, want flushed content", logs)
	}

	// The ctx.Done return path: start with an already-cancelled context and
	// invoke the returned stop func; both paths must terminate.
	cancelledCtx, cancelledCancel := context.WithCancel(context.Background())
	cancelledCancel()
	done := make(chan struct{})
	go func() {
		s := periodicFlush(cancelledCtx, lw, time.Millisecond)
		close(done)
		s()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("periodicFlush goroutines did not terminate")
	}
}
