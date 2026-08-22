package riverjobs

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"

	"github.com/luke/hive/control-plane/internal/backup"
)

// BackupWorker executes the queued backup run matching the job's target,
// claiming it transactionally so it never double-runs alongside another
// executor.
type BackupWorker struct {
	river.WorkerDefaults[BackupJobArgs]
	Runner *backup.Runner
}

// Work processes a backup job, executing the backup and storing the artifact.
func (w *BackupWorker) Work(ctx context.Context, job *river.Job[BackupJobArgs]) error {
	if w.Runner == nil {
		return fmt.Errorf("backup runner is not configured")
	}
	return w.Runner.ExecuteQueued(ctx, job.Args.TargetType, job.Args.TargetID)
}
