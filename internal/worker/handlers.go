package worker

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/backup"
	"github.com/lholliger/hive/internal/database"
	"github.com/lholliger/hive/internal/deploy"
	"github.com/lholliger/hive/internal/github"
	"github.com/lholliger/hive/internal/maintenance"
	"github.com/lholliger/hive/internal/networking"
	"github.com/lholliger/hive/internal/notify"
	"github.com/lholliger/hive/internal/storage"
	"github.com/lholliger/hive/internal/store"
	hiveswarm "github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/pkg/encryption"
)

// toStringMap converts a map[string]any (from JSON unmarshal) to map[string]string,
// converting numeric and other non-string values via fmt.Sprintf.
func toStringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			out[k] = val
		case nil:
			out[k] = ""
		default:
			out[k] = fmt.Sprintf("%v", val)
		}
	}
	return out
}

func (p *Pool) handleBuild(msg *nats.Msg) {
	var raw map[string]any
	if err := json.Unmarshal(msg.Data, &raw); err != nil {
		p.log.Errorf("build: invalid job: %v", err)
		return
	}
	job := toStringMap(raw)
	appID := job["app_id"]
	deploymentID := job["deployment_id"]
	p.log.Infof("build job received: app=%s type=%s", appID, job["deploy_type"])

	p.publishProgress(appID, "cloning repository...")
	buildDir, err := os.MkdirTemp(filepath.Join(p.cfg.DataDir, "builds"), job["name"]+"-*")
	if err != nil {
		if mkErr := os.MkdirAll(filepath.Join(p.cfg.DataDir, "builds"), 0755); mkErr != nil {
			p.log.Errorf("build: failed to create builds parent dir: %v", mkErr)
			return
		}
		buildDir, err = os.MkdirTemp(filepath.Join(p.cfg.DataDir, "builds"), job["name"]+"-*")
		if err != nil {
			p.log.Errorf("build: failed to create build dir: %v", err)
			return
		}
	}
	defer func() { _ = os.RemoveAll(buildDir) }()

	repo := job["git_repo"]
	branch := job["git_branch"]
	if branch == "" {
		branch = "main"
	}

	cloneURL := repo
	cloneCtx := context.Background()
	if strings.Contains(repo, "github.com") {
		if token, err := p.gitHubCloneToken(cloneCtx); err == nil && token != "" {
			cloneURL = injectTokenInURL(repo, token)
		}
	} else if sourceID := job["git_source_id"]; sourceID != "" {
		if token := p.patCloneToken(cloneCtx, sourceID); token != "" {
			cloneURL = injectTokenInURL(repo, token)
		}
	}

	cloneCmd := exec.Command("git", "clone", "--depth=1", "--branch", branch, cloneURL, buildDir)
	if output, err := cloneCmd.CombinedOutput(); err != nil {
		p.publishProgress(appID, fmt.Sprintf("clone failed: %s", string(output)))
		p.log.Errorf("build: clone failed: %v", err)
		p.finishDeployment(deploymentID, "failed", string(output))
		p.setAppFailed(appID)
		p.notifyDeployFailure(appID, job["name"], string(output))
		return
	}
	p.publishProgress(appID, "repository cloned, building image...")

	imageName := fmt.Sprintf("hive-%s:latest", job["name"])
	if p.cfg.MultiNode {
		registryDomain := p.cfg.RegistryDomain
		if registryDomain == "" {
			registryDomain = "127.0.0.1:5000"
		}
		imageName = fmt.Sprintf("%s/hive-%s:latest", registryDomain, job["name"])
	}

	dockerfile := job["dockerfile"]
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	var buildLog string
	dockerfilePath := filepath.Join(buildDir, dockerfile)
	if _, err := os.Stat(dockerfilePath); err == nil {
		buildCmd := exec.Command("docker", "build", "-t", imageName, "-f", dockerfilePath, buildDir)
		if output, err := buildCmd.CombinedOutput(); err != nil {
			buildLog = string(output)
			p.publishProgress(appID, fmt.Sprintf("docker build failed: %s", buildLog))
			p.log.Errorf("build: docker build failed: %v", err)
			p.finishDeployment(deploymentID, "failed", buildLog)
			p.setAppFailed(appID)
			p.notifyDeployFailure(appID, job["name"], buildLog)
			return
		} else {
			buildLog = string(output)
		}
	} else {
		p.publishProgress(appID, "no Dockerfile found, trying nixpacks...")
		nixCmd := exec.Command("nixpacks", "build", buildDir, "--name", imageName)
		if output, err := nixCmd.CombinedOutput(); err != nil {
			buildLog = string(output)
			p.publishProgress(appID, fmt.Sprintf("nixpacks build failed: %s", buildLog))
			p.log.Errorf("build: nixpacks failed: %v", err)
			p.finishDeployment(deploymentID, "failed", buildLog)
			p.setAppFailed(appID)
			p.notifyDeployFailure(appID, job["name"], buildLog)
			return
		} else {
			buildLog = string(output)
		}
	}
	p.publishProgress(appID, "image built successfully")

	if p.cfg.MultiNode {
		p.publishProgress(appID, "pushing to internal registry...")
		pushCmd := exec.Command("docker", "push", imageName)
		if output, err := pushCmd.CombinedOutput(); err != nil {
			p.publishProgress(appID, fmt.Sprintf("push failed: %s", string(output)))
			p.log.Errorf("build: push failed: %v", err)
			p.finishDeployment(deploymentID, "failed", string(output))
			p.setAppFailed(appID)
			return
		}
		p.publishProgress(appID, "image pushed to registry")
	}

	p.appendDeploymentLog(deploymentID, buildLog)

	deployJob, err := json.Marshal(map[string]string{
		"action":        "deploy",
		"app_id":        appID,
		"deployment_id": deploymentID,
		"deploy_type":   "image",
		"image":         imageName,
		"name":          job["name"],
		"domain":        job["domain"],
	})
	if err != nil {
		p.log.Errorf("build: marshal deploy job: %v", err)
		p.finishDeployment(deploymentID, "failed", err.Error())
		p.setAppFailed(appID)
		return
	}
	if err := p.nc.Publish("hive.deploy", deployJob); err != nil {
		p.log.Errorf("failed to publish deploy job: %v", err)
	}
	p.publishProgress(appID, "build complete, deploying...")
}

func (p *Pool) handleDeploy(msg *nats.Msg) {
	var raw map[string]any
	if err := json.Unmarshal(msg.Data, &raw); err != nil {
		p.log.Errorf("deploy: invalid job: %v", err)
		return
	}
	job := toStringMap(raw)

	action := job["action"]
	p.log.Infof("deploy job: action=%s app=%s", action, job["name"])

	sc, err := hiveswarm.NewClient(p.log)
	if err != nil {
		p.log.Errorf("deploy: docker client error: %v", err)
		return
	}
	defer func() { _ = sc.Close() }()

	ctx := context.Background()
	p.dispatchDeployAction(ctx, sc, action, job)
}

func (p *Pool) dispatchDeployAction(ctx context.Context, sc *hiveswarm.Client, action string, job map[string]string) {
	switch action {
	case "deploy":
		p.deployService(ctx, sc, job)
	case "remove":
		p.removeService(ctx, sc, job)
	case "provision":
		p.provisionDatabase(ctx, sc, job)
	case "stack_deploy":
		p.deployStack(ctx, sc, job)
	case "stack_remove":
		p.removeStack(ctx, sc, job)
	case "run_job":
		p.runScheduledJob(ctx, job)
	default:
		p.log.Warnf("deploy: unknown action: %s", action)
	}
}

