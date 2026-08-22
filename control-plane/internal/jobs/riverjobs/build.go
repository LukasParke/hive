package riverjobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	buildruntime "github.com/luke/hive/control-plane/internal/build"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/deploy"
	"github.com/luke/hive/control-plane/internal/git"
	"github.com/luke/hive/control-plane/internal/notify"
)

// BuildWorker executes a build_jobs row: clone, image build + push, then
// hands off deployment to the DeployWorker via a pending deployments row.
type BuildWorker struct {
	river.WorkerDefaults[BuildJobArgs]
	Pool         *pgxpool.Pool
	RegistryHost string // internal registry fallback (REGISTRY_ADDR)
	Swarm        deploy.SwarmStack
	Buildkit     buildruntime.Builder
	Notifier     *notify.Dispatcher
}

// Work processes a build job, building and pushing the application image.
func (w *BuildWorker) Work(ctx context.Context, job *river.Job[BuildJobArgs]) error {
	if _, err := uuid.Parse(job.Args.BuildID); err != nil {
		return fmt.Errorf("invalid build id %q: %w", job.Args.BuildID, err)
	}
	buildID := job.Args.BuildID
	queries := dbgen.New(w.Pool)

	row, err := queries.GetBuildForExecution(ctx, uuidOrNil(buildID))
	if errors.Is(err, pgx.ErrNoRows) {
		return river.JobCancel(fmt.Errorf("build %s no longer exists", buildID))
	} else if err != nil {
		return fmt.Errorf("load build %s: %w", buildID, err)
	}
	if row.Status == "cancelled" {
		slog.InfoContext(ctx, "build cancelled before start", "build_id", buildID)
		return river.JobCancel(fmt.Errorf("build %s cancelled", buildID))
	}

	if err := queries.MarkBuildRunning(ctx, uuidOrNil(buildID)); err != nil {
		return fmt.Errorf("mark build running: %w", err)
	}

	logWriter := NewThrottledLogWriter(queries, buildID)
	defer func() { _ = logWriter.Close(context.WithoutCancel(ctx)) }()
	stopFlush := periodicFlush(ctx, logWriter, 500*time.Millisecond)
	defer stopFlush()

	imageTag := row.Image
	switch {
	case row.Trigger == "rollback" && row.RequestedImageTag != "":
		imageTag = row.RequestedImageTag
	case row.SourceType == "git":
		var err error
		imageTag, err = w.buildAndPush(ctx, job, row, logWriter)
		if err != nil {
			w.markFailedIfFinal(ctx, job, buildID, "build", err)
			return err
		}
	case row.SourceType == "image":
		// nothing to build; deploy the configured image directly
	default:
		err := fmt.Errorf("application source type %q is not buildable", row.SourceType)
		w.markFailedIfFinal(ctx, job, buildID, "build", err)
		return err
	}

	if err := w.checkCancelled(ctx, queries, buildID); err != nil {
		return err
	}

	deploymentID, err := queries.CreatePendingDeployment(ctx, dbgen.CreatePendingDeploymentParams{
		ApplicationID: uuidOrNil(row.ApplicationID),
		ImageTag:      imageTag,
		Trigger:       row.Trigger,
	})
	if err != nil {
		return fmt.Errorf("create pending deployment: %w", err)
	}

	if rc := river.ClientFromContext[pgx.Tx](ctx); rc != nil {
		if _, err := rc.Insert(ctx, DeployJobArgs{DeploymentID: deploymentID}, nil); err != nil {
			return fmt.Errorf("enqueue deploy job: %w", err)
		}
	}

	if err := queries.CompleteBuildJob(ctx, dbgen.CompleteBuildJobParams{
		ImageTag: pgText(imageTag),
		BuildID:  uuidOrNil(buildID),
	}); err != nil {
		return fmt.Errorf("mark build complete: %w", err)
	}

	if w.Notifier != nil {
		w.Notifier.Notify(ctx, "build.succeeded", map[string]any{"jobId": buildID, "imageTag": imageTag})
	}
	return nil
}

