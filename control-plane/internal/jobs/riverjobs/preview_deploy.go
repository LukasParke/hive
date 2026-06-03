package riverjobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	buildruntime "github.com/luke/hive/control-plane/internal/build"
	"github.com/luke/hive/control-plane/internal/deploy"
	"github.com/luke/hive/control-plane/internal/git"
	"github.com/luke/hive/control-plane/internal/notify"
	"github.com/luke/hive/control-plane/internal/proxy"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

type PreviewDeployWorker struct {
	river.WorkerDefaults[PreviewDeployJobArgs]
	Pool         *pgxpool.Pool
	RegistryHost string
	Swarm        *swarmclient.Client
	Buildkit     *buildruntime.Client
	Notifier     *notify.Dispatcher
}

func (w *PreviewDeployWorker) Work(ctx context.Context, job *river.Job[PreviewDeployJobArgs]) error {
	var appID string
	var appName string
	var projectID string
	var repoURL string
	var containerPort int
	err := w.Pool.QueryRow(ctx, `
		select a.id::text, a.name, a.project_id::text, coalesce(a.repository_url, ''), coalesce(a.container_port, 3000)
		from applications a
		where a.id = $1::uuid
	`, job.Args.ApplicationID).Scan(&appID, &appName, &projectID, &repoURL, &containerPort)
	if err != nil {
		return err
	}

	imageTag := fmt.Sprintf("%s/%s/%s:preview-%s", w.RegistryHost, projectID, appName, job.Args.PreviewID)
	buildContext := "."
	if repoURL != "" {
		repoDir, err := git.Clone(ctx, repoURL, job.Args.Branch)
		if err != nil {
			w.markFailed(ctx, job.Args.PreviewID, err)
			return err
		}
		defer git.Cleanup(repoDir)
		buildContext = repoDir
	}

	if err := w.Buildkit.BuildAndPush(ctx, buildruntime.BuildRequest{
		ContextPath: buildContext,
		Dockerfile:  "Dockerfile",
		ImageTag:    imageTag,
	}, nil); err != nil {
		w.markFailed(ctx, job.Args.PreviewID, err)
		return err
	}

	var envVars []deploy.EnvVar
	envRows, envErr := w.Pool.Query(ctx, `select key, value, is_secret, secret_version from app_env_vars where application_id = $1::uuid order by key`, appID)
	if envErr == nil {
		defer envRows.Close()
		for envRows.Next() {
			var key string
			var value *string
			var isSecret bool
			var secretVersion int
			if err := envRows.Scan(&key, &value, &isSecret, &secretVersion); err != nil {
				continue
			}
			ev := deploy.EnvVar{Key: key, IsSecret: isSecret}
			if isSecret {
				truncID := appID
				if len(truncID) > 12 {
					truncID = truncID[:12]
				}
				ev.SecretName = fmt.Sprintf("hive.%s.%s.v%d", truncID, key, secretVersion)
			} else if value != nil {
				ev.Value = *value
			}
			envVars = append(envVars, ev)
		}
	}

	previewURL := fmt.Sprintf("https://preview-%s-%s.preview.local", appName, job.Args.PreviewID)
	if err := deploy.DeployApplication(ctx, w.Swarm, deploy.ApplicationSpec{
		AppID:         job.Args.PreviewID,
		ServiceName:   fmt.Sprintf("preview-%s-%s", appName, job.Args.PreviewID),
		Image:         imageTag,
		ContainerPort: containerPort,
		EnvVars:       envVars,
	}); err != nil {
		w.markFailed(ctx, job.Args.PreviewID, err)
		return err
	}

	// Apply domain labels for Traefik routing to the preview service.
	if w.Swarm != nil {
		services, _ := w.Swarm.ListServices(ctx)
		var targetServiceID string
		for _, svc := range services {
			if svc.Spec.Labels["dokploy.app.id"] == job.Args.PreviewID {
				targetServiceID = svc.ID
				break
			}
		}
		if targetServiceID != "" {
			domainManager := proxy.NewDomainManager(w.Swarm)
			_ = domainManager.ApplyDomain(ctx, targetServiceID, proxy.RouterNameFromHost(previewURL), previewURL, containerPort)
		}
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
	w.Pool.Exec(ctx, `update preview_deployments set status = 'failed', error_message = $1 where id = $2::uuid`, err.Error(), previewID)
	if w.Notifier != nil {
		w.Notifier.Notify(ctx, "preview.failed", map[string]any{"previewId": previewID, "error": err.Error()})
	}
}