func (p *Pool) runScheduledJob(ctx context.Context, job map[string]string) {
	if p.store == nil {
		p.log.Warn("run_job: store unavailable")
		return
	}
	jobID := job["job_id"]
	if jobID == "" {
		p.log.Warn("run_job: missing job_id")
		return
	}

	sj, err := p.store.GetScheduledJob(ctx, jobID)
	if err != nil {
		p.log.Errorf("run_job: load scheduled job %s: %v", jobID, err)
		return
	}

	run, err := p.store.CreateJobRun(ctx, sj.ID, "running", "")
	if err != nil {
		p.log.Errorf("run_job: create job_run: %v", err)
		return
	}

	jobCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	containerName := fmt.Sprintf("hive-job-%s", strings.ReplaceAll(run.ID, "-", "")[:12])
	args := []string{
		"run", "--rm",
		"--name", containerName,
		"--network", "hive-net",
		"--label", "hive.managed=true",
		"--label", "hive.job_id=" + sj.ID,
	}

	var env map[string]string
	if len(sj.Env) > 0 {
		_ = json.Unmarshal(sj.Env, &env)
	}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, sj.Image)
	if strings.TrimSpace(sj.Command) != "" {
		args = append(args, "sh", "-c", sj.Command)
	}

	cmd := exec.CommandContext(jobCtx, "docker", args...)
	output, runErr := cmd.CombinedOutput()
	logs := string(output)

	status := "success"
	var exitCode *int
	if runErr != nil {
		status = "failed"
		if ee, ok := runErr.(*exec.ExitError); ok {
			code := ee.ExitCode()
			exitCode = &code
		} else {
			code := 1
			exitCode = &code
		}
		p.log.Warnf("run_job: job %s failed: %v", sj.Name, runErr)
	} else {
		code := 0
		exitCode = &code
	}

	if err := p.store.UpdateJobRun(context.Background(), run.ID, status, exitCode, logs); err != nil {
		p.log.Errorf("run_job: update job_run: %v", err)
	}
	_ = p.store.UpdateJobLastRun(context.Background(), sj.ID, nil)
}

func (p *Pool) ensureAppVolumes(ctx context.Context, sc *hiveswarm.Client, appID string) {
	if p.store == nil || sc == nil {
		return
	}
	appVolumes, err := p.store.ListAppVolumes(ctx, appID)
	if err != nil || len(appVolumes) == 0 {
		return
	}
	for _, av := range appVolumes {
		vol, err := p.store.GetVolume(ctx, av.VolumeID)
		if err != nil || vol == nil {
			continue
		}
		if vol.MountType == "" || vol.MountType == "volume" {
			continue
		}
		labels := map[string]string{
			"hive.managed":    "true",
			"hive.volume_id":  vol.ID,
			"hive.project_id": vol.ProjectID,
		}
		switch vol.MountType {
		case "nfs":
			if vol.RemoteHost != "" && vol.RemotePath != "" {
				if _, err := sc.CreateNFSVolume(ctx, vol.Name, vol.RemoteHost, vol.RemotePath, vol.MountOptions, labels); err != nil {
					p.log.Warnf("ensure volume %s (nfs): %v", vol.Name, err)
				}
			}
		case "cifs":
			if vol.RemoteHost != "" && vol.RemotePath != "" {
				username, password := "", ""
				if vol.StorageHostID != "" {
					if sh, shErr := p.store.GetStorageHost(ctx, vol.StorageHostID); shErr == nil {
						creds, _ := storage.DecryptCredentials(sh)
						username = creds
					}
				}
				if _, err := sc.CreateCIFSVolume(ctx, vol.Name, vol.RemoteHost, vol.RemotePath, username, password, vol.MountOptions, labels); err != nil {
					p.log.Warnf("ensure volume %s (cifs): %v", vol.Name, err)
				}
			}
		case "cephfs":
			if vol.StorageHostID != "" {
				if sh, shErr := p.store.GetStorageHost(ctx, vol.StorageHostID); shErr == nil {
					monitors := storage.CephMonitorAddresses(sh)
					monStr := strings.Join(monitors, ",")
					if _, err := sc.CreateCephFSVolume(ctx, vol.Name, monStr, vol.CephFSName, vol.RemotePath, vol.MountOptions, labels); err != nil {
						p.log.Warnf("ensure volume %s (cephfs): %v", vol.Name, err)
					}
				}
			}
		case "ceph-rbd":
			if vol.StorageHostID != "" {
				if sh, shErr := p.store.GetStorageHost(ctx, vol.StorageHostID); shErr == nil {
					monitors := storage.CephMonitorAddresses(sh)
					monStr := strings.Join(monitors, ",")
					if _, err := sc.CreateCephRBDVolume(ctx, vol.Name, monStr, vol.CephPool, vol.CephImage, vol.MountOptions, labels); err != nil {
						p.log.Warnf("ensure volume %s (ceph-rbd): %v", vol.Name, err)
					}
				}
			}
		}
	}
}

