package swarm

import (
	"context"
	"fmt"
	"strings"

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
	_, err := c.raw.Ping(ctx, dockerclient.PingOptions{})
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
	_, err := c.raw.ServiceRemove(ctx, id, dockerclient.ServiceRemoveOptions{})
	return err
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
	_, err := c.raw.NetworkRemove(ctx, id, dockerclient.NetworkRemoveOptions{})
	return err
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
	_, err := c.raw.SecretRemove(ctx, id, dockerclient.SecretRemoveOptions{})
	return err
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
	_, err := c.raw.ConfigRemove(ctx, id, dockerclient.ConfigRemoveOptions{})
	return err
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

// ServiceTaskIPOnNetwork returns the overlay IP of the running task of the
// service identified by the given service label (key=value) that is scheduled
// on nodeID, on the network whose name has the given suffix. It is used to
// reach a specific node's agent over the encrypted hive_internal overlay
// instead of the node's host IP.
func (c *Client) ServiceTaskIPOnNetwork(ctx context.Context, labelKey, labelValue, nodeID, networkNameSuffix string) (string, error) {
	services, err := c.ListServices(ctx)
	if err != nil {
		return "", err
	}
	var serviceID string
	for _, s := range services {
		if s.Spec.Labels[labelKey] == labelValue {
			serviceID = s.ID
			break
		}
	}
	if serviceID == "" {
		return "", fmt.Errorf("no service found with label %s=%s", labelKey, labelValue)
	}

	tasks, err := c.ListTasks(ctx, serviceID)
	if err != nil {
		return "", err
	}
	for _, t := range tasks {
		if t.NodeID != nodeID || t.DesiredState != swarm.TaskStateRunning {
			continue
		}
		for _, na := range t.NetworksAttachments {
			if !strings.HasSuffix(na.Network.Spec.Name, networkNameSuffix) {
				continue
			}
			for _, addr := range na.Addresses {
				return addr.Addr().String(), nil
			}
		}
	}
	return "", fmt.Errorf("no running task for %s=%s on node %s attached to %s", labelKey, labelValue, nodeID, networkNameSuffix)
}
