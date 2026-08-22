package riverjobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/deploy"
)

// DeployWorker applies a pending deployments row: service create/update
// with env vars, domain routing, status transitions and realtime NOTIFY.
type DeployWorker struct {
	river.WorkerDefaults[DeployJobArgs]
	Pool   *pgxpool.Pool
	Swarm  deploy.SwarmStack
	Fanout deploy.Emitter // optional; nil skips NOTIFY emission
}

// Work processes a deploy job for an application.
func (w *DeployWorker) Work(ctx context.Context, job *river.Job[DeployJobArgs]) error {
	if _, err := uuid.Parse(job.Args.DeploymentID); err != nil {
		return fmt.Errorf("invalid deployment id %q: %w", job.Args.DeploymentID, err)
	}
	queries := dbgen.New(w.Pool)
	deps := deploy.Deps{Pool: w.Pool, Swarm: w.Swarm, Fanout: w.Fanout}

	row, err := queries.GetDeploymentForExecution(ctx, uuidOrNil(job.Args.DeploymentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return river.JobCancel(fmt.Errorf("deployment %s no longer exists", job.Args.DeploymentID))
	} else if err != nil {
		return fmt.Errorf("load deployment %s: %w", job.Args.DeploymentID, err)
	}

	if err := queries.MarkDeploymentStatus(ctx, dbgen.MarkDeploymentStatusParams{
		Status:       "deploying",
		DeploymentID: uuidOrNil(row.ID),
	}); err != nil {
		return fmt.Errorf("mark deployment deploying: %w", err)
	}

	err = w.apply(ctx, deps, row)
	if err != nil {
		if markErr := queries.MarkDeploymentStatus(ctx, dbgen.MarkDeploymentStatusParams{
			Status:       "failed",
			DeploymentID: uuidOrNil(row.ID),
		}); markErr != nil {
			slog.ErrorContext(ctx, "mark deployment failed", "deployment_id", row.ID, "error", markErr)
		}
		return err
	}

	if err := queries.MarkDeploymentStatus(ctx, dbgen.MarkDeploymentStatusParams{
		Status:       "deployed",
		DeploymentID: uuidOrNil(row.ID),
	}); err != nil {
		return fmt.Errorf("mark deployment deployed: %w", err)
	}
	deploy.NotifyDeployment(ctx, w.Fanout, row.ApplicationID)
	return nil
}

func (w *DeployWorker) apply(ctx context.Context, deps deploy.Deps, row dbgen.GetDeploymentForExecutionRow) error {
	queries := dbgen.New(w.Pool)
	projectName, err := queries.GetProjectNameForApplication(ctx, uuidOrNil(row.ApplicationID))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load project for app %s: %w", row.ApplicationID, err)
	}
	spec := deploy.ApplicationSpec{
		AppID:         row.ApplicationID,
		ServiceName:   "app-" + row.Name,
		Image:         row.ImageTag,
		ContainerPort: int(row.ContainerPort),
		ProjectSlug:   projectName,
		DomainLookup:  domainLookup(ctx, w.Pool, row.ApplicationID),
	}
	// Serialize every spec mutation for this application (deploys race
	// domain-label updates on the same read-modify-write path).
	if err := deploy.WithAppLock(ctx, w.Pool, row.ApplicationID, func(ctx context.Context) error {
		if err := deploy.RunDeployment(ctx, deps, spec); err != nil {
			return fmt.Errorf("apply service: %w", err)
		}
		if err := deploy.ApplyApplicationDomains(ctx, deps, row.ApplicationID, int(row.ContainerPort)); err != nil {
			return fmt.Errorf("apply domains: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}