func (p *Pool) deployService(ctx context.Context, sc *hiveswarm.Client, job map[string]string) {
	serviceName := p.resolveServiceName(job)
	image := job["image"]
	appID := job["app_id"]
	deploymentID := job["deployment_id"]

	p.publishProgress(appID, fmt.Sprintf("deploying service %s", serviceName))

	replicas := uint64(1)
	port := 3000
	labels := map[string]string{
		"hive.managed": "true",
		"hive.app_id":  appID,
	}

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   serviceName,
			Labels: labels,
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: image,
			},
			Networks: []swarm.NetworkAttachmentConfig{
				{Target: "hive-net"},
			},
		},
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{Replicas: &replicas},
		},
	}

	var app *store.App
	if p.store != nil && appID != "" {
		var appErr error
		app, appErr = p.store.GetApp(ctx, appID)
		if appErr == nil && app != nil {
			if app.Replicas > 0 {
				r := uint64(app.Replicas)
				spec.Mode.Replicated.Replicas = &r
			}
			if app.Port > 0 {
				port = app.Port
			}

			if app.CPULimit > 0 || app.MemoryLimit > 0 {
				resources := &swarm.ResourceRequirements{Limits: &swarm.Limit{}}
				if app.CPULimit > 0 {
					resources.Limits.NanoCPUs = int64(app.CPULimit * 1e9)
				}
				if app.MemoryLimit > 0 {
					resources.Limits.MemoryBytes = app.MemoryLimit
				}
				spec.TaskTemplate.Resources = resources
			}

			if app.HealthCheckPath != "" {
				intervalNs := int64(app.HealthCheckInterval) * 1e9
				if intervalNs == 0 {
					intervalNs = 30e9
				}
				timeoutNs := int64(10e9)
				retriesInt := 3
				spec.TaskTemplate.ContainerSpec.Healthcheck = &container.HealthConfig{
					Test:     []string{"CMD-SHELL", fmt.Sprintf("wget -qO- http://localhost:%d%s || exit 1", port, app.HealthCheckPath)},
					Interval: time.Duration(intervalNs),
					Timeout:  time.Duration(timeoutNs),
					Retries:  retriesInt,
				}
			}
		}

		domain := job["domain"]
		if domain == "" && app != nil {
			domain = app.Domain
		}
		if domain != "" {
			certResolver := p.certResolver(ctx)
			labels["traefik.enable"] = "true"
			labels[fmt.Sprintf("traefik.http.routers.%s.rule", serviceName)] = fmt.Sprintf("Host(`%s`)", domain)
			labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", serviceName)] = "websecure"
			labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", serviceName)] = certResolver
			labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", serviceName)] = fmt.Sprintf("%d", port)
		}

		appSecrets, err := p.store.ListAppSecrets(ctx, appID)
		if err == nil && len(appSecrets) > 0 {
			var secretRefs []*swarm.SecretReference
			for _, as := range appSecrets {
				sec, err := p.store.GetSecret(ctx, as.SecretID)
				if err != nil {
					p.log.Warnf("deploy: could not load secret %s: %v", as.SecretID, err)
					continue
				}
				target := as.Target
				if target == "" {
					target = sec.Name
				}
				mode := os.FileMode(as.Mode)
				if mode == 0 {
					mode = 0444
				}
				secretRefs = append(secretRefs, &swarm.SecretReference{
					SecretID:   sec.DockerSecretID,
					SecretName: sec.Name,
					File: &swarm.SecretReferenceFileTarget{
						Name: target,
						UID:  as.UID,
						GID:  as.GID,
						Mode: mode,
					},
				})
			}
			spec.TaskTemplate.ContainerSpec.Secrets = secretRefs
		}

		if env := p.buildMergedEnv(ctx, appID, job["env"]); len(env) > 0 {
			spec.TaskTemplate.ContainerSpec.Env = env
		}

		p.ensureAppVolumes(ctx, sc, appID)

		appVolumes, err := p.store.ListAppVolumes(ctx, appID)
		if err == nil && len(appVolumes) > 0 {
			var existingConstraints []string
			if app, innerErr := p.store.GetApp(ctx, appID); innerErr == nil && len(app.PlacementConstraints) > 0 {
				if err := json.Unmarshal(app.PlacementConstraints, &existingConstraints); err != nil {
					p.log.Warnf("deploy: failed to parse placement constraints: %v", err)
				}
			}

			resolvedMounts, addedConstraints, resolveErr := storage.ResolveVolumeMounts(ctx, p.store, appVolumes, existingConstraints)
			if resolveErr != nil {
				p.log.Warnf("deploy: volume resolution: %v", resolveErr)
			} else {
				spec.TaskTemplate.ContainerSpec.Mounts = resolvedMounts
				if len(addedConstraints) > 0 {
					if spec.TaskTemplate.Placement == nil {
						spec.TaskTemplate.Placement = &swarm.Placement{}
					}
					spec.TaskTemplate.Placement.Constraints = append(spec.TaskTemplate.Placement.Constraints, addedConstraints...)
				}
			}
		}

		if app != nil {
			var constraints []string
			if len(app.PlacementConstraints) > 0 {
				if err := json.Unmarshal(app.PlacementConstraints, &constraints); err != nil {
					p.log.Warnf("deploy: failed to parse placement constraints: %v", err)
				}
			}
			if len(constraints) > 0 {
				if spec.TaskTemplate.Placement == nil {
					spec.TaskTemplate.Placement = &swarm.Placement{}
				}
				spec.TaskTemplate.Placement.Constraints = append(spec.TaskTemplate.Placement.Constraints, constraints...)
			}

			p.appendAppLabels(app, labels)
			p.applyAppUpdateStrategy(app, &spec)
		}
	}

	if port > 0 {
		spec.EndpointSpec = &swarm.EndpointSpec{
			Ports: []swarm.PortConfig{{
				Protocol:      swarm.PortConfigProtocolTCP,
				TargetPort:    uint32(port),
				PublishedPort: uint32(port),
				PublishMode:   swarm.PortConfigPublishModeIngress,
			}},
		}
	}

	var placementConstraints []string
	if spec.TaskTemplate.Placement != nil {
		placementConstraints = spec.TaskTemplate.Placement.Constraints
	}
	if err := sc.ValidatePlacement(ctx, placementConstraints); err != nil {
		p.log.Errorf("deploy: preflight failed: %v", err)
		p.publishProgress(appID, "deployment failed: "+err.Error())
		p.finishDeployment(deploymentID, "failed", "preflight: "+err.Error())
		p.setAppFailed(appID)
		p.notifyDeployFailure(appID, job["name"], "preflight: "+err.Error())
		return
	}

	if err := p.applyServiceSpec(ctx, sc, serviceName, &spec, port); err != nil {
		p.publishProgress(appID, "deployment failed: "+err.Error())
		p.finishDeployment(deploymentID, "failed", err.Error())
		p.setAppFailed(appID)
		p.notifyDeployFailure(appID, job["name"], err.Error())
		return
	}

	p.publishProgress(appID, "deployment complete")
	p.finishDeployment(deploymentID, "success", "")
	if p.store != nil {
		if err := p.store.UpdateAppStatus(ctx, appID, "running"); err != nil {
			p.log.Warnf("failed to update app status: %v", err)
		}
	}
	p.notifyDeploySuccess(appID, job["name"])
}

func (p *Pool) applyServiceSpec(ctx context.Context, sc *hiveswarm.Client, serviceName string, spec *swarm.ServiceSpec, port int) error {
	exists, err := sc.ServiceExists(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("check service: %w", err)
	}

	tryAutoAssign := func() {
		if spec.EndpointSpec == nil {
			return
		}
		for i := range spec.EndpointSpec.Ports {
			spec.EndpointSpec.Ports[i].PublishedPort = 0
		}
	}

	if exists {
		svc, err := sc.GetService(ctx, serviceName)
		if err != nil || svc == nil {
			return fmt.Errorf("service lookup failed")
		}
		if err := sc.UpdateService(ctx, svc.ID, svc.Version, *spec); err != nil {
			if strings.Contains(err.Error(), "already allocated") && spec.EndpointSpec != nil {
				p.log.Warnf("deploy: port conflict on update, retrying with auto-assign")
				tryAutoAssign()
				if retryErr := sc.UpdateService(ctx, svc.ID, svc.Version, *spec); retryErr != nil {
					return retryErr
				}
				return nil
			}
			return err
		}
		return nil
	}

	if err := sc.CreateService(ctx, *spec); err != nil {
		if strings.Contains(err.Error(), "already allocated") && spec.EndpointSpec != nil {
			p.log.Warnf("deploy: port %d conflict, retrying with auto-assign", port)
			tryAutoAssign()
			if retryErr := sc.CreateService(ctx, *spec); retryErr != nil {
				return retryErr
			}
			return nil
		}
		return err
	}
	return nil
}

func (p *Pool) buildMergedEnv(ctx context.Context, appID, jobEnv string) []string {
	// Merge env from all sources (lowest priority first):
	// 1. Job-level env (from template deploy)
	// 2. App env vars (user-configured via UI/API)
	// 3. Service link env (auto-discovered connections)
	mergedEnv := make(map[string]string)

	if jobEnv != "" {
		var templateEnv map[string]string
		if err := json.Unmarshal([]byte(jobEnv), &templateEnv); err == nil {
			for k, v := range templateEnv {
				mergedEnv[k] = v
			}
		}
	}

	appEnvVars, err := p.store.ListAppEnvVars(ctx, appID)
	if err == nil {
		for _, ev := range appEnvVars {
			val, decErr := encryption.Decrypt(ev.ValueEncrypted)
			if decErr != nil {
				p.log.Warnf("deploy: decrypt env var %s: %v", ev.Key, decErr)
				continue
			}
			mergedEnv[ev.Key] = string(val)
		}
	}

	serviceLinkEnv, err := networking.ResolveServiceLinks(ctx, p.store, appID)
	if err == nil {
		for k, v := range serviceLinkEnv {
			mergedEnv[k] = v
		}
	}

	if len(mergedEnv) == 0 {
		return nil
	}
	env := make([]string, 0, len(mergedEnv))
	for k, v := range mergedEnv {
		env = append(env, k+"="+v)
	}
	return env
}

func (p *Pool) appendAppLabels(app *store.App, labels map[string]string) {
	var homepageLabels map[string]string
	if len(app.HomepageLabels) > 0 {
		if err := json.Unmarshal(app.HomepageLabels, &homepageLabels); err != nil {
			p.log.Warnf("unmarshal homepage labels: %v", err)
		}
	}
	for k, v := range homepageLabels {
		labels[k] = v
	}

	var extraLabels map[string]string
	if len(app.ExtraLabels) > 0 {
		if err := json.Unmarshal(app.ExtraLabels, &extraLabels); err != nil {
			p.log.Warnf("unmarshal extra labels: %v", err)
		}
	}
	for k, v := range extraLabels {
		labels[k] = v
	}
}

func (p *Pool) applyAppUpdateStrategy(app *store.App, spec *swarm.ServiceSpec) {
	updateDelay := 5 * time.Second
	if app.UpdateDelay != "" {
		if d, err := time.ParseDuration(app.UpdateDelay); err == nil {
			updateDelay = d
		}
	}

	parallelism := uint64(1)
	if app.UpdateParallelism > 0 {
		parallelism = uint64(app.UpdateParallelism)
	}

	failureAction := app.UpdateFailureAction
	if failureAction == "" {
		failureAction = "rollback"
	}

	updateOrder := app.UpdateOrder
	if updateOrder == "" {
		updateOrder = "stop-first"
	}

	spec.UpdateConfig = &swarm.UpdateConfig{
		Parallelism:   parallelism,
		Delay:         updateDelay,
		FailureAction: failureAction,
		Order:         updateOrder,
		Monitor:       5 * time.Second,
	}
	spec.RollbackConfig = &swarm.UpdateConfig{
		Parallelism:   1,
		Delay:         5 * time.Second,
		FailureAction: "pause",
		Order:         "stop-first",
	}
}

