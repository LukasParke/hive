package network

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	dockernet "github.com/moby/moby/api/types/network"

	"github.com/luke/hive/control-plane/internal/swarm"
)

// Store is the slice of the swarm client the Manager needs.
// *swarm.Client satisfies it; tests inject fakes.
type Store interface {
	ListNetworks(ctx context.Context) ([]dockernet.Summary, error)
	CreateNetwork(ctx context.Context, name string) (string, error)
}

// Compile-time check that the concrete swarm client satisfies Store.
var _ Store = (*swarm.Client)(nil)

// Manager creates and reuses swarm overlay networks.
type Manager struct {
	client Store
}

// New returns a Manager backed by the given swarm client.
func New(client Store) *Manager {
	return &Manager{client: client}
}

var unsafe = regexp.MustCompile(`[^a-z0-9_-]+`)

// EnsureOverlay returns the ID of the named overlay network, creating it if
// it does not exist yet.
func (m *Manager) EnsureOverlay(ctx context.Context, name string) (string, error) {
	networks, err := m.client.ListNetworks(ctx)
	if err != nil {
		return "", fmt.Errorf("list networks: %w", err)
	}
	for _, n := range networks {
		if n.Name == name {
			return n.ID, nil
		}
	}
	id, err := m.client.CreateNetwork(ctx, name)
	if err != nil {
		return "", fmt.Errorf("create network %s: %w", name, err)
	}
	return id, nil
}

// ProjectNetworkName returns the shared overlay name for a project slug,
// normalizing the slug to [a-z0-9_-].
func ProjectNetworkName(projectSlug string) string {
	slug := unsafe.ReplaceAllString(strings.ToLower(projectSlug), "-")
	return fmt.Sprintf("hive_project_%s", slug)
}

// EnsureProjectNetwork ensures the shared project overlay hive_project_{slug}
// exists and returns its ID.
func (m *Manager) EnsureProjectNetwork(ctx context.Context, projectSlug string) (string, error) {
	return m.EnsureOverlay(ctx, ProjectNetworkName(projectSlug))
}
