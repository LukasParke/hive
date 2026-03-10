package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/network"
)

func (c *Client) EnsureNetwork(ctx context.Context, name string, opts network.CreateOptions) error {
	nets, err := c.docker.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("network list: %w", err)
	}
	for _, n := range nets {
		if n.Name == name {
			c.log.Infof("network %s already exists", name)
			return nil
		}
	}

	_, err = c.docker.NetworkCreate(ctx, name, opts)
	if err != nil {
		return fmt.Errorf("network create %s: %w", name, err)
	}
	c.log.Infof("created network: %s", name)
	return nil
}

func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	nets, err := c.docker.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("network list: %w", err)
	}
	for _, n := range nets {
		if n.Name == name {
			if err := c.docker.NetworkRemove(ctx, n.ID); err != nil {
				return fmt.Errorf("network remove %s: %w", name, err)
			}
			c.log.Infof("removed network: %s", name)
			return nil
		}
	}
	return nil
}
