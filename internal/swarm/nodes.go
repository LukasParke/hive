package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/swarm"
)

func (c *Client) ListNodes(ctx context.Context) ([]swarm.Node, error) {
	nodes, err := c.docker.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("node list: %w", err)
	}
	return nodes, nil
}

func (c *Client) GetNode(ctx context.Context, nodeID string) (swarm.Node, error) {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, nodeID)
	if err != nil {
		return swarm.Node{}, fmt.Errorf("node inspect %s: %w", nodeID, err)
	}
	return node, nil
}

func (c *Client) GetSwarmJoinTokens(ctx context.Context) (worker string, manager string, err error) {
	sw, err := c.docker.SwarmInspect(ctx)
	if err != nil {
		return "", "", fmt.Errorf("swarm inspect: %w", err)
	}
	return sw.JoinTokens.Worker, sw.JoinTokens.Manager, nil
}

func (c *Client) NodeCount(ctx context.Context) (int, error) {
	nodes, err := c.docker.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		return 0, err
	}
	return len(nodes), nil
}

func (c *Client) UpdateNodeLabels(ctx context.Context, nodeID string, labels map[string]string) error {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("inspect node %s: %w", nodeID, err)
	}

	if node.Spec.Labels == nil {
		node.Spec.Labels = make(map[string]string)
	}
	for k, v := range labels {
		if v == "" {
			delete(node.Spec.Labels, k)
		} else {
			node.Spec.Labels[k] = v
		}
	}

	return c.docker.NodeUpdate(ctx, nodeID, node.Version, node.Spec)
}
