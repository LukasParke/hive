package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

const stackLabelKey = "com.docker.stack.namespace"

func (c *Client) DeployStack(ctx context.Context, stackName string, services []swarm.ServiceSpec, networks []network.CreateOptions) error {
	for _, netOpts := range networks {
		netName := stackName + "-net"
		if err := c.EnsureNetwork(ctx, netName, netOpts); err != nil {
			return fmt.Errorf("ensure network %s: %w", netName, err)
		}
	}

	for _, spec := range services {
		if spec.Labels == nil {
			spec.Labels = make(map[string]string)
		}
		spec.Labels[stackLabelKey] = stackName

		exists, err := c.ServiceExists(ctx, spec.Name)
		if err != nil {
			return err
		}
		if exists {
			c.log.Infof("service %s already exists in stack %s, updating", spec.Name, stackName)
			svc, err := c.GetService(ctx, spec.Name)
			if err != nil {
				return err
			}
			if svc != nil {
				if err := c.UpdateService(ctx, svc.ID, svc.Version, spec); err != nil {
					return err
				}
			}
		} else {
			if err := c.CreateService(ctx, spec); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) RemoveStack(ctx context.Context, stackName string) error {
	services, err := c.docker.ServiceList(ctx, swarm.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", stackLabelKey+"="+stackName)),
	})
	if err != nil {
		return fmt.Errorf("list stack services: %w", err)
	}
	for _, svc := range services {
		if err := c.docker.ServiceRemove(ctx, svc.ID); err != nil {
			c.log.Warnf("failed to remove service %s: %v", svc.Spec.Name, err)
		}
	}
	return nil
}
