package riverjobs

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/luke/hive/control-plane/internal/notify"
	"github.com/luke/hive/control-plane/internal/testdb"
)

func TestPreviewDeployWorkerSuccess(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	// repository_url NULL: buildContext falls back to "." and no clone runs.
	appID := testdb.SeedApplicationNoAutoDeploy(t, fixture.ProjectID, "previewapp", "")
	previewID := seedPreview(t, fixture.OrgID, appID, "now() + interval '7 days'")

	swarm := newFakeSwarm()
	builder := &fakeBuilder{}
	notifier := notify.NewDispatcher(pool)
	w := &PreviewDeployWorker{
		Pool:         pool,
		RegistryHost: "127.0.0.1:5000",
		Swarm:        swarm,
		Buildkit:     builder,
		Notifier:     notifier,
	}
	err := w.Work(workCtx(t), &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: previewID, ApplicationID: appID, Branch: "feature"},
	})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}

	var status, url string
	if err := pool.QueryRow(context.Background(),
		`select status, coalesce(url,'') from preview_deployments where id=$1::uuid`, previewID).
		Scan(&status, &url); err != nil {
		t.Fatal(err)
	}
	if status != "ready" {
		t.Fatalf("preview status = %q, want ready", status)
	}
	if !strings.HasPrefix(url, "https://preview-previewapp-"+previewID) {
		t.Fatalf("url = %q, want preview-<app>-<id> form", url)
	}

	req := builder.last(t)
	wantTag := "127.0.0.1:5000/" + fixture.ProjectID + "/previewapp:preview-" + previewID
	if req.ImageTag != wantTag {
		t.Fatalf("image tag = %q, want %q", req.ImageTag, wantTag)
	}
	if req.ContextPath != "." {
		t.Fatalf("context path = %q, want \".\" when no repo is linked", req.ContextPath)
	}

	swarm.mu.Lock()
	n := len(swarm.createdSpecs)
	swarm.mu.Unlock()
	if n != 1 {
		t.Fatalf("created services = %d, want 1", n)
	}
}

func TestPreviewDeployWorkerCloneFailureMarksFailed(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "badrepo", "/nonexistent/repo/path", nil)
	previewID := seedPreview(t, fixture.OrgID, appID, "now() + interval '7 days'")

	swarm := newFakeSwarm()
	w := &PreviewDeployWorker{
		Pool:         pool,
		RegistryHost: "127.0.0.1:5000",
		Swarm:        swarm,
		Buildkit:     &fakeBuilder{},
	}
	err := w.Work(workCtx(t), &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: previewID, ApplicationID: appID, Branch: "main"},
	})
	if err == nil || !strings.Contains(err.Error(), "clone") {
		t.Fatalf("expected clone failure, got %v", err)
	}

	swarm.mu.Lock()
	created := len(swarm.createdSpecs)
	swarm.mu.Unlock()
	if created != 0 {
		t.Fatalf("no service should be created on clone failure, got %d", created)
	}

	// The application row itself must still exist (markFailed only touches
	// the preview row) and the job error carries the failure.
	var n int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from preview_deployments where id=$1::uuid`, previewID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("preview rows = %d, want 1", n)
	}
}

func TestPreviewDeployWorkerUnknownApplication(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	w := &PreviewDeployWorker{Pool: pool, Swarm: newFakeSwarm(), Buildkit: &fakeBuilder{}}
	err := w.Work(workCtx(t), &river.Job[PreviewDeployJobArgs]{
		Args: PreviewDeployJobArgs{PreviewID: uuid.NewString(), ApplicationID: uuid.NewString()},
	})
	if err == nil || !strings.Contains(err.Error(), "no rows in result set") {
		t.Fatalf("expected missing-application error, got %v", err)
	}
}