// resolveServiceName determines the Docker service name from the job.
// Preview deploys already carry the full service name; normal app deploys
// get the "hive-app-" prefix.
func (p *Pool) resolveServiceName(job map[string]string) string {
	if job["preview"] == "true" {
		return job["name"]
	}
	return "hive-app-" + job["name"]
}

func (p *Pool) removeService(ctx context.Context, sc *hiveswarm.Client, job map[string]string) {
	serviceName := p.resolveServiceName(job)
	svc, err := sc.GetService(ctx, serviceName)
	if err != nil || svc == nil {
		p.log.Warnf("remove: service %s not found", serviceName)
		return
	}
	if err := sc.RemoveService(ctx, svc.ID); err != nil {
		p.log.Errorf("remove: %v", err)
	}
}

func (p *Pool) provisionDatabase(ctx context.Context, sc *hiveswarm.Client, job map[string]string) {
	p.log.Infof("provisioning database: %s type=%s mode=%s", job["name"], job["db_type"], job["storage_mode"])
	p.publishProgress(job["db_id"], fmt.Sprintf("provisioning %s database %s", job["db_type"], job["name"]))

	provisioner := database.NewProvisioner(sc, p.log)
	opts := database.ProvisionOptions{
		StorageMode:   job["storage_mode"],
		StorageHostID: job["storage_host_id"],
		NodeID:        job["node_id"],
	}

	if opts.StorageMode == "remote" && opts.StorageHostID != "" && p.store != nil {
		sh, err := p.store.GetStorageHost(ctx, opts.StorageHostID)
		if err == nil {
			opts.NFSHost = sh.Address
			opts.NFSPath = sh.DefaultExportPath + "/hive-db-" + job["name"]
		}
	}

	connStr, err := provisioner.ProvisionWithOptions(ctx, job["name"], job["db_type"], job["version"], opts)
	if err != nil {
		p.log.Errorf("provision: %v", err)
		p.publishProgress(job["db_id"], "provisioning failed: "+err.Error())
		if p.store != nil {
			_ = p.store.UpdateManagedDatabaseStatus(ctx, job["db_id"], "failed")
		}
		return
	}

	if p.store != nil && connStr != "" {
		connEncrypted, err := encryption.Encrypt([]byte(connStr))
		if err == nil {
			_ = p.store.UpdateManagedDatabaseConnection(ctx, job["db_id"], connEncrypted)
		}
		_ = p.store.UpdateManagedDatabaseStatus(ctx, job["db_id"], "running")
	}
	p.publishProgress(job["db_id"], "database provisioned successfully")
}

func (p *Pool) handleBackup(msg *nats.Msg) {
	var raw map[string]any
	if err := json.Unmarshal(msg.Data, &raw); err != nil {
		p.log.Errorf("backup: invalid job: %v", err)
		return
	}
	job := toStringMap(raw)

	if job["action"] == "restore" {
		p.handleRestore(job)
		return
	}

	configID := job["config_id"]
	p.log.Infof("backup job received for config %s", configID)

	if p.store == nil {
		p.log.Error("backup: store not available on this worker")
		return
	}

	ctx := context.Background()

	config, err := p.store.GetBackupConfig(ctx, configID)
	if err != nil {
		p.log.Errorf("backup: load config %s: %v", configID, err)
		return
	}

	run := &store.BackupRun{ConfigID: configID, Status: "running"}
	if err := p.store.CreateBackupRun(ctx, run); err != nil {
		p.log.Errorf("backup: create run record: %v", err)
		return
	}

	var outputPath string
	var backupName string

	switch config.BackupType {
	case "volume":
		vol, err := p.store.GetVolume(ctx, config.VolumeID)
		if err != nil {
			p.log.Errorf("backup: load volume %s: %v", config.VolumeID, err)
			if err := p.store.UpdateBackupRun(ctx, run.ID, "failed", 0, ""); err != nil {
				p.log.Warnf("failed to update backup run: %v", err)
			}
			p.notifyBackupFailure(configID, "", err.Error())
			return
		}
		backupName = vol.Name
		outputDir := filepath.Join(p.cfg.BackupDir, vol.Name)
		fileRunner := backup.NewFileBackupRunner(p.log)
		outputPath, err = fileRunner.BackupVolume(ctx, vol.Name, outputDir)
		if err != nil {
			p.log.Errorf("backup: volume backup failed: %v", err)
			if err := p.store.UpdateBackupRun(ctx, run.ID, "failed", 0, ""); err != nil {
				p.log.Warnf("failed to update backup run: %v", err)
			}
			p.notifyBackupFailure(configID, vol.Name, err.Error())
			return
		}
	default:
		db, err := p.store.GetManagedDatabase(ctx, config.ResourceID)
		if err != nil {
			p.log.Errorf("backup: load database %s: %v", config.ResourceID, err)
			if err := p.store.UpdateBackupRun(ctx, run.ID, "failed", 0, ""); err != nil {
				p.log.Warnf("failed to update backup run: %v", err)
			}
			p.notifyBackupFailure(configID, "", err.Error())
			return
		}
		backupName = db.Name
		serviceName := fmt.Sprintf("hive-db-%s", db.Name)
		password := fmt.Sprintf("hive-%s-pass", db.Name)
		if len(db.ConnectionEncrypted) > 0 {
			if connBytes, decErr := encryption.Decrypt(db.ConnectionEncrypted); decErr == nil {
				connStr := string(connBytes)
				if idx := strings.Index(connStr, "://"); idx >= 0 {
					rest := connStr[idx+3:]
					if colonIdx := strings.Index(rest, ":"); colonIdx >= 0 {
						if atIdx := strings.Index(rest, "@"); atIdx > colonIdx {
							password = rest[colonIdx+1 : atIdx]
						}
					}
				}
			}
		}
		outputDir := filepath.Join(p.cfg.BackupDir, db.Name)

		runner := database.NewBackupRunner(p.log)
		outputPath, err = runner.BackupDatabase(ctx, db.DBType, serviceName, db.Name, db.Name, password, outputDir)
		if err != nil {
			p.log.Errorf("backup: run failed: %v", err)
			if err := p.store.UpdateBackupRun(ctx, run.ID, "failed", 0, ""); err != nil {
				p.log.Errorf("backup: update run status: %v", err)
			}
			p.notifyBackupFailure(configID, db.Name, err.Error())
			return
		}
	}

	fileInfo, statErr := os.Stat(outputPath)
	if statErr != nil {
		p.log.Warnf("backup: stat output file %s: %v", outputPath, statErr)
	}
	size := int64(0)
	if fileInfo != nil {
		size = fileInfo.Size()
	}

	targetPath := outputPath
	destination := config.Destination
	if destination == "" && config.S3Bucket != "" {
		destination = "s3"
	}
	if destination == "" {
		destination = "local"
	}

	switch destination {
	case "s3":
		s3Path := fmt.Sprintf("%s/%s", config.S3Prefix, filepath.Base(outputPath))
		targetPath = fmt.Sprintf("s3://%s/%s", config.S3Bucket, s3Path)

		s3Cfg := backup.S3Config{
			Endpoint:  os.Getenv("HIVE_S3_ENDPOINT"),
			AccessKey: os.Getenv("HIVE_S3_ACCESS_KEY"),
			SecretKey: os.Getenv("HIVE_S3_SECRET_KEY"),
			Bucket:    config.S3Bucket,
			UseSSL:    os.Getenv("HIVE_S3_USE_SSL") != "false",
		}
		if s3Cfg.Endpoint != "" && s3Cfg.AccessKey != "" {
			uploader, err := backup.NewS3Uploader(s3Cfg, p.log)
			if err == nil {
				f, err := os.Open(outputPath)
				if err == nil {
					if err := uploader.Upload(ctx, config.S3Bucket, s3Path, f, size); err != nil {
						p.log.Warnf("backup: S3 upload failed: %v", err)
					} else {
						p.log.Infof("backup: uploaded to %s", targetPath)
						_ = os.Remove(outputPath)
					}
					_ = f.Close()
				}
			}
		}
	case "nas":
		nasPath := config.NASPath
		if nasPath == "" {
			nasPath = filepath.Join(p.cfg.DataDir, "nas-backups", backupName)
		}
		nasWriter := backup.NewNASBackupWriter(p.log)
		if dest, err := nasWriter.CopyToNAS(ctx, outputPath, nasPath); err != nil {
			p.log.Warnf("backup: NAS copy failed: %v", err)
		} else {
			targetPath = dest
			_ = os.Remove(outputPath)
		}
		if config.RetentionDays > 0 {
			nasWriter.EnforceRetention(nasPath, config.RetentionDays)
		}
	case "local":
		if config.LocalPath != "" {
			nasWriter := backup.NewNASBackupWriter(p.log)
			if dest, err := nasWriter.CopyToNAS(ctx, outputPath, config.LocalPath); err != nil {
				p.log.Warnf("backup: local copy failed: %v", err)
			} else {
				targetPath = dest
				_ = os.Remove(outputPath)
			}
			if config.RetentionDays > 0 {
				nasWriter.EnforceRetention(config.LocalPath, config.RetentionDays)
			}
		}
	}

	if err := p.store.UpdateBackupRun(ctx, run.ID, "success", size, targetPath); err != nil {
		p.log.Errorf("backup: update run status: %v", err)
	}
	p.log.Infof("backup complete: config=%s path=%s size=%d", configID, targetPath, size)
	p.notifyBackupSuccess(configID, backupName, targetPath, size)
}

