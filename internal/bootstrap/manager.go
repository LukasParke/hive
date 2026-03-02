package bootstrap

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

const managerServiceName = "hive-manager"

func (b *Bootstrapper) ensureManager(ctx context.Context) error {
	exists, err := b.swarm.ServiceExists(ctx, managerServiceName)
	if err != nil {
		return err
	}
	if exists {
		b.log.Info("hive-manager service already running")
		return nil
	}

	b.log.Info("deploying hive-manager as a Swarm service")

	replicas := uint64(1)
	apiPort := uint32(b.cfg.APIPort)
	natsPort := uint32(b.cfg.NATSPort)

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: managerServiceName,
			Labels: map[string]string{
				"hive.managed":   "true",
				"hive.component": "manager",
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:   b.cfg.HiveImage,
				Command: []string{"hive"},
				Env: []string{
					"HIVE_MANAGED=true",
					"HIVE_ROLE=manager",
					"HIVE_IMAGE=" + b.cfg.HiveImage,
					fmt.Sprintf("HIVE_API_PORT=%d", b.cfg.APIPort),
					fmt.Sprintf("HIVE_NATS_PORT=%d", b.cfg.NATSPort),
					fmt.Sprintf("HIVE_AGENT_INTERVAL=%d", b.cfg.AgentInterval),
				},
				Mounts: []mount.Mount{
					{
						Type:   mount.TypeBind,
						Source: "/var/run/docker.sock",
						Target: "/var/run/docker.sock",
					},
					{
						Type:   mount.TypeVolume,
						Source: "hive-data",
						Target: "/data",
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
					PublishedPort: apiPort,
					TargetPort:    apiPort,
					PublishMode:   swarm.PortConfigPublishModeIngress,
				},
				{
					Protocol:      swarm.PortConfigProtocolTCP,
					PublishedPort: natsPort,
					TargetPort:    natsPort,
					PublishMode:   swarm.PortConfigPublishModeIngress,
				},
			},
		},
	}

	return b.swarm.CreateService(ctx, spec)
}
