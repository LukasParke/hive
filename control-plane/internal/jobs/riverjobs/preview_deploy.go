package riverjobs

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	buildruntime "github.com/luke/hive/control-plane/internal/build"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/deploy"
	"github.com/luke/hive/control-plane/internal/git"
	"github.com/luke/hive/control-plane/internal/notify"
	"github.com/luke/hive/control-plane/internal/proxy"
)

// PreviewDeployWorker builds and deploys pull-request preview environments.
type PreviewDeployWorker struct {
	river.WorkerDefaults[PreviewDeployJobArgs]
	Pool         *pgxpool.Pool
	RegistryHost string
	Swarm        deploy.SwarmStack
	Buildkit     buildruntime.Builder
	Notifier     *notify.Dispatcher
}

// Work processes a preview deployment job.
func (w *PreviewDeployWorker) Work(ctx context.Context, job *river.Job[PreviewDeployJobArgs]) error {
	var appID string
	var appName string
	var projectID string
	var repoURL string
	var containerPort int32
	var registryID *string
	err := w.Pool.QueryRow(ctx, `
		select a.id::text, a.name, a.project_id::text, coalesce(a.repository_url, ''),
		       coalesce(a.container_port, 3000), a.registry_id::text
		from applications a
		where a.id = $1::uuid
	`, job.Args.ApplicationID).Scan(&appID, &appName, &projectID, &repoURL, &containerPort, &registryID)
	if err != nil {
		return err
	}

	auth, err := buildruntime.ResolveRegistry(ctx, w.Pool, registryID, w.RegistryHost)
	if err != nil {
		w.markFailed(ctx, job.Args.PreviewID, err)
		return err
	}

	imageTag := auth.ImageRef(projectID, appName, "preview-"+job.Args.PreviewID)
	buildContext := "."
	if repoURL != "" {
		keyPath, keyErr := materializeAppSSHKey(ctx, w.Pool, job.Args.ApplicationID)
		if keyErr != nil {
			w.markFailed(ctx, job.Args.PreviewID, keyErr)
			return keyErr
		}
		if keyPath != "" {
			defer git.Cleanup(filepath.Dir(keyPath))
		}
		repoDir, _, cloneErr := git.Clone(ctx, git.Options{
			RepositoryURL: repoURL,
			Ref:           job.Args.Branch,
			SSHKeyPath:    keyPath,
		})
		if cloneErr != nil {
			w.markFailed(ctx, job.Args.PreviewID, cloneErr)
			return cloneErr
		}
		defer git.Cleanup(repoDir)
		buildContext = repoDir
	}

	if err := w.Buildkit.BuildAndPush(ctx, buildruntime.Request{
		ContextPath: buildContext,
		Dockerfile:  "Dockerfile",
		ImageTag:    imageTag,
		Auth:        &auth,
	}, nil); err != nil {
		w.markFailed(ctx, job.Args.PreviewID, err)
		return err
	}

	envVars, err := deploy.LoadEnvVars(ctx, w.Pool, appID)
	if err != nil {
		w.markFailed(ctx, job.Args.PreviewID, err)
		return err
	}

	previewURL := fmt.Sprintf("https://preview-%s-%s.preview.local", appName, job.Args.PreviewID)
	// Previews always get the preview host applied below, so hive_proxy must
	// be attached for Traefik to reach the service. The project overlay comes
	// from the PARENT application's project.
	queries := dbgen.New(w.Pool)
	projectName, projErr := queries.GetProjectNameForApplication(ctx, uuidOrNil(job.Args.ApplicationID))
	if projErr != nil && !errors.Is(projErr, pgx.ErrNoRows) {
		w.markFailed(ctx, job.Args.PreviewID, projErr)
		return projErr
	}
	previewHost := fmt.Sprintf("preview-%s-%s.preview.local", appName, job.Args.PreviewID)
	lockAppID := job.Args.PreviewID
	if err := deploy.WithAppLock(ctx, w.Pool, lockAppID, func(ctx context.Context) error {
		if err := deploy.Application(ctx, w.Swarm, deploy.ApplicationSpec{
			AppID:         job.Args.PreviewID,
			ServiceName:   fmt.Sprintf("preview-%s-%s", appName, job.Args.PreviewID),
			Image:         imageTag,
			ContainerPort: int(containerPort),
			EnvVars:       envVars,
			ProjectSlug:   projectName,
			DomainLookup: func(_ context.Context, _ string) ([]string, error) {
				return []string{previewHost}, nil
			},
		}); err != nil {
			return err
		}

		// Apply domain labels for Traefik routing to the preview service.
		if targetServiceID, svcErr := deploy.ResolveAppService(ctx, w.Swarm, job.Args.PreviewID); svcErr == nil && targetServiceID != "" {
			domainManager := proxy.NewDomainManager(w.Swarm)
			return domainManager.ApplyDomain(ctx, targetServiceID, proxy.RouterNameFromHost(previewURL), proxy.Route{Host: previewURL, TLSEnabled: true}, int(containerPort))
		}
		return nil
	}); err != nil {
		w.markFailed(ctx, job.Args.PreviewID, err)
		return err
	}

	if _, err := w.Pool.Exec(ctx, `
		update preview_deployments set status = 'ready', url = $1 where id = $2::uuid
	`, previewURL, job.Args.PreviewID); err != nil {
		return err
	}

	if w.Notifier != nil {
		w.Notifier.Notify(ctx, "preview.ready", map[string]any{"previewId": job.Args.PreviewID, "url": previewURL})
	}
	return nil
}

func (w *PreviewDeployWorker) markFailed(ctx context.Context, previewID string, err error) {
	// Best-effort status flip: the job error itself carries the failure.
	_, _ = w.Pool.Exec(ctx, `update preview_deployments set status = 'failed', error_message = $1 where id = $2::uuid`, err.Error(), previewID)
	if w.Notifier != nil {
		w.Notifier.Notify(ctx, "preview.failed", map[string]any{"previewId": previewID, "error": err.Error()})
	}
}
