package build

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	buildruntime "github.com/luke/hive/control-plane/internal/build"
	"github.com/luke/hive/control-plane/internal/deploy"
	"github.com/luke/hive/control-plane/internal/git"
	"github.com/luke/hive/control-plane/internal/notify"
	"github.com/luke/hive/control-plane/internal/proxy"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

type Handler struct {
	pool         *pgxpool.Pool
	registryHost string
	swarm        *swarmclient.Client
	buildkit     *buildruntime.Client
	notifier     *notify.Dispatcher
}

func NewHandler(pool *pgxpool.Pool, registryHost string, swarm *swarmclient.Client, buildkit *buildruntime.Client, notifier *notify.Dispatcher) *Handler {
	return &Handler{
		pool:         pool,
		registryHost: registryHost,
		swarm:        swarm,
		buildkit:     buildkit,
		notifier:     notifier,
	}
}

func (h *Handler) Handle(ctx context.Context, jobID string) error {
	var applicationID string
	var sourceType string
	var trigger string
	var appName string
	var projectID string
	var image string
	var requestedImageTag string
	var repositoryURL string
	var gitRef string
	var containerPort int
	err := h.pool.QueryRow(ctx, `
		select bj.application_id::text, a.source_type::text, bj.trigger, a.name, a.project_id::text, coalesce(a.image, ''), coalesce(bj.image_tag, ''), coalesce(a.repository_url, ''), coalesce(a.git_ref, 'main'), coalesce(a.container_port, 3000)
		from build_jobs bj
		join applications a on a.id = bj.application_id
		where bj.id = $1::uuid
	`, jobID).Scan(&applicationID, &sourceType, &trigger, &appName, &projectID, &image, &requestedImageTag, &repositoryURL, &gitRef, &containerPort)
	if err != nil {
		return err
	}

	_, err = h.pool.Exec(ctx, `
		update build_jobs
		set logs = logs || $2
		where id = $1::uuid
	`, jobID, fmt.Sprintf("Build started for job %s\n", jobID))
	if err != nil {
		return err
	}

	imageTag := image
	if trigger == "rollback" && requestedImageTag != "" {
		imageTag = requestedImageTag
	}
	buildContext := "."
	if trigger != "rollback" && sourceType != "image" {
		if sourceType == "git" {
			repoDir, err := git.Clone(ctx, repositoryURL, gitRef)
			if err != nil {
				_, _ = h.pool.Exec(ctx, `update build_jobs set status='failed', error_message=$2 where id=$1::uuid`, jobID, err.Error())
				if h.notifier != nil {
					h.notifier.Notify(ctx, "build.failed", map[string]any{"jobId": jobID, "error": err.Error()})
				}
				return err
			}
			defer git.Cleanup(repoDir)
			buildContext = repoDir
		}
		imageTag = fmt.Sprintf("%s/%s/%s:%s", strings.TrimSuffix(h.registryHost, "/"), projectID, appName, jobID[:8])
		if err := h.buildkit.BuildAndPush(ctx, buildruntime.BuildRequest{
			ContextPath: buildContext,
			Dockerfile:  "Dockerfile",
			ImageTag:    imageTag,
		}, nil); err != nil {
			_, _ = h.pool.Exec(ctx, `update build_jobs set status='failed', error_message=$2 where id=$1::uuid`, jobID, err.Error())
			if h.notifier != nil {
				h.notifier.Notify(ctx, "build.failed", map[string]any{"jobId": jobID, "error": err.Error()})
			}
			return err
		}
	}

	// Query application env vars for deployment.
	var envVars []deploy.EnvVar
	envRows, envErr := h.pool.Query(ctx, `
		select key, value, is_secret, secret_version
		from app_env_vars where application_id = $1::uuid order by key
	`, applicationID)
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
				truncID := applicationID
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

	if err := deploy.DeployApplication(ctx, h.swarm, deploy.ApplicationSpec{
		AppID:         applicationID,
		ServiceName:   "app-" + appName,
		Image:         imageTag,
		ContainerPort: containerPort,
		EnvVars:       envVars,
	}); err != nil {
		_, _ = h.pool.Exec(ctx, `update build_jobs set status='failed', error_message=$2 where id=$1::uuid`, jobID, err.Error())
		if h.notifier != nil {
			h.notifier.Notify(ctx, "deploy.failed", map[string]any{"jobId": jobID, "error": err.Error()})
		}
		return err
	}

	rows, err := h.pool.Query(ctx, `select hostname from domains where application_id = $1::uuid`, applicationID)
	if err == nil {
		defer rows.Close()
		services, _ := h.swarm.ListServices(ctx)
		var targetServiceID string
		for _, svc := range services {
			if svc.Spec.Labels["hive.app.id"] == applicationID {
				targetServiceID = svc.ID
				break
			}
		}
		if targetServiceID != "" {
			domainManager := proxy.NewDomainManager(h.swarm)
			for rows.Next() {
				var host string
				if err := rows.Scan(&host); err == nil {
					_ = domainManager.ApplyDomain(ctx, targetServiceID, proxy.RouterNameFromHost(host), host, containerPort)
				}
			}
		}
	}

	_, _ = h.pool.Exec(ctx, `
		insert into deployments(application_id, image_tag, status, trigger)
		values ($1::uuid, $2, 'deployed', $3)
	`, applicationID, imageTag, trigger)

	_, err = h.pool.Exec(ctx, `
		update build_jobs
		set image_tag = $2,
		    logs = logs || $3
		where id = $1::uuid
	`, jobID, imageTag, "Build completed and deploy applied\n")
	if err == nil && h.notifier != nil {
		h.notifier.Notify(ctx, "build.succeeded", map[string]any{"jobId": jobID, "imageTag": imageTag})
	}
	return err
}
