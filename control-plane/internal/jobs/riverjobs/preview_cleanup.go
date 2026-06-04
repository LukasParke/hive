package riverjobs

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

type PreviewCleanupWorker struct {
	river.WorkerDefaults[PreviewCleanupJobArgs]
	Pool  *pgxpool.Pool
	Swarm *swarmclient.Client
}

func (w *PreviewCleanupWorker) Work(ctx context.Context, job *river.Job[PreviewCleanupJobArgs]) error {
	// Mark expired previews in DB.
	_, err := w.Pool.Exec(ctx, `
		update preview_deployments set status = 'expired'
		where expires_at < now() and status != 'expired'
	`)
	if err != nil {
		log.Printf("preview cleanup expire error: %v", err)
	}

	// Remove expired preview services from Swarm.
	if w.Swarm != nil {
		rows, err := w.Pool.Query(ctx, `select id::text from preview_deployments where status = 'expired'`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var previewID string
				if err := rows.Scan(&previewID); err != nil {
					continue
				}
				services, _ := w.Swarm.ListServices(ctx)
				for _, svc := range services {
					if svc.Spec.Labels["hive.app.id"] == previewID {
						_ = w.Swarm.RemoveService(ctx, svc.ID)
						break
					}
				}
			}
		}
	}
	return nil
}
