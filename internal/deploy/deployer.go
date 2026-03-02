package deploy

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/lholliger/hive/internal/proxy"
	hiveswarm "github.com/lholliger/hive/internal/swarm"

	"go.uber.org/zap"
)

type Deployer struct {
	swarm hiveswarm.ServiceAPI
	log   *zap.SugaredLogger
}

func NewDeployer(sc hiveswarm.ServiceAPI, log *zap.SugaredLogger) *Deployer {
	return &Deployer{swarm: sc, log: log}
}

type PlacementStrategy string

const (
	PlacementSpread PlacementStrategy = "spread"
	PlacementBinpack PlacementStrategy = "binpack"
	PlacementManager PlacementStrategy = "manager"
)

type DeployRequest struct {
	Name            string
	Image           string
	Domain          string
	Port            int
	Replicas        int
	Env             map[string]string
	Labels          map[string]string
	Placement       PlacementStrategy
	Constraints     []string
	HealthCheck     *HealthCheckConfig
	RollingUpdate   *RollingUpdateConfig
	Secrets         []SecretMount
	Volumes         []VolumeMount
}

type SecretMount struct {
	DockerSecretID string
	SecretName     string
	Target         string
	UID            string
	GID            string
	Mode           os.FileMode
}

type VolumeMount struct {
	VolumeName    string
	ContainerPath string
	ReadOnly      bool
}

type HealthCheckConfig struct {
	Test     []string
	Interval time.Duration
	Timeout  time.Duration
	Retries  int
}

type RollingUpdateConfig struct {
	Parallelism   uint64
	Delay         time.Duration
	Order         string // "start-first" or "stop-first"
}

func (d *Deployer) Deploy(ctx context.Context, req DeployRequest) error {
	serviceName := "hive-app-" + req.Name

	if req.Port == 0 {
		req.Port = 3000
	}
	if req.Replicas == 0 {
		req.Replicas = 1
	}

	labels := map[string]string{
		"hive.managed": "true",
		"hive.app":     req.Name,
	}
	if req.Domain != "" {
		traefikLabels := proxy.ServiceLabels(serviceName, req.Domain, req.Port)
		labels = proxy.MergeLabels(labels, traefikLabels)
	}
	labels = proxy.MergeLabels(labels, req.Labels)

	var env []string
	for k, v := range req.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	replicas := uint64(req.Replicas)
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   serviceName,
			Labels: labels,
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: req.Image,
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

	// Placement constraints
	placement := &swarm.Placement{}
	if len(req.Constraints) > 0 {
		placement.Constraints = req.Constraints
	} else {
		switch req.Placement {
		case PlacementManager:
			placement.Constraints = []string{"node.role == manager"}
		case PlacementSpread:
			placement.Preferences = []swarm.PlacementPreference{
				{Spread: &swarm.SpreadOver{SpreadDescriptor: "node.id"}},
			}
		}
	}
	spec.TaskTemplate.Placement = placement

	// Health check
	if req.HealthCheck != nil {
		spec.TaskTemplate.ContainerSpec.Healthcheck = &container.HealthConfig{
			Test:     req.HealthCheck.Test,
			Interval: req.HealthCheck.Interval,
			Timeout:  req.HealthCheck.Timeout,
			Retries:  req.HealthCheck.Retries,
		}
	}

	// Secret references
	if len(req.Secrets) > 0 {
		var secretRefs []*swarm.SecretReference
		for _, s := range req.Secrets {
			target := s.Target
			if target == "" {
				target = s.SecretName
			}
			mode := s.Mode
			if mode == 0 {
				mode = 0444
			}
			secretRefs = append(secretRefs, &swarm.SecretReference{
				SecretID:   s.DockerSecretID,
				SecretName: s.SecretName,
				File: &swarm.SecretReferenceFileTarget{
					Name: target,
					UID:  s.UID,
					GID:  s.GID,
					Mode: mode,
				},
			})
		}
		spec.TaskTemplate.ContainerSpec.Secrets = secretRefs
	}

	// Volume mounts
	if len(req.Volumes) > 0 {
		var mounts []mount.Mount
		for _, v := range req.Volumes {
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeVolume,
				Source:   v.VolumeName,
				Target:   v.ContainerPath,
				ReadOnly: v.ReadOnly,
			})
		}
		spec.TaskTemplate.ContainerSpec.Mounts = append(spec.TaskTemplate.ContainerSpec.Mounts, mounts...)
	}

	// Rolling update
	if req.RollingUpdate != nil {
		order := req.RollingUpdate.Order
		if order == "" {
			order = "start-first"
		}
		spec.UpdateConfig = &swarm.UpdateConfig{
			Parallelism: req.RollingUpdate.Parallelism,
			Delay:       req.RollingUpdate.Delay,
			Order:       order,
		}
	} else {
		spec.UpdateConfig = &swarm.UpdateConfig{
			Parallelism: 1,
			Delay:       10 * time.Second,
			Order:       "start-first",
		}
	}

	// Rollback config
	spec.RollbackConfig = &swarm.UpdateConfig{
		Parallelism: 1,
		Delay:       10 * time.Second,
		Order:       "stop-first",
	}

	exists, err := d.swarm.ServiceExists(ctx, serviceName)
	if err != nil {
		return err
	}

	if exists {
		svc, err := d.swarm.GetService(ctx, serviceName)
		if err != nil {
			return err
		}
		if svc != nil {
			return d.swarm.UpdateService(ctx, svc.ID, svc.Version, spec)
		}
	}

	return d.swarm.CreateService(ctx, spec)
}

func (d *Deployer) Remove(ctx context.Context, name string) error {
	serviceName := "hive-app-" + name
	svc, err := d.swarm.GetService(ctx, serviceName)
	if err != nil {
		return err
	}
	if svc == nil {
		return nil
	}
	return d.swarm.RemoveService(ctx, svc.ID)
}
