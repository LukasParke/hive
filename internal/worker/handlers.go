package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/backup"
	"github.com/lholliger/hive/internal/database"
	"github.com/lholliger/hive/internal/deploy"
	"github.com/lholliger/hive/internal/maintenance"
	"github.com/lholliger/hive/internal/networking"
	"github.com/lholliger/hive/internal/notify"
	"github.com/lholliger/hive/internal/storage"
	"github.com/lholliger/hive/internal/store"
	hiveswarm "github.com/lholliger/hive/internal/swarm"
)

func (p *Pool) handleBuild(msg *nats.Msg) {
	var job map[string]string
	if err := json.Unmarshal(msg.Data, &job); err != nil {
		p.log.Errorf("build: invalid job: %v", err)
		return
	}
	appID := job["app_id"]
	deploymentID := job["deployment_id"]
	p.log.Infof("build job received: app=%s type=%s", appID, job["deploy_type"])

	p.publishProgress(appID, "cloning repository...")
	buildDir := filepath.Join(p.cfg.DataDir, "builds", job["name"])
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		p.log.Errorf("build: failed to create build dir: %v", err)
		return
	}
	defer func() { _ = os.RemoveAll(buildDir) }()

	repo := job["git_repo"]
	branch := job["git_branch"]
	if branch == "" {
		branch = "main"
	}

	cloneCmd := exec.Command("git", "clone", "--depth=1", "--branch", branch, repo, buildDir)
	if output, err := cloneCmd.CombinedOutput(); err != nil {
		p.publishProgress(appID, fmt.Sprintf("clone failed: %s", string(output)))
		p.log.Errorf("build: clone failed: %v", err)
		p.finishDeployment(deploymentID, "failed", string(output))
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
			return
		}
		p.publishProgress(appID, "image pushed to registry")
	}

	p.appendDeploymentLog(deploymentID, buildLog)

	deployJob, _ := json.Marshal(map[string]string{
		"action":        "deploy",
		"app_id":        appID,
		"deployment_id": deploymentID,
		"deploy_type":   "image",
		"image":         imageName,
		"name":          job["name"],
		"domain":        job["domain"],
	})
	if err := p.nc.Publish("hive.deploy", deployJob); err != nil {
		p.log.Errorf("failed to publish deploy job: %v", err)
	}
	p.publishProgress(appID, "build complete, deploying...")
}

func (p *Pool) handleDeploy(msg *nats.Msg) {
	var job map[string]string
	if err := json.Unmarshal(msg.Data, &job); err != nil {
		p.log.Errorf("deploy: invalid job: %v", err)
		return
	}

	action := job["action"]
	p.log.Infof("deploy job: action=%s app=%s", action, job["name"])

	sc, err := hiveswarm.NewClient(p.log)
	if err != nil {
		p.log.Errorf("deploy: docker client error: %v", err)
		return
	}
	defer func() { _ = sc.Close() }()

	ctx := context.Background()

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
	default:
		p.log.Warnf("deploy: unknown action: %s", action)
	}
}

