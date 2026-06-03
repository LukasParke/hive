package swarm

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	dockerclient "github.com/docker/docker/client"
)

type Client struct {
	raw *dockerclient.Client
}

func New(host string) (*Client, error) {
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(host),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}
	return &Client{raw: cli}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.raw.Ping(ctx)
	return err
}

func (c *Client) ListServices(ctx context.Context) ([]swarm.Service, error) {
	return c.raw.ServiceList(ctx, types.ServiceListOptions{})
}

func (c *Client) CreateService(ctx context.Context, spec swarm.ServiceSpec) (string, error) {
	resp, err := c.raw.ServiceCreate(ctx, spec, types.ServiceCreateOptions{})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) UpdateService(ctx context.Context, id string, version uint64, spec swarm.ServiceSpec) error {
	_, err := c.raw.ServiceUpdate(ctx, id, swarm.Version{Index: version}, spec, types.ServiceUpdateOptions{})
	return err
}

func (c *Client) RemoveService(ctx context.Context, id string) error {
	return c.raw.ServiceRemove(ctx, id)
}

func (c *Client) ListNodes(ctx context.Context) ([]swarm.Node, error) {
	return c.raw.NodeList(ctx, types.NodeListOptions{})
}

func (c *Client) CreateNetwork(ctx context.Context, name string) (string, error) {
	resp, err := c.raw.NetworkCreate(ctx, name, network.CreateOptions{Driver: "overlay"})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	return c.raw.NetworkList(ctx, network.ListOptions{})
}

func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.raw.NetworkRemove(ctx, id)
}

func (c *Client) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error) {
	resp, err := c.raw.SecretCreate(ctx, spec)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	return c.raw.SecretList(ctx, types.SecretListOptions{})
}

func (c *Client) RemoveSecret(ctx context.Context, id string) error {
	return c.raw.SecretRemove(ctx, id)
}

func (c *Client) CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error) {
	resp, err := c.raw.ConfigCreate(ctx, spec)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) ListConfigs(ctx context.Context) ([]swarm.Config, error) {
	return c.raw.ConfigList(ctx, types.ConfigListOptions{})
}

func (c *Client) RemoveConfig(ctx context.Context, id string) error {
	return c.raw.ConfigRemove(ctx, id)
}

func (c *Client) PullImage(ctx context.Context, ref string) error {
	r, err := c.raw.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	return r.Close()
}

func (c *Client) ContainerLogs(ctx context.Context, id string) error {
	r, err := c.raw.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: "200"})
	if err != nil {
		return err
	}
	return r.Close()
}

func (c *Client) ListTasks(ctx context.Context, serviceID string) ([]swarm.Task, error) {
	f := filters.NewArgs()
	f.Add("service", serviceID)
	return c.raw.TaskList(ctx, types.TaskListOptions{Filters: f})
}

// ListAllTasks returns all tasks in the swarm (not filtered by service).
func (c *Client) ListAllTasks(ctx context.Context) ([]swarm.Task, error) {
	return c.raw.TaskList(ctx, types.TaskListOptions{})
}

// GetNode returns a single node by ID.
func (c *Client) GetNode(ctx context.Context, nodeID string) (swarm.Node, error) {
	node, _, err := c.raw.NodeInspectWithRaw(ctx, nodeID)
	return node, err
}
