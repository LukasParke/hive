package bootstrap

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

const (
	registryServiceName = "hive-registry"
	registryImage       = "registry:2"
	registryPort        = 5000
)

func (b *Bootstrapper) ensureRegistry(ctx context.Context) error {
	exists, err := b.swarm.ServiceExists(ctx, registryServiceName)
	if err != nil {
		return err
	}
	if exists {
		b.log.Info("registry service already running")
		return nil
	}

	b.log.Info("deploying container registry for multi-node image distribution")

	registryDomain := b.cfg.RegistryDomain
	if registryDomain == "" {
		registryDomain = "registry.hive.local"
	}

	replicas := uint64(1)
	labels := map[string]string{
		"hive.managed":   "true",
		"hive.component": "registry",
	}

	if registryDomain != "registry.hive.local" {
		labels["traefik.enable"] = "true"
		labels["traefik.http.routers.hive-registry.rule"] = fmt.Sprintf("Host(`%s`)", registryDomain)
		labels["traefik.http.routers.hive-registry.entrypoints"] = "websecure"
		labels["traefik.http.routers.hive-registry.tls.certresolver"] = "letsencrypt"
		labels["traefik.http.services.hive-registry.loadbalancer.server.port"] = "5000"
	}

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   registryServiceName,
			Labels: labels,
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: registryImage,
				Env: []string{
					"REGISTRY_STORAGE_DELETE_ENABLED=true",
				},
				Mounts: []mount.Mount{
					{
						Type:   mount.TypeVolume,
						Source: "hive-registry-data",
						Target: "/var/lib/registry",
					},
				},
			},
			Networks: []swarm.NetworkAttachmentConfig{
				{Target: hivenetName},
			},
			Placement: &swarm.Placement{
				Constraints: []string{"node.role == manager"},
			},
		},
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{Replicas: &replicas},
		},
		EndpointSpec: &swarm.EndpointSpec{
			Ports: []swarm.PortConfig{
				{
					Protocol:      swarm.PortConfigProtocolTCP,
					TargetPort:    registryPort,
					PublishedPort: registryPort,
					PublishMode:   swarm.PortConfigPublishModeIngress,
				},
			},
		},
	}

	return b.swarm.CreateService(ctx, spec)
}
