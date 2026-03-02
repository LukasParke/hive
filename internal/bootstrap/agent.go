package bootstrap

import (
	"context"
	"fmt"
	"os"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

const agentServiceName = "hive-agent"

func (b *Bootstrapper) ensureAgent(ctx context.Context) error {
	exists, err := b.swarm.ServiceExists(ctx, agentServiceName)
	if err != nil {
		return err
	}
	if exists {
		b.log.Info("agent service already running")
		return nil
	}

	b.log.Info("deploying hive monitoring agent as global service")

	hiveImage := os.Getenv("HIVE_IMAGE")
	if hiveImage == "" {
		hiveImage = "ghcr.io/lholliger/hive:latest"
	}

	natsURL := fmt.Sprintf("nats://hive-nats:%d", b.cfg.NATSPort)
	agentInterval := fmt.Sprintf("%d", b.cfg.AgentInterval)
	if agentInterval == "0" {
		agentInterval = "10"
	}

	memLimit := int64(64 * 1024 * 1024) // 64MB
	nanoCPU := int64(100000000)          // 0.1 CPU

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: agentServiceName,
			Labels: map[string]string{
				"hive.managed":   "true",
				"hive.component": "agent",
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:   hiveImage,
				Command: []string{"hive", "agent"},
				Env: []string{
					"HIVE_NATS_URL=" + natsURL,
					"HIVE_AGENT_INTERVAL=" + agentInterval,
					"HIVE_ROLE=agent",
				},
				Mounts: []mount.Mount{
					{
						Type:     mount.TypeBind,
						Source:   "/proc",
						Target:   "/host/proc",
						ReadOnly: true,
					},
					{
						Type:     mount.TypeBind,
						Source:   "/sys",
						Target:   "/host/sys",
						ReadOnly: true,
					},
					{
						Type:     mount.TypeBind,
						Source:   "/etc/os-release",
						Target:   "/host/etc/os-release",
						ReadOnly: true,
					},
					{
						Type:   mount.TypeBind,
						Source: "/var/run/docker.sock",
						Target: "/var/run/docker.sock",
					},
				},
			},
			Networks: []swarm.NetworkAttachmentConfig{
				{Target: hivenetName},
			},
			Resources: &swarm.ResourceRequirements{
				Limits: &swarm.Limit{
					MemoryBytes: memLimit,
					NanoCPUs:    nanoCPU,
				},
			},
		},
		Mode: swarm.ServiceMode{
			Global: &swarm.GlobalService{},
		},
	}

	return b.swarm.CreateService(ctx, spec)
}