// buildAndPush clones the repository when needed and pushes the image to
// the resolved registry. Returns the pushed image reference.
func (w *BuildWorker) buildAndPush(ctx context.Context, job *river.Job[BuildJobArgs], row dbgen.GetBuildForExecutionRow, logWriter *ThrottledLogWriter) (string, error) {
	queries := dbgen.New(w.Pool)

	auth, err := buildruntime.ResolveRegistry(ctx, w.Pool, uuidRef(row.RegistryID), w.RegistryHost)
	if err != nil {
		return "", err
	}

	keyPath, err := materializeAppSSHKey(ctx, w.Pool, row.ApplicationID)
	if err != nil {
		return "", err
	}
	if keyPath != "" {
		defer git.Cleanup(filepath.Dir(keyPath))
	}

	repoDir, sha, err := git.Clone(ctx, git.Options{
		RepositoryURL: row.RepositoryUrl,
		Ref:           row.GitRef,
		SSHKeyPath:    keyPath,
	})
	if err != nil {
		return "", fmt.Errorf("clone %s@%s: %w", row.RepositoryUrl, row.GitRef, err)
	}
	defer git.Cleanup(repoDir)

	if sha != "" {
		if err := queries.SetBuildGitSha(ctx, dbgen.SetBuildGitShaParams{
			GitSha:  pgText(sha),
			BuildID: uuidOrNil(job.Args.BuildID),
		}); err != nil {
			return "", fmt.Errorf("record commit sha: %w", err)
		}
	}

	imageTag := auth.ImageRef(row.ProjectID, row.Name, shortTag(sha, job.Args.BuildID))
	if err := queries.SetBuildImageTag(ctx, dbgen.SetBuildImageTagParams{
		ImageTag: pgText(imageTag),
		BuildID:  uuidOrNil(job.Args.BuildID),
	}); err != nil {
		return "", fmt.Errorf("record image tag: %w", err)
	}

	if err := w.checkCancelled(ctx, queries, job.Args.BuildID); err != nil {
		return "", err
	}

	if err := w.Buildkit.BuildAndPush(ctx, buildruntime.Request{
		ContextPath: repoDir,
		Dockerfile:  "Dockerfile",
		ImageTag:    imageTag,
		Auth:        &auth,
	}, logWriter); err != nil {
		return "", fmt.Errorf("build and push %s: %w", imageTag, err)
	}
	return imageTag, nil
}

// uuidRef converts a nullable registry uuid column into the *string form
// ResolveRegistry expects; nil when unset.
func uuidRef(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	s := uuid.UUID(id.Bytes).String()
	return &s
}

// checkCancelled aborts the river job when the build row has been
// cancelled between stages.
func (w *BuildWorker) checkCancelled(ctx context.Context, queries *dbgen.Queries, buildID string) error {
	status, err := queries.GetBuildStatus(ctx, uuidOrNil(buildID))
	if err != nil {
		return fmt.Errorf("check build status: %w", err)
	}
	if status == "cancelled" {
		return river.JobCancel(fmt.Errorf("build %s cancelled", buildID))
	}
	return nil
}

// markFailedIfFinal records the failure on the build row once river has
// exhausted its retry budget for the job.
func (w *BuildWorker) markFailedIfFinal(ctx context.Context, job *river.Job[BuildJobArgs], buildID, stage string, workErr error) {
	if job.Attempt < job.MaxAttempts {
		return
	}
	_, _ = w.Pool.Exec(ctx,
		`update build_jobs set status='failed', error_message=$2, completed_at=now() where id=$1::uuid`,
		buildID, stage+": "+workErr.Error())
	if w.Notifier != nil {
		w.Notifier.Notify(ctx, stage+".failed", map[string]any{"jobId": buildID, "error": workErr.Error()})
	}
}

// periodicFlush flushes the log writer on an interval so long-running
// builds stream progress even without hitting the line threshold.
func periodicFlush(ctx context.Context, lw *ThrottledLogWriter, interval time.Duration) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case now := <-ticker.C:
				_ = lw.Flush(ctx, now)
			}
		}
	}()
	return func() { close(done) }
}

// shortTag prefers the commit sha; a build with no sha (image sources or
// failed rev-parse) falls back to the build id prefix.
func shortTag(sha, buildID string) string {
	if sha != "" {
		if len(sha) > 12 {
			return sha[:12]
		}
		return sha
	}
	if len(buildID) >= 8 {
		return buildID[:8]
	}
	return buildID
}
