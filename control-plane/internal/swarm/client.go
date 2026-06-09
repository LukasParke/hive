package swarm

import (
	"context"

	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"
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
	result, err := c.raw.ServiceList(ctx, dockerclient.ServiceListOptions{})
	return result.Items, err
}

func (c *Client) CreateService(ctx context.Context, spec swarm.ServiceSpec) (string, error) {
	resp, err := c.raw.ServiceCreate(ctx, dockerclient.ServiceCreateOptions{Spec: spec})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) UpdateService(ctx context.Context, id string, version uint64, spec swarm.ServiceSpec) error {
	_, err := c.raw.ServiceUpdate(ctx, id, dockerclient.ServiceUpdateOptions{
		Version: swarm.Version{Index: version},
		Spec:    spec,
	})
	return err
}

func (c *Client) RemoveService(ctx context.Context, id string) error {
	return c.raw.ServiceRemove(ctx, id)
}

func (c *Client) ListNodes(ctx context.Context) ([]swarm.Node, error) {
	result, err := c.raw.NodeList(ctx, dockerclient.NodeListOptions{})
	return result.Items, err
}

func (c *Client) CreateNetwork(ctx context.Context, name string) (string, error) {
	resp, err := c.raw.NetworkCreate(ctx, name, dockerclient.NetworkCreateOptions{Driver: "overlay"})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	result, err := c.raw.NetworkList(ctx, dockerclient.NetworkListOptions{})
	return result.Items, err
}

func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.raw.NetworkRemove(ctx, id)
}

func (c *Client) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error) {
	resp, err := c.raw.SecretCreate(ctx, dockerclient.SecretCreateOptions{Spec: spec})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	result, err := c.raw.SecretList(ctx, dockerclient.SecretListOptions{})
	return result.Items, err
}

func (c *Client) RemoveSecret(ctx context.Context, id string) error {
	return c.raw.SecretRemove(ctx, id)
}

func (c *Client) CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error) {
	resp, err := c.raw.ConfigCreate(ctx, dockerclient.ConfigCreateOptions{Spec: spec})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

func (c *Client) ListConfigs(ctx context.Context) ([]swarm.Config, error) {
	result, err := c.raw.ConfigList(ctx, dockerclient.ConfigListOptions{})
	return result.Items, err
}

func (c *Client) RemoveConfig(ctx context.Context, id string) error {
	return c.raw.ConfigRemove(ctx, id)
}

func (c *Client) PullImage(ctx context.Context, ref string) error {
	r, err := c.raw.ImagePull(ctx, ref, dockerclient.ImagePullOptions{})
	if err != nil {
		return err
	}
	return r.Close()
}

func (c *Client) ContainerLogs(ctx context.Context, id string) error {
	r, err := c.raw.ContainerLogs(ctx, id, dockerclient.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: "200"})
	if err != nil {
		return err
	}
	return r.Close()
}

func (c *Client) ListTasks(ctx context.Context, serviceID string) ([]swarm.Task, error) {
	f := make(dockerclient.Filters).Add("service", serviceID)
	result, err := c.raw.TaskList(ctx, dockerclient.TaskListOptions{Filters: f})
	return result.Items, err
}

// ListAllTasks returns all tasks in the swarm (not filtered by service).
func (c *Client) ListAllTasks(ctx context.Context) ([]swarm.Task, error) {
	result, err := c.raw.TaskList(ctx, dockerclient.TaskListOptions{})
	return result.Items, err
}

// GetNode returns a single node by ID.
func (c *Client) GetNode(ctx context.Context, nodeID string) (swarm.Node, error) {
	result, err := c.raw.NodeInspect(ctx, nodeID, dockerclient.NodeInspectOptions{})
	return result.Node, err
}
