package swarm

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
)

func (c *Client) ServiceExists(ctx context.Context, name string) (bool, error) {
	services, err := c.docker.ServiceList(ctx, swarm.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return false, fmt.Errorf("service list: %w", err)
	}
	for _, s := range services {
		if s.Spec.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) CreateService(ctx context.Context, spec swarm.ServiceSpec) error {
	_, err := c.docker.ServiceCreate(ctx, spec, swarm.ServiceCreateOptions{})
	if err != nil {
		return fmt.Errorf("service create %s: %w", spec.Name, err)
	}
	c.log.Infof("created service: %s", spec.Name)
	return nil
}

func (c *Client) UpdateService(ctx context.Context, serviceID string, version swarm.Version, spec swarm.ServiceSpec) error {
	_, err := c.docker.ServiceUpdate(ctx, serviceID, version, spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return fmt.Errorf("service update %s: %w", spec.Name, err)
	}
	return nil
}

func (c *Client) RemoveService(ctx context.Context, serviceID string) error {
	return c.docker.ServiceRemove(ctx, serviceID)
}

func (c *Client) GetService(ctx context.Context, name string) (*swarm.Service, error) {
	services, err := c.docker.ServiceList(ctx, swarm.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return nil, fmt.Errorf("service list: %w", err)
	}
	for _, s := range services {
		if s.Spec.Name == name {
			return &s, nil
		}
	}
	return nil, nil
}

func (c *Client) ListServices(ctx context.Context) ([]swarm.Service, error) {
	return c.docker.ServiceList(ctx, swarm.ServiceListOptions{})
}

func (c *Client) ServiceLogs(ctx context.Context, serviceID string, tail string, follow bool) (io.ReadCloser, error) {
	if tail == "" {
		tail = "200"
	}
	return c.docker.ServiceLogs(ctx, serviceID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Timestamps: true,
	})
}

func (c *Client) ScaleService(ctx context.Context, serviceID string, replicas uint64) error {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, serviceID, swarm.ServiceInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect service: %w", err)
	}
	svc.Spec.Mode.Replicated.Replicas = &replicas
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return fmt.Errorf("scale service: %w", err)
	}
	return nil
}

func (c *Client) RollbackService(ctx context.Context, serviceID string) error {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, serviceID, swarm.ServiceInspectOptions{})
	if err != nil {
		return fmt.Errorf("inspect service: %w", err)
	}
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{
		Rollback: "previous",
	})
	if err != nil {
		return fmt.Errorf("rollback service: %w", err)
	}
	return nil
}