func (p *Pool) deployStack(ctx context.Context, sc *hiveswarm.Client, job map[string]string) {
	stackID := job["stack_id"]
	stackName := job["name"]

	if p.store == nil {
		p.log.Error("stack deploy: store not available")
		return
	}

	st, err := p.store.GetStack(ctx, stackID)
	if err != nil {
		p.log.Errorf("stack deploy: load stack %s: %v", stackID, err)
		return
	}

	cf, err := deploy.ParseCompose(st.ComposeContent)
	if err != nil {
		p.log.Errorf("stack deploy: parse compose: %v", err)
		if err := p.store.UpdateStack(ctx, &store.Stack{ID: stackID, Name: stackName, ComposeContent: st.ComposeContent, Status: "failed"}); err != nil {
			p.log.Warnf("failed to update stack status: %v", err)
		}
		return
	}

	services, err := deploy.ExtractServices(cf, stackName)
	if err != nil {
		p.log.Errorf("stack deploy: extract services: %v", err)
		return
	}

	// Create per-stack overlay network for inter-service DNS resolution
	stackNetName := stackName + "-net"
	if err := sc.EnsureNetwork(ctx, stackNetName, network.CreateOptions{
		Driver:     "overlay",
		Attachable: true,
		Labels:     map[string]string{"hive.managed": "true", "hive.stack_id": stackID},
	}); err != nil {
		p.log.Warnf("stack deploy: create stack network %s: %v", stackNetName, err)
	}

	// Create compose-defined networks
	composeNetMap := make(map[string]string) // compose network name -> actual Docker network name
	for netName, netDef := range cf.Networks {
		if netDef.External {
			composeNetMap[netName] = netName
			continue
		}
		actualName := fmt.Sprintf("%s_%s", stackName, netName)
		driver := netDef.Driver
		if driver == "" {
			driver = "overlay"
		}
		if err := sc.EnsureNetwork(ctx, actualName, network.CreateOptions{
			Driver:     driver,
			Attachable: true,
			Labels:     map[string]string{"hive.managed": "true", "hive.stack_id": stackID},
		}); err != nil {
			p.log.Warnf("stack deploy: create compose network %s: %v", actualName, err)
		}
		composeNetMap[netName] = actualName
	}

	anyServiceFailed := false
	stackDomain := strings.TrimSpace(job["domain"])
	stackDomainAssigned := false

	for _, svc := range services {
		replicas := uint64(svc.Replicas)
		var env []string
		for k, v := range svc.Environment {
			env = append(env, k+"="+v)
		}

		svcLabels := map[string]string{
			"hive.managed":  "true",
			"hive.stack_id": stackID,
		}
		for k, v := range svc.Labels {
			svcLabels[k] = v
		}
		for k, v := range svc.DeployLabels {
			svcLabels[k] = v
		}

		hasTraefikRule := false
		for k := range svcLabels {
			if strings.Contains(k, "traefik.http.routers.") && strings.HasSuffix(k, ".rule") {
				hasTraefikRule = true
				break
			}
		}

		if len(svc.Ports) > 0 {
			svcLabels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", svc.Name)] = fmt.Sprintf("%d", svc.Ports[0].Target)
			if hasTraefikRule || svcLabels["traefik.enable"] == "true" {
				svcLabels["traefik.enable"] = "true"
			}
		}

		if stackDomain != "" && !hasTraefikRule && !stackDomainAssigned && len(svc.Ports) > 0 {
			certResolver := p.certResolver(ctx)
			svcLabels["traefik.enable"] = "true"
			svcLabels[fmt.Sprintf("traefik.http.routers.%s.rule", svc.Name)] = fmt.Sprintf("Host(`%s`)", stackDomain)
			svcLabels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", svc.Name)] = "websecure"
			svcLabels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", svc.Name)] = certResolver
			stackDomainAssigned = true
		}

		var mounts []mount.Mount
		for _, vm := range svc.Volumes {
			if vm.Source == "" {
				// Anonymous volume (single-path in compose, e.g. "/data")
				mounts = append(mounts, mount.Mount{
					Type:     mount.TypeVolume,
					Target:   vm.Target,
					ReadOnly: vm.ReadOnly,
				})
				continue
			}

			if strings.HasPrefix(vm.Source, "/") {
				mounts = append(mounts, mount.Mount{
					Type:     mount.TypeBind,
					Source:   vm.Source,
					Target:   vm.Target,
					ReadOnly: vm.ReadOnly,
				})
				continue
			}

			// Skip relative paths (./data, ~/data) - not valid in Swarm
			if strings.HasPrefix(vm.Source, ".") || strings.HasPrefix(vm.Source, "~") {
				p.log.Warnf("stack deploy: skipping relative volume path %q for service %s", vm.Source, svc.Name)
				continue
			}

			volName := fmt.Sprintf("%s_%s", stackName, vm.Source)
			if volDef, ok := cf.Volumes[vm.Source]; ok && !volDef.External && volDef.Driver != "" {
				labels := map[string]string{"hive.managed": "true", "hive.stack_id": stackID}
				if volDef.DriverOpts != nil {
					if volDef.DriverOpts["type"] == "nfs" {
						addr := volDef.DriverOpts["o"]
						device := volDef.DriverOpts["device"]
						if addr != "" && device != "" {
							if _, err := sc.CreateNFSVolume(ctx, volName, addr, device, "", labels); err != nil {
								p.log.Warnf("stack deploy: create NFS volume %s: %v", volName, err)
							}
						}
					} else if volDef.DriverOpts["type"] == "cifs" {
						device := volDef.DriverOpts["device"]
						opts := volDef.DriverOpts["o"]
						if device != "" {
							if _, vErr := sc.CreateVolume(ctx, volName, "local", volDef.DriverOpts, labels); vErr != nil {
								p.log.Warnf("stack deploy: create CIFS volume %s: %v", volName, vErr)
							}
							_ = opts
						}
					} else {
						if _, vErr := sc.CreateVolume(ctx, volName, volDef.Driver, volDef.DriverOpts, labels); vErr != nil {
							p.log.Warnf("stack deploy: create volume %s with driver %s: %v", volName, volDef.Driver, vErr)
						}
					}
				} else {
					if _, vErr := sc.CreateVolume(ctx, volName, volDef.Driver, nil, labels); vErr != nil {
						p.log.Warnf("stack deploy: create volume %s with driver %s: %v", volName, volDef.Driver, vErr)
					}
				}
			}
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeVolume,
				Source:   volName,
				Target:   vm.Target,
				ReadOnly: vm.ReadOnly,
			})
		}

		containerSpec := &swarm.ContainerSpec{
			Image:  svc.Image,
			Env:    env,
			Mounts: mounts,
		}

		if len(svc.Command) > 0 {
			if len(svc.Command) == 1 {
				containerSpec.Command = svc.Command
			} else {
				containerSpec.Command = svc.Command[:1]
				containerSpec.Args = svc.Command[1:]
			}
		}
		if len(svc.Entrypoint) > 0 {
			containerSpec.Command = svc.Entrypoint
			if len(svc.Command) > 0 {
				containerSpec.Args = svc.Command
			}
		}
		if svc.User != "" {
			containerSpec.User = svc.User
		}
		if svc.WorkingDir != "" {
			containerSpec.Dir = svc.WorkingDir
		}
		if svc.Hostname != "" {
			containerSpec.Hostname = svc.Hostname
		}
		if len(svc.CapAdd) > 0 {
			containerSpec.CapabilityAdd = svc.CapAdd
		}
		if len(svc.CapDrop) > 0 {
			containerSpec.CapabilityDrop = svc.CapDrop
		}

		if svc.Healthcheck != nil {
			test := deploy.ParseHealthcheckTest(svc.Healthcheck.Test)
			if len(test) > 0 {
				hc := &container.HealthConfig{
					Test: test,
				}
				if d := deploy.ParseDuration(svc.Healthcheck.Interval); d > 0 {
					hc.Interval = d
				}
				if d := deploy.ParseDuration(svc.Healthcheck.Timeout); d > 0 {
					hc.Timeout = d
				}
				if svc.Healthcheck.Retries > 0 {
					hc.Retries = svc.Healthcheck.Retries
				}
				if d := deploy.ParseDuration(svc.Healthcheck.StartPeriod); d > 0 {
					hc.StartPeriod = d
				}
				containerSpec.Healthcheck = hc
			}
		}

		svcNetworks := []swarm.NetworkAttachmentConfig{
			{Target: stackNetName, Aliases: []string{svc.ShortName}},
			{Target: "hive-net"},
		}
		for _, netName := range svc.Networks {
			if actual, ok := composeNetMap[netName]; ok {
				alreadyAdded := false
				for _, n := range svcNetworks {
					if n.Target == actual {
						alreadyAdded = true
						break
					}
				}
				if !alreadyAdded {
					svcNetworks = append(svcNetworks, swarm.NetworkAttachmentConfig{Target: actual})
				}
			}
		}

		taskSpec := swarm.TaskSpec{
			ContainerSpec: containerSpec,
			Networks:      svcNetworks,
		}

		if svc.Resources != nil {
			taskSpec.Resources = &swarm.ResourceRequirements{}
			if svc.Resources.Limits != nil {
				taskSpec.Resources.Limits = &swarm.Limit{}
				if n := deploy.ParseCPUs(svc.Resources.Limits.CPUs); n > 0 {
					taskSpec.Resources.Limits.NanoCPUs = n
				}
				if m := deploy.ParseMemory(svc.Resources.Limits.Memory); m > 0 {
					taskSpec.Resources.Limits.MemoryBytes = m
				}
			}
			if svc.Resources.Reservations != nil {
				taskSpec.Resources.Reservations = &swarm.Resources{}
				if n := deploy.ParseCPUs(svc.Resources.Reservations.CPUs); n > 0 {
					taskSpec.Resources.Reservations.NanoCPUs = n
				}
				if m := deploy.ParseMemory(svc.Resources.Reservations.Memory); m > 0 {
					taskSpec.Resources.Reservations.MemoryBytes = m
				}
			}
		}

		if svc.RestartPolicy != nil {
			rp := &swarm.RestartPolicy{}
			switch svc.RestartPolicy.Condition {
			case "none":
				c := swarm.RestartPolicyConditionNone
				rp.Condition = c
			case "on-failure":
				c := swarm.RestartPolicyConditionOnFailure
				rp.Condition = c
			default:
				c := swarm.RestartPolicyConditionAny
				rp.Condition = c
			}
			if d := deploy.ParseDuration(svc.RestartPolicy.Delay); d > 0 {
				rp.Delay = &d
			}
			if svc.RestartPolicy.MaxAttempts > 0 {
				ma := uint64(svc.RestartPolicy.MaxAttempts)
				rp.MaxAttempts = &ma
			}
			if d := deploy.ParseDuration(svc.RestartPolicy.Window); d > 0 {
				rp.Window = &d
			}
			taskSpec.RestartPolicy = rp
		}

		if len(svc.Constraints) > 0 {
			taskSpec.Placement = &swarm.Placement{
				Constraints: svc.Constraints,
			}
		}

		spec := swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   svc.Name,
				Labels: svcLabels,
			},
			TaskTemplate: taskSpec,
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		}

		if svc.ServiceMode == "global" {
			spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
		}

		if svc.UpdateConfig != nil {
			spec.UpdateConfig = &swarm.UpdateConfig{}
			if svc.UpdateConfig.Parallelism > 0 {
				p := uint64(svc.UpdateConfig.Parallelism)
				spec.UpdateConfig.Parallelism = p
			}
			if d := deploy.ParseDuration(svc.UpdateConfig.Delay); d > 0 {
				spec.UpdateConfig.Delay = d
			}
			switch svc.UpdateConfig.FailureAction {
			case "rollback":
				spec.UpdateConfig.FailureAction = "rollback"
			case "pause":
				spec.UpdateConfig.FailureAction = "pause"
			case "continue":
				spec.UpdateConfig.FailureAction = "continue"
			}
			if svc.UpdateConfig.Order != "" {
				spec.UpdateConfig.Order = svc.UpdateConfig.Order
			}
		}

		if len(svc.Ports) > 0 {
			var portConfigs []swarm.PortConfig
			for _, pm := range svc.Ports {
				published := uint32(pm.Published)
				if published == 0 {
					published = uint32(pm.Target)
				}
				proto := swarm.PortConfigProtocolTCP
				if pm.Protocol == "udp" {
					proto = swarm.PortConfigProtocolUDP
				}
				portConfigs = append(portConfigs, swarm.PortConfig{
					Protocol:      proto,
					TargetPort:    uint32(pm.Target),
					PublishedPort: published,
					PublishMode:   swarm.PortConfigPublishModeIngress,
				})
			}
			spec.EndpointSpec = &swarm.EndpointSpec{Ports: portConfigs}
		}

		exists, existsErr := sc.ServiceExists(ctx, svc.Name)
		if existsErr != nil {
			p.log.Warnf("stack deploy: check service %s: %v", svc.Name, existsErr)
			anyServiceFailed = true
		}
		if exists {
			existing, err := sc.GetService(ctx, svc.Name)
			if err == nil && existing != nil {
				if err := sc.UpdateService(ctx, existing.ID, existing.Version, spec); err != nil {
					if strings.Contains(err.Error(), "already allocated") && spec.EndpointSpec != nil {
						p.log.Warnf("stack deploy: port conflict on %s, retrying with auto-assign", svc.Name)
						for i := range spec.EndpointSpec.Ports {
							spec.EndpointSpec.Ports[i].PublishedPort = 0
						}
						if retryErr := sc.UpdateService(ctx, existing.ID, existing.Version, spec); retryErr != nil {
							p.log.Warnf("stack deploy: update service %s: %v", svc.Name, retryErr)
							anyServiceFailed = true
						}
					} else {
						p.log.Warnf("stack deploy: update service %s: %v", svc.Name, err)
						anyServiceFailed = true
					}
				}
			} else if err != nil {
				anyServiceFailed = true
			}
		} else {
			if err := sc.CreateService(ctx, spec); err != nil {
				if strings.Contains(err.Error(), "already allocated") && spec.EndpointSpec != nil {
					p.log.Warnf("stack deploy: port conflict on %s, retrying with auto-assign", svc.Name)
					for i := range spec.EndpointSpec.Ports {
						spec.EndpointSpec.Ports[i].PublishedPort = 0
					}
					if retryErr := sc.CreateService(ctx, spec); retryErr != nil {
						p.log.Errorf("stack deploy: create service %s: %v", svc.Name, retryErr)
						anyServiceFailed = true
					}
				} else {
					p.log.Errorf("stack deploy: create service %s: %v", svc.Name, err)
					anyServiceFailed = true
				}
			}
		}
	}

	finalStatus := "running"
	if anyServiceFailed {
		finalStatus = "failed"
	}
	if err := p.store.UpdateStackStatus(ctx, stackID, finalStatus); err != nil {
		p.log.Warnf("failed to update stack status: %v", err)
	}
	if anyServiceFailed {
		p.log.Warnf("stack deployed with failures: %s (%d services)", stackName, len(services))
	} else {
		p.log.Infof("stack deployed: %s (%d services)", stackName, len(services))
	}
}

