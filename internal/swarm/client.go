package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"

	"go.uber.org/zap"
)

type Client struct {
	docker *client.Client
	log    *zap.SugaredLogger
}

func NewClient(log *zap.SugaredLogger) (*Client, error) {
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client init: %w", err)
	}
	if log == nil {
		l, _ := zap.NewNop().Sugar(), error(nil)
		log = l
	}
	return &Client{docker: docker, log: log}, nil
}

func (c *Client) Docker() *client.Client {
	return c.docker
}

func (c *Client) EnsureSwarm(ctx context.Context) error {
	info, err := c.docker.Info(ctx)
	if err != nil {
		return fmt.Errorf("docker info: %w", err)
	}

	if info.Swarm.LocalNodeState == swarm.LocalNodeStateActive {
		c.log.Info("docker swarm already active")
		return nil
	}

	c.log.Info("initializing docker swarm")
	_, err = c.docker.SwarmInit(ctx, swarm.InitRequest{
		ListenAddr: "0.0.0.0:2377",
	})
	if err != nil {
		return fmt.Errorf("swarm init: %w", err)
	}

	c.log.Info("docker swarm initialized")
	return nil
}

func (c *Client) IsMultiNode(ctx context.Context) (bool, error) {
	nodes, err := c.docker.NodeList(ctx, swarm.NodeListOptions{})
	if err != nil {
		return false, fmt.Errorf("node list: %w", err)
	}
	return len(nodes) > 1, nil
}

func (c *Client) Close() error {
	return c.docker.Close()
}