func (p *Pool) deployService(ctx context.Context, sc *hiveswarm.Client, job map[string]string) {
	serviceName := "hive-app-" + job["name"]
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
	if domain := job["domain"]; domain != "" {
		labels["traefik.enable"] = "true"
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", serviceName)] = fmt.Sprintf("Host(`%s`)", domain)
		labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", serviceName)] = "websecure"
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", serviceName)] = "letsencrypt"
		labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", serviceName)] = fmt.Sprintf("%d", port)
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

	if p.store != nil && appID != "" {
		app, err := p.store.GetApp(ctx, appID)
		if err == nil && app != nil {
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

		serviceLinkEnv, err := networking.ResolveServiceLinks(ctx, p.store, appID)
		if err == nil && len(serviceLinkEnv) > 0 {
			var env []string
			for k, v := range serviceLinkEnv {
				env = append(env, k+"="+v)
			}
			spec.TaskTemplate.ContainerSpec.Env = env
		}

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

		if app, err := p.store.GetApp(ctx, appID); err == nil && app != nil {
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

			updateDelay := time.Duration(5 * time.Second)
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
	}

	exists, err := sc.ServiceExists(ctx, serviceName)
	if err != nil {
		p.log.Errorf("deploy: check service: %v", err)
		p.publishProgress(appID, "deployment failed: "+err.Error())
		p.finishDeployment(deploymentID, "failed", err.Error())
		p.notifyDeployFailure(appID, job["name"], err.Error())
		return
	}

	if exists {
		svc, err := sc.GetService(ctx, serviceName)
		if err != nil || svc == nil {
			p.log.Errorf("deploy: get service: %v", err)
			p.finishDeployment(deploymentID, "failed", "service lookup failed")
			return
		}
		if err := sc.UpdateService(ctx, svc.ID, svc.Version, spec); err != nil {
			p.publishProgress(appID, "deployment failed: "+err.Error())
			p.finishDeployment(deploymentID, "failed", err.Error())
			p.notifyDeployFailure(appID, job["name"], err.Error())
			return
		}
	} else {
		if err := sc.CreateService(ctx, spec); err != nil {
			p.publishProgress(appID, "deployment failed: "+err.Error())
			p.finishDeployment(deploymentID, "failed", err.Error())
			p.notifyDeployFailure(appID, job["name"], err.Error())
			return
		}
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

func (p *Pool) removeService(ctx context.Context, sc *hiveswarm.Client, job map[string]string) {
	serviceName := "hive-app-" + job["name"]
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
	p.log.Infof("provisioning database: %s type=%s", job["name"], job["db_type"])
	p.publishProgress(job["db_id"], fmt.Sprintf("provisioning %s database %s", job["db_type"], job["name"]))

	dbImages := map[string]string{
		"postgres": "postgres:16-alpine",
		"mysql":    "mysql:8",
		"redis":    "redis:7-alpine",
		"mongo":    "mongo:7",
	}

	image, ok := dbImages[job["db_type"]]
	if !ok {
		p.log.Errorf("provision: unsupported db type: %s", job["db_type"])
		return
	}
	if v := job["version"]; v != "" && v != "latest" {
		image = fmt.Sprintf("%s:%s", job["db_type"], v)
	}

	serviceName := fmt.Sprintf("hive-db-%s", job["name"])
	password := fmt.Sprintf("hive-%s-pass", job["name"])

	var env []string
	switch job["db_type"] {
	case "postgres":
		env = []string{
			"POSTGRES_DB=" + job["name"],
			"POSTGRES_USER=" + job["name"],
			"POSTGRES_PASSWORD=" + password,
		}
	case "mysql":
		env = []string{
			"MYSQL_DATABASE=" + job["name"],
			"MYSQL_USER=" + job["name"],
			"MYSQL_PASSWORD=" + password,
			"MYSQL_ROOT_PASSWORD=" + password + "-root",
		}
	case "redis":
		env = []string{}
	case "mongo":
		env = []string{
			"MONGO_INITDB_ROOT_USERNAME=" + job["name"],
			"MONGO_INITDB_ROOT_PASSWORD=" + password,
		}
	}

	replicas := uint64(1)
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: serviceName,
			Labels: map[string]string{
				"hive.managed":   "true",
				"hive.component": "database",
				"hive.db_type":   job["db_type"],
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: image,
				Env:   env,
			},
			Networks: []swarm.NetworkAttachmentConfig{
				{Target: "hive-net"},
			},
			Placement: &swarm.Placement{
				Constraints: []string{"node.role == manager"},
			},
		},
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{Replicas: &replicas},
		},
	}

	if err := sc.CreateService(ctx, spec); err != nil {
		p.log.Errorf("provision: %v", err)
		p.publishProgress(job["db_id"], "provisioning failed: "+err.Error())
		return
	}
	p.publishProgress(job["db_id"], "database provisioned successfully")
}

func (p *Pool) handleBackup(msg *nats.Msg) {
	var job map[string]string
	if err := json.Unmarshal(msg.Data, &job); err != nil {
		p.log.Errorf("backup: invalid job: %v", err)
		return
	}

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
		outputDir := filepath.Join(p.cfg.DataDir, "backups", vol.Name)
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
		outputDir := filepath.Join(p.cfg.DataDir, "backups", db.Name)

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

	fileInfo, _ := os.Stat(outputPath)
	size := int64(0)
	if fileInfo != nil {
		size = fileInfo.Size()
	}

	targetPath := outputPath
	if config.S3Bucket != "" {
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

		spec := swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   svc.Name,
				Labels: svcLabels,
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: svc.Image,
					Env:   env,
				},
				Networks: []swarm.NetworkAttachmentConfig{
					{Target: "hive-net"},
				},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		}

		if len(svc.Constraints) > 0 {
			spec.TaskTemplate.Placement = &swarm.Placement{
				Constraints: svc.Constraints,
			}
		}

		exists, _ := sc.ServiceExists(ctx, svc.Name)
		if exists {
			existing, err := sc.GetService(ctx, svc.Name)
			if err == nil && existing != nil {
				if err := sc.UpdateService(ctx, existing.ID, existing.Version, spec); err != nil {
				p.log.Warnf("stack deploy: update service %s: %v", svc.Name, err)
			}
			}
		} else {
			if err := sc.CreateService(ctx, spec); err != nil {
				p.log.Errorf("stack deploy: create service %s: %v", svc.Name, err)
			}
		}
	}

	if err := p.store.UpdateStack(ctx, &store.Stack{ID: stackID, Name: stackName, ComposeContent: st.ComposeContent, Status: "running"}); err != nil {
		p.log.Warnf("failed to update stack status: %v", err)
	}
	p.log.Infof("stack deployed: %s (%d services)", stackName, len(services))
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
	data, _ := json.Marshal(map[string]string{
		"app_id":  appID,
		"message": message,
	})
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
		localPath := filepath.Join(p.cfg.DataDir, "restores", filepath.Base(backupPath))
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

func (p *Pool) handleMaintenance(msg *nats.Msg) {
	var job map[string]string
	if err := json.Unmarshal(msg.Data, &job); err != nil {
		p.log.Errorf("maintenance: invalid job: %v", err)
		return
	}
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
