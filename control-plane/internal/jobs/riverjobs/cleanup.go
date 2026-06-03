package riverjobs

import (
	"context"
	"log"

	"github.com/riverqueue/river"
)

type CleanupWorker struct {
	river.WorkerDefaults[CleanupJobArgs]
}

func (w *CleanupWorker) Work(ctx context.Context, job *river.Job[CleanupJobArgs]) error {
	log.Printf("cleanup job %d running", job.ID)
	return nil
}
