package riverjobs

import (
	"context"
	"testing"

	"github.com/riverqueue/river"

	"github.com/luke/hive/control-plane/internal/testdb"
)

func TestCleanupWorkerNilPool(t *testing.T) {
	w := &CleanupWorker{}
	if err := w.Work(context.Background(), &river.Job[CleanupJobArgs]{}); err != nil {
		t.Fatalf("Work with nil pool = %v, want nil", err)
	}
}

func TestCleanupWorkerPrunesBuildHistory(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appA := testdb.SeedApplication(t, fixture.ProjectID, "app-a", "", nil)
	appB := testdb.SeedApplication(t, fixture.ProjectID, "app-b", "", nil)

	// 503 completed builds for A, 2 for B. Stagger created_at so the newest
	// 500 are well-defined.
	if _, err := pool.Exec(context.Background(), `
		insert into build_jobs(application_id, status, trigger, created_at)
		select $1::uuid, 'complete', 'manual', now() - (i || ' seconds')::interval
		from generate_series(1, 503) i
	`, appA); err != nil {
		t.Fatalf("seed builds A: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into build_jobs(application_id, status, trigger, created_at)
		select $1::uuid, 'complete', 'manual', now() - (i || ' seconds')::interval
		from generate_series(1, 2) i
	`, appB); err != nil {
		t.Fatalf("seed builds B: %v", err)
	}

	w := &CleanupWorker{Pool: pool, Swarm: newFakeSwarm()}
	if err := w.Work(context.Background(), &river.Job[CleanupJobArgs]{}); err != nil {
		t.Fatalf("Work: %v", err)
	}

	if got := testdb.QueryCount(t, `select count(*) from build_jobs where application_id=$1::uuid`, appA); got != 500 {
		t.Fatalf("app A builds = %d, want 500", got)
	}
	if got := testdb.QueryCount(t, `select count(*) from build_jobs where application_id=$1::uuid`, appB); got != 2 {
		t.Fatalf("app B builds = %d, want 2", got)
	}
}

func TestCleanupWorkerRemovesOrphanedPreviews(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "orphaned", "", nil)

	longGone := seedPreview(t, fixture.OrgID, appID, "now() - interval '49 hours'")
	recent := seedPreview(t, fixture.OrgID, appID, "now() - interval '1 hour'")
	noServices := seedPreview(t, fixture.OrgID, appID, "now() - interval '72 hours'")

	swarm := newFakeSwarm()
	svcID := swarm.addService(map[string]string{"hive.app.id": longGone})
	swarm.addService(map[string]string{"hive.app.id": "someone-else"})

	w := &CleanupWorker{Pool: pool, Swarm: swarm}
	if err := w.Work(context.Background(), &river.Job[CleanupJobArgs]{}); err != nil {
		t.Fatalf("Work: %v", err)
	}

	if got := testdb.QueryCount(t, `select count(*) from preview_deployments where id=$1::uuid`, longGone); got != 0 {
		t.Fatalf("long-expired preview still present")
	}
	if got := testdb.QueryCount(t, `select count(*) from preview_deployments where id=$1::uuid`, recent); got != 1 {
		t.Fatalf("recently expired preview must be kept")
	}
	if got := testdb.QueryCount(t, `select count(*) from preview_deployments where id=$1::uuid`, noServices); got != 0 {
		t.Fatalf("long-expired preview without services must be deleted")
	}

	swarm.mu.Lock()
	removed := append([]string(nil), swarm.removedIDs...)
	swarm.mu.Unlock()
	if len(removed) != 1 || removed[0] != svcID {
		t.Fatalf("removed services = %v, want [%s]", removed, svcID)
	}
}

func TestCleanupWorkerNilSwarmSkipsServiceRemoval(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "noswarm", "", nil)
	previewID := seedPreview(t, fixture.OrgID, appID, "now() - interval '72 hours'")

	w := &CleanupWorker{Pool: pool, Swarm: nil}
	if err := w.Work(context.Background(), &river.Job[CleanupJobArgs]{}); err != nil {
		t.Fatalf("Work: %v", err)
	}
	if got := testdb.QueryCount(t, `select count(*) from preview_deployments where id=$1::uuid`, previewID); got != 0 {
		t.Fatalf("orphan preview must still be deleted without a swarm client")
	}
}

func TestPreviewCleanupWorkerExpiresAndRemovesServices(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "prevcleanup", "", nil)

	expiring := seedPreview(t, fixture.OrgID, appID, "now() - interval '1 hour'")
	already := seedPreviewWithStatus(t, fixture.OrgID, appID, "expired", "now() - interval '1 hour'")

	swarm := newFakeSwarm()
	swarm.addService(map[string]string{"hive.app.id": expiring})
	swarm.addService(map[string]string{"hive.app.id": already})

	w := &PreviewCleanupWorker{Pool: pool, Swarm: swarm}
	if err := w.Work(context.Background(), &river.Job[PreviewCleanupJobArgs]{}); err != nil {
		t.Fatalf("Work: %v", err)
	}

	status := func(id string) string {
		var s string
		if err := pool.QueryRow(context.Background(),
			`select status from preview_deployments where id=$1::uuid`, id).Scan(&s); err != nil {
			t.Fatalf("load preview %s: %v", id, err)
		}
		return s
	}
	if got := status(expiring); got != "expired" {
		t.Fatalf("expiring preview status = %q, want expired", got)
	}
	if got := status(already); got != "expired" {
		t.Fatalf("already-expired preview status = %q, want expired", got)
	}

	swarm.mu.Lock()
	removed := append([]string(nil), swarm.removedIDs...)
	swarm.mu.Unlock()
	if len(removed) != 2 {
		t.Fatalf("removed services = %v, want both preview services removed", removed)
	}
}

func TestPreviewCleanupWorkerNilSwarm(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "prevcleanupnil", "", nil)
	seedPreview(t, fixture.OrgID, appID, "now() - interval '1 hour'")

	w := &PreviewCleanupWorker{Pool: pool, Swarm: nil}
	if err := w.Work(context.Background(), &river.Job[PreviewCleanupJobArgs]{}); err != nil {
		t.Fatalf("Work: %v", err)
	}
	if got := testdb.QueryCount(t, `select count(*) from preview_deployments where status='expired'`); got != 1 {
		t.Fatalf("expired previews = %d, want 1", got)
	}
}

// seedPreviewWithStatus inserts a preview row with an explicit status.
func seedPreviewWithStatus(t *testing.T, orgID, appID, status, expiresAt string) string {
	t.Helper()
	p := testdb.Get(t)
	var previewID string
	err := p.QueryRow(context.Background(), `
		insert into preview_deployments(organization_id, application_id, pr_number, branch, status, expires_at)
		values ($1::uuid, $2::uuid, 8, 'feature', $3, `+expiresAt+`)
		returning id::text
	`, orgID, appID, status).Scan(&previewID)
	if err != nil {
		t.Fatalf("seedPreviewWithStatus: %v", err)
	}
	return previewID
}
