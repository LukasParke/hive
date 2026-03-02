package tunnel

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/swarm"

	hiveswarm "github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/pkg/config"
	"go.uber.org/zap"
)

const (
	cloudflaredService = "hive-cloudflared"
	cloudflaredImage   = "cloudflare/cloudflared:latest"
)

type CloudflaredManager struct {
	swarm *hiveswarm.Client
	cfg   *config.Config
	log   *zap.SugaredLogger
}

func NewCloudflaredManager(sc *hiveswarm.Client, cfg *config.Config, log *zap.SugaredLogger) *CloudflaredManager {
	return &CloudflaredManager{swarm: sc, cfg: cfg, log: log}
}

func (m *CloudflaredManager) EnsureTunnel(ctx context.Context) error {
	if m.cfg.CFTunnelToken == "" {
		m.log.Debug("no Cloudflare tunnel token configured, skipping")
		return nil
	}

	exists, err := m.swarm.ServiceExists(ctx, cloudflaredService)
	if err != nil {
		return fmt.Errorf("check cloudflared: %w", err)
	}
	if exists {
		m.log.Info("cloudflared tunnel service already running")
		return nil
	}

	m.log.Info("deploying cloudflared tunnel service")

	replicas := uint64(1)
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: cloudflaredService,
			Labels: map[string]string{
				"hive.managed":   "true",
				"hive.component": "cloudflared",
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: cloudflaredImage,
				Args: []string{
					"tunnel",
					"--no-autoupdate",
					"run",
					"--token",
					m.cfg.CFTunnelToken,
				},
			},
			Networks: []swarm.NetworkAttachmentConfig{
				{Target: "hive-net"},
			},
		},
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{Replicas: &replicas},
		},
	}

	return m.swarm.CreateService(ctx, spec)
}

func (m *CloudflaredManager) RemoveTunnel(ctx context.Context) error {
	svc, err := m.swarm.GetService(ctx, cloudflaredService)
	if err != nil || svc == nil {
		return nil
	}
	return m.swarm.RemoveService(ctx, svc.ID)
}

func (m *CloudflaredManager) IsRunning(ctx context.Context) bool {
	exists, err := m.swarm.ServiceExists(ctx, cloudflaredService)
	return err == nil && exists
}