func (p *Pool) removeStack(ctx context.Context, sc *hiveswarm.Client, job map[string]string) {
	stackName := job["name"]

	svcs, err := sc.ListServices(ctx)
	if err != nil {
		p.log.Errorf("stack remove: list services: %v", err)
		return
	}

	for _, svc := range svcs {
		if svc.Spec.Labels["hive.stack_id"] == job["stack_id"] {
			if err := sc.RemoveService(ctx, svc.ID); err != nil {
				p.log.Warnf("stack remove: remove service %s: %v", svc.Spec.Name, err)
			}
		}
	}

	stackNetName := stackName + "-net"
	if err := sc.RemoveNetwork(ctx, stackNetName); err != nil {
		p.log.Warnf("stack remove: remove network %s: %v", stackNetName, err)
	}

	p.log.Infof("stack removed: %s", stackName)
}

func (p *Pool) handleCleanup(msg *nats.Msg) {
	p.log.Info("cleanup job: pruning unused images and containers")

	// Prune stopped containers
	containerPrune := exec.Command("docker", "container", "prune", "-f")
	if output, err := containerPrune.CombinedOutput(); err != nil {
		p.log.Warnf("cleanup container prune: %v: %s", err, string(output))
	}

	// Prune dangling images only (not all unused)
	imagePrune := exec.Command("docker", "image", "prune", "-f")
	if output, err := imagePrune.CombinedOutput(); err != nil {
		p.log.Warnf("cleanup image prune: %v: %s", err, string(output))
	}

	// Prune dangling build cache
	buildPrune := exec.Command("docker", "builder", "prune", "-f", "--filter", "until=24h")
	if output, err := buildPrune.CombinedOutput(); err != nil {
		p.log.Warnf("cleanup builder prune: %v: %s", err, string(output))
	}

	// Prune unused networks (but not hive-net)
	netPrune := exec.Command("docker", "network", "prune", "-f", "--filter", "label!=hive.managed=true")
	if output, err := netPrune.CombinedOutput(); err != nil {
		p.log.Warnf("cleanup network prune: %v: %s", err, string(output))
	}

	p.log.Info("cleanup job: completed (volumes preserved)")
}

