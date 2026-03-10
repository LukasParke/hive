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

func (c *Client) SetNodeAvailability(ctx context.Context, nodeID string, availability swarm.NodeAvailability) error {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("inspect node %s: %w", nodeID, err)
	}
	node.Spec.Availability = availability
	return c.docker.NodeUpdate(ctx, nodeID, node.Version, node.Spec)
}

func (c *Client) SetNodeRole(ctx context.Context, nodeID string, role swarm.NodeRole) error {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("inspect node %s: %w", nodeID, err)
	}
	node.Spec.Role = role
	return c.docker.NodeUpdate(ctx, nodeID, node.Version, node.Spec)
}

func (c *Client) RemoveNode(ctx context.Context, nodeID string, force bool) error {
	return c.docker.NodeRemove(ctx, nodeID, swarm.NodeRemoveOptions{Force: force})
}

// ListReadyNodes returns all nodes that are both Ready (status) and Active (availability).
func (c *Client) ListReadyNodes(ctx context.Context) ([]swarm.Node, error) {
	nodes, err := c.docker.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("node list: %w", err)
	}
	var ready []swarm.Node
	for _, n := range nodes {
		if n.Status.State == swarm.NodeStateReady && n.Spec.Availability == swarm.NodeAvailabilityActive {
			ready = append(ready, n)
		}
	}
	return ready, nil
}

// ValidatePlacement checks that at least one ready+active node satisfies the
// given placement constraints. Returns an error with an actionable message
// if no eligible node exists, so callers can fail fast instead of pending forever.
func (c *Client) ValidatePlacement(ctx context.Context, constraints []string) error {
	nodes, err := c.ListReadyNodes(ctx)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return fmt.Errorf("no ready+active nodes in swarm")
	}
	if len(constraints) == 0 {
		return nil
	}

	for _, node := range nodes {
		if nodeMatchesConstraints(node, constraints) {
			return nil
		}
	}
	return fmt.Errorf("no ready node satisfies constraints %v (ready nodes: %d)", constraints, len(nodes))
}

func nodeMatchesConstraints(node swarm.Node, constraints []string) bool {
	for _, c := range constraints {
		if !nodeMatchesConstraint(node, c) {
			return false
		}
	}
	return true
}

func nodeMatchesConstraint(node swarm.Node, constraint string) bool {
	parts := splitConstraint(constraint)
	if len(parts) != 3 {
		return true
	}
	key, op, val := parts[0], parts[1], parts[2]

	var actual string
	switch key {
	case "node.role":
		actual = string(node.Spec.Role)
	case "node.id":
		actual = node.ID
	case "node.hostname":
		actual = node.Description.Hostname
	default:
		if len(key) > 12 && key[:12] == "node.labels." {
			labelKey := key[12:]
			if node.Spec.Labels != nil {
				actual = node.Spec.Labels[labelKey]
			}
		} else {
			return true
		}
	}

	switch op {
	case "==":
		return actual == val
	case "!=":
		return actual != val
	default:
		return true
	}
}

func splitConstraint(c string) []string {
	for _, op := range []string{"==", "!="} {
		idx := 0
		for i := 0; i <= len(c)-len(op); i++ {
			if c[i:i+len(op)] == op {
				idx = i
				break
			}
		}
		if idx > 0 {
			key := c[:idx]
			val := c[idx+len(op):]
			for len(key) > 0 && key[len(key)-1] == ' ' {
				key = key[:len(key)-1]
			}
			for len(val) > 0 && val[0] == ' ' {
				val = val[1:]
			}
			return []string{key, op, val}
		}
	}
	return nil
}
