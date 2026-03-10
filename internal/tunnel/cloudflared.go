package tunnel

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

	replicas := uint64(2)
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
			Placement: &swarm.Placement{
				MaxReplicas: 1,
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

// TunnelID extracts the tunnel ID from the base64-encoded tunnel token.
// The token decodes to JSON: {"a":"<account_tag>","t":"<tunnel_id>","s":"<secret>"}
func (m *CloudflaredManager) TunnelID() string {
	return ParseTunnelID(m.cfg.CFTunnelToken)
}

// CNAMETarget returns the CNAME target for the tunnel (e.g., "<tunnel-id>.cfargotunnel.com").
func (m *CloudflaredManager) CNAMETarget() string {
	tid := m.TunnelID()
	if tid == "" {
		return ""
	}
	return tid + ".cfargotunnel.com"
}

// ParseTunnelID extracts the tunnel ID from a raw tunnel token string.
func ParseTunnelID(token string) string {
	if token == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		b, err = base64.RawStdEncoding.DecodeString(token)
		if err != nil {
			return ""
		}
	}
	var parsed struct {
		TunnelID string `json:"t"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return ""
	}
	return parsed.TunnelID
}