func (p *Pool) handleHealth(msg *nats.Msg) {
	p.log.Debug("health check")
}

func (p *Pool) publishProgress(appID, message string) {
	data, marshalErr := json.Marshal(map[string]string{
		"app_id":  appID,
		"message": message,
	})
	if marshalErr != nil {
		p.log.Errorf("failed to marshal progress: %v", marshalErr)
		return
	}
	if err := p.nc.Publish("hive.progress."+appID, data); err != nil {
		p.log.Errorf("failed to publish progress: %v", err)
	}
}

func (p *Pool) finishDeployment(deploymentID, status, logs string) {
	if p.store == nil || deploymentID == "" {
		return
	}
	ctx := context.Background()
	if err := p.store.UpdateDeploymentStatus(ctx, deploymentID, status, logs); err != nil {
		p.log.Warnf("failed to update deployment status: %v", err)
	}
}

func (p *Pool) setAppFailed(appID string) {
	if p.store == nil || appID == "" {
		return
	}
	if err := p.store.UpdateAppStatus(context.Background(), appID, "failed"); err != nil {
		p.log.Warnf("failed to set app status to failed: %v", err)
	}
}

func (p *Pool) certResolver(ctx context.Context) string {
	if p.store != nil {
		if mode, err := p.store.GetSetting(ctx, "ingress_mode"); err == nil {
			if mode == "cloudflare_tunnel" || mode == "both" {
				return "cloudflare"
			}
		}
	}
	return "letsencrypt"
}

func (p *Pool) appendDeploymentLog(deploymentID, logs string) {
	if p.store == nil || deploymentID == "" {
		return
	}
	ctx := context.Background()
	if err := p.store.AppendDeploymentLogs(ctx, deploymentID, logs); err != nil {
		p.log.Warnf("failed to append deployment logs: %v", err)
	}
}

func (p *Pool) notifyDeploySuccess(appID, appName string) {
	if p.store == nil {
		return
	}
	d := notify.NewDispatcher(p.store, p.log)
	d.SendForApp(context.Background(), appID, notify.Event{
		Type:    "deploy.success",
		Title:   fmt.Sprintf("Deployment Successful: %s", appName),
		Message: fmt.Sprintf("App **%s** has been deployed successfully.", appName),
	})
}

func (p *Pool) notifyDeployFailure(appID, appName, reason string) {
	if p.store == nil {
		return
	}
	d := notify.NewDispatcher(p.store, p.log)
	d.SendForApp(context.Background(), appID, notify.Event{
		Type:    "deploy.failure",
		Title:   fmt.Sprintf("Deployment Failed: %s", appName),
		Message: fmt.Sprintf("App **%s** deployment failed: %s", appName, reason),
	})
}

func (p *Pool) notifyBackupSuccess(configID, dbName, path string, size int64) {
	if p.store == nil {
		return
	}
	d := notify.NewDispatcher(p.store, p.log)
	d.SendForBackup(context.Background(), configID, notify.Event{
		Type:    "backup.success",
		Title:   fmt.Sprintf("Backup Successful: %s", dbName),
		Message: fmt.Sprintf("**%s** backed up to %s (%d bytes)", dbName, path, size),
	})
}

func (p *Pool) notifyBackupFailure(configID, dbName, reason string) {
	if p.store == nil {
		return
	}
	d := notify.NewDispatcher(p.store, p.log)
	d.SendForBackup(context.Background(), configID, notify.Event{
		Type:    "backup.failure",
		Title:   fmt.Sprintf("Backup Failed: %s", dbName),
		Message: fmt.Sprintf("**%s** backup failed: %s", dbName, reason),
	})
}

