package network

import (
	"context"
	"fmt"
	"regexp"

	"github.com/luke/hive/control-plane/internal/swarm"
)

type Manager struct {
	client *swarm.Client
}

func New(client *swarm.Client) *Manager {
	return &Manager{client: client}
}

var unsafe = regexp.MustCompile(`[^a-z0-9_-]+`)

func (m *Manager) EnsureProjectNetwork(ctx context.Context, projectSlug string) (string, error) {
	slug := unsafe.ReplaceAllString(projectSlug, "-")
	name := fmt.Sprintf("dokploy_project_%s", slug)
	return m.client.CreateNetwork(ctx, name)
}
