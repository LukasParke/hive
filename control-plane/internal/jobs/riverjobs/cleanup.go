package riverjobs

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/deploy"
)

// maxBuildsPerApplication bounds build_jobs history retention per app.
const maxBuildsPerApplication = 500

// CleanupWorker prunes old build rows and removes orphaned preview stacks
// whose expiry passed more than 48 hours ago.
type CleanupWorker struct {
	river.WorkerDefaults[CleanupJobArgs]
	Pool  *pgxpool.Pool
	Swarm deploy.SwarmStack
}

// Work processes a cleanup job, pruning expired resources.
func (w *CleanupWorker) Work(ctx context.Context, job *river.Job[CleanupJobArgs]) error {
	if w.Pool == nil {
		return nil
	}
	if err := dbgen.New(w.Pool).PruneBuildHistory(ctx, maxBuildsPerApplication); err != nil {
		slog.ErrorContext(ctx, "prune build history", "error", err)
	}
	w.pruneOrphanPreviews(ctx)
	return nil
}

// pruneOrphanPreviews deletes preview_deployments rows that expired more
// than 48 hours ago after removing their swarm services.
func (w *CleanupWorker) pruneOrphanPreviews(ctx context.Context) {
	rows, err := w.Pool.Query(ctx, `
		select id::text from preview_deployments
		where expires_at < now() - interval '48 hours'
	`)
	if err != nil {
		slog.ErrorContext(ctx, "list expired previews", "error", err)
		return
	}
	defer rows.Close()

	var expired []string
	for rows.Next() {
		var previewID string
		if rows.Scan(&previewID) == nil {
			expired = append(expired, previewID)
		}
	}

	for _, previewID := range expired {
		w.removePreviewServices(ctx, previewID)
		if _, err := w.Pool.Exec(ctx, `delete from preview_deployments where id = $1::uuid`, previewID); err != nil {
			slog.ErrorContext(ctx, "delete expired preview", "preview_id", previewID, "error", err)
		} else {
			slog.InfoContext(ctx, "removed orphaned preview", "preview_id", previewID)
		}
	}
}

func (w *CleanupWorker) removePreviewServices(ctx context.Context, previewID string) {
	if w.Swarm == nil {
		return
	}
	services, err := w.Swarm.ListServices(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "list services for preview cleanup", "error", err)
		return
	}
	for _, svc := range services {
		if svc.Spec.Labels["hive.app.id"] == previewID {
			_ = w.Swarm.RemoveService(ctx, svc.ID)
		}
	}
}