func (p *Pool) handleRestore(job map[string]string) {
	configID := job["config_id"]
	runID := job["run_id"]
	p.log.Infof("restore job received: config=%s run=%s", configID, runID)

	if p.store == nil {
		p.log.Error("restore: store not available on this worker")
		return
	}

	ctx := context.Background()

	config, err := p.store.GetBackupConfig(ctx, configID)
	if err != nil {
		p.log.Errorf("restore: load config %s: %v", configID, err)
		return
	}

	run, err := p.store.GetBackupRun(ctx, runID)
	if err != nil {
		p.log.Errorf("restore: load run %s: %v", runID, err)
		return
	}

	restoreRun := &store.BackupRun{ConfigID: configID, Status: "restoring"}
	if err := p.store.CreateBackupRun(ctx, restoreRun); err != nil {
		p.log.Errorf("restore: create run record: %v", err)
		return
	}

	backupPath := run.TargetPath
	isS3 := len(backupPath) > 5 && backupPath[:5] == "s3://"
	if isS3 {
		localPath := filepath.Join(p.cfg.BackupDir, "restores", filepath.Base(backupPath))
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			p.log.Errorf("restore: create dir: %v", err)
			return
		}

		s3Cfg := backup.S3Config{
			Endpoint:  os.Getenv("HIVE_S3_ENDPOINT"),
			AccessKey: os.Getenv("HIVE_S3_ACCESS_KEY"),
			SecretKey: os.Getenv("HIVE_S3_SECRET_KEY"),
			Bucket:    config.S3Bucket,
			UseSSL:    os.Getenv("HIVE_S3_USE_SSL") != "false",
		}
		downloader, err := backup.NewS3Downloader(s3Cfg, p.log)
		if err != nil {
			p.log.Errorf("restore: create S3 downloader: %v", err)
			if err := p.store.UpdateBackupRun(ctx, restoreRun.ID, "restore_failed", 0, ""); err != nil {
				p.log.Errorf("restore: update run status: %v", err)
			}
			p.notifyRestoreFailure(configID, err.Error())
			return
		}

		s3Key := strings.TrimPrefix(backupPath, "s3://"+config.S3Bucket+"/")
		if err := downloader.Download(ctx, config.S3Bucket, s3Key, localPath); err != nil {
			p.log.Errorf("restore: S3 download failed: %v", err)
			if err := p.store.UpdateBackupRun(ctx, restoreRun.ID, "restore_failed", 0, ""); err != nil {
				p.log.Errorf("restore: update run status: %v", err)
			}
			p.notifyRestoreFailure(configID, err.Error())
			return
		}
		defer func() { _ = os.Remove(localPath) }()
		backupPath = localPath
	}

	runner := backup.NewRestoreRunner(p.log)

	switch config.BackupType {
	case "volume":
		vol, err := p.store.GetVolume(ctx, config.VolumeID)
		if err != nil {
			p.log.Errorf("restore: load volume %s: %v", config.VolumeID, err)
			if err := p.store.UpdateBackupRun(ctx, restoreRun.ID, "restore_failed", 0, ""); err != nil {
				p.log.Errorf("restore: update run status: %v", err)
			}
			p.notifyRestoreFailure(configID, err.Error())
			return
		}
		if err := runner.RestoreVolume(ctx, vol.Name, backupPath); err != nil {
			p.log.Errorf("restore: volume restore failed: %v", err)
			if err := p.store.UpdateBackupRun(ctx, restoreRun.ID, "restore_failed", 0, ""); err != nil {
				p.log.Errorf("restore: update run status: %v", err)
			}
			p.notifyRestoreFailure(configID, err.Error())
			return
		}
	default:
		db, err := p.store.GetManagedDatabase(ctx, config.ResourceID)
		if err != nil {
			p.log.Errorf("restore: load database %s: %v", config.ResourceID, err)
			if err := p.store.UpdateBackupRun(ctx, restoreRun.ID, "restore_failed", 0, ""); err != nil {
				p.log.Errorf("restore: update run status: %v", err)
			}
			p.notifyRestoreFailure(configID, err.Error())
			return
		}
		serviceName := fmt.Sprintf("hive-db-%s", db.Name)
		password := fmt.Sprintf("hive-%s-pass", db.Name)
		if err := runner.RestoreDatabase(ctx, db.DBType, serviceName, db.Name, db.Name, password, backupPath); err != nil {
			p.log.Errorf("restore: database restore failed: %v", err)
			if err := p.store.UpdateBackupRun(ctx, restoreRun.ID, "restore_failed", 0, ""); err != nil {
				p.log.Errorf("restore: update run status: %v", err)
			}
			p.notifyRestoreFailure(configID, err.Error())
			return
		}
	}

	if err := p.store.UpdateBackupRun(ctx, restoreRun.ID, "restored", 0, run.TargetPath); err != nil {
		p.log.Errorf("restore: update run status: %v", err)
	}
	p.log.Infof("restore complete: config=%s from run=%s", configID, runID)
	p.notifyRestoreSuccess(configID, run.TargetPath)
}

func (p *Pool) notifyRestoreSuccess(configID, path string) {
	if p.store == nil {
		return
	}
	d := notify.NewDispatcher(p.store, p.log)
	d.SendForBackup(context.Background(), configID, notify.Event{
		Type:    "restore.success",
		Title:   "Restore Successful",
		Message: fmt.Sprintf("Backup restored successfully from %s", path),
	})
}

func (p *Pool) notifyRestoreFailure(configID, reason string) {
	if p.store == nil {
		return
	}
	d := notify.NewDispatcher(p.store, p.log)
	d.SendForBackup(context.Background(), configID, notify.Event{
		Type:    "restore.failure",
		Title:   "Restore Failed",
		Message: fmt.Sprintf("Backup restore failed: %s", reason),
	})
}

func generateSecurePassword() (string, error) {
	b := make([]byte, 24)
	if _, err := cryptorand.Read(b); err != nil {
		return "", fmt.Errorf("generate secure password: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (p *Pool) handleMaintenance(msg *nats.Msg) {
	var raw map[string]any
	if err := json.Unmarshal(msg.Data, &raw); err != nil {
		p.log.Errorf("maintenance: invalid job: %v", err)
		return
	}
	job := toStringMap(raw)
	taskID := job["task_id"]
	taskType := job["type"]
	p.log.Infof("maintenance job: type=%s task=%s", taskType, taskID)

	ctx := context.Background()
	if p.store == nil {
		return
	}

	// Fetch task to get type if not in message (e.g. from scheduler)
	if taskType == "" {
		task, err := p.store.GetMaintenanceTask(ctx, taskID)
		if err != nil || task == nil {
			p.log.Errorf("maintenance: task not found: %s", taskID)
			return
		}
		taskType = task.Type
	}

	run := &store.MaintenanceRun{TaskID: taskID, Status: "running"}
	if err := p.store.CreateMaintenanceRun(ctx, run); err != nil {
		p.log.Errorf("maintenance: create run: %v", err)
		return
	}

	var details string
	var err error
	switch taskType {
	case "image_prune":
		details, err = maintenance.RunImagePrune(ctx, p.log)
	case "db_vacuum":
		details, err = maintenance.RunDBVacuum(ctx, p.cfg.DatabaseURL, p.log)
	default:
		details = "unknown task type: " + taskType
		err = fmt.Errorf("%s", details)
	}

	status := "success"
	if err != nil {
		status = "failed"
		details = err.Error()
	}
	if err := p.store.UpdateMaintenanceRun(ctx, run.ID, status, details); err != nil {
		p.log.Warnf("failed to update maintenance run: %v", err)
	}
	if err := p.store.UpdateMaintenanceTaskLastRun(ctx, taskID, status); err != nil {
		p.log.Warnf("failed to update maintenance task last run: %v", err)
	}
}

func (p *Pool) gitHubCloneToken(ctx context.Context) (string, error) {
	apps, err := p.store.ListGitHubApps(ctx)
	if err != nil || len(apps) == 0 {
		return "", fmt.Errorf("no GitHub App")
	}
	app := apps[0]
	if app.InstallationID == 0 {
		return "", fmt.Errorf("GitHub App not installed")
	}
	fullApp, err := p.store.GetGitHubApp(ctx, app.ID)
	if err != nil {
		return "", err
	}
	pemKey, err := encryption.Decrypt(fullApp.PemEncrypted)
	if err != nil {
		return "", err
	}
	return github.GenerateInstallationToken(fullApp.AppID, pemKey, fullApp.InstallationID)
}

func (p *Pool) patCloneToken(ctx context.Context, sourceID string) string {
	gs, err := p.store.GetGitSource(ctx, sourceID)
	if err != nil || gs == nil {
		return ""
	}
	token, err := encryption.Decrypt(gs.TokenEncrypted)
	if err != nil {
		return ""
	}
	return string(token)
}

func injectTokenInURL(repoURL, token string) string {
	if strings.HasPrefix(repoURL, "https://") {
		return strings.Replace(repoURL, "https://", "https://x-access-token:"+token+"@", 1)
	}
	if strings.HasPrefix(repoURL, "http://") {
		return strings.Replace(repoURL, "http://", "http://x-access-token:"+token+"@", 1)
	}
	return repoURL
}
