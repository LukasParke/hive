package swarm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"
)

// APIClient is the slice of the moby SDK client used by Client. It exists so
// tests can stub the daemon interaction; *dockerclient.Client satisfies it
// structurally via dockerAPI below — except for ImagePull, whose SDK result
// type embeds internal stream types no external package can construct, so
// that one method is narrowed to the io.ReadCloser surface PullImage consumes.
type APIClient interface {
	Ping(ctx context.Context, opts dockerclient.PingOptions) (dockerclient.PingResult, error)

	ServiceList(ctx context.Context, opts dockerclient.ServiceListOptions) (dockerclient.ServiceListResult, error)
	ServiceCreate(ctx context.Context, opts dockerclient.ServiceCreateOptions) (dockerclient.ServiceCreateResult, error)
	ServiceUpdate(ctx context.Context, serviceID string, opts dockerclient.ServiceUpdateOptions) (dockerclient.ServiceUpdateResult, error)
	ServiceRemove(ctx context.Context, serviceID string, opts dockerclient.ServiceRemoveOptions) (dockerclient.ServiceRemoveResult, error)
	ServiceInspect(ctx context.Context, serviceID string, opts dockerclient.ServiceInspectOptions) (dockerclient.ServiceInspectResult, error)
	ServiceLogs(ctx context.Context, serviceID string, opts dockerclient.ServiceLogsOptions) (dockerclient.ServiceLogsResult, error)

	NodeList(ctx context.Context, opts dockerclient.NodeListOptions) (dockerclient.NodeListResult, error)
	NodeInspect(ctx context.Context, nodeID string, opts dockerclient.NodeInspectOptions) (dockerclient.NodeInspectResult, error)
	NodeUpdate(ctx context.Context, nodeID string, opts dockerclient.NodeUpdateOptions) (dockerclient.NodeUpdateResult, error)
	NodeRemove(ctx context.Context, nodeID string, opts dockerclient.NodeRemoveOptions) (dockerclient.NodeRemoveResult, error)

	NetworkCreate(ctx context.Context, name string, opts dockerclient.NetworkCreateOptions) (dockerclient.NetworkCreateResult, error)
	NetworkList(ctx context.Context, opts dockerclient.NetworkListOptions) (dockerclient.NetworkListResult, error)
	NetworkInspect(ctx context.Context, network string, opts dockerclient.NetworkInspectOptions) (dockerclient.NetworkInspectResult, error)
	NetworkRemove(ctx context.Context, network string, opts dockerclient.NetworkRemoveOptions) (dockerclient.NetworkRemoveResult, error)

	SecretCreate(ctx context.Context, opts dockerclient.SecretCreateOptions) (dockerclient.SecretCreateResult, error)
	SecretList(ctx context.Context, opts dockerclient.SecretListOptions) (dockerclient.SecretListResult, error)
	SecretInspect(ctx context.Context, id string, opts dockerclient.SecretInspectOptions) (dockerclient.SecretInspectResult, error)
	SecretUpdate(ctx context.Context, id string, opts dockerclient.SecretUpdateOptions) (dockerclient.SecretUpdateResult, error)
	SecretRemove(ctx context.Context, id string, opts dockerclient.SecretRemoveOptions) (dockerclient.SecretRemoveResult, error)

	ConfigCreate(ctx context.Context, opts dockerclient.ConfigCreateOptions) (dockerclient.ConfigCreateResult, error)
	ConfigList(ctx context.Context, opts dockerclient.ConfigListOptions) (dockerclient.ConfigListResult, error)
	ConfigInspect(ctx context.Context, id string, opts dockerclient.ConfigInspectOptions) (dockerclient.ConfigInspectResult, error)
	ConfigUpdate(ctx context.Context, id string, opts dockerclient.ConfigUpdateOptions) (dockerclient.ConfigUpdateResult, error)
	ConfigRemove(ctx context.Context, id string, opts dockerclient.ConfigRemoveOptions) (dockerclient.ConfigRemoveResult, error)

	TaskList(ctx context.Context, opts dockerclient.TaskListOptions) (dockerclient.TaskListResult, error)
	TaskInspect(ctx context.Context, taskID string, opts dockerclient.TaskInspectOptions) (dockerclient.TaskInspectResult, error)

	ImagePull(ctx context.Context, ref string, opts dockerclient.ImagePullOptions) (io.ReadCloser, error)
	ContainerLogs(ctx context.Context, containerID string, opts dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error)

	Events(ctx context.Context, opts dockerclient.EventsListOptions) dockerclient.EventsResult
}

// dockerAPI adapts *dockerclient.Client to APIClient, narrowing ImagePull's
// return to the io.ReadCloser surface PullImage consumes. The declared method
// shadows the promoted one from the embedded SDK client.
type dockerAPI struct {
	*dockerclient.Client
}

// ImagePull narrows the SDK result to the io.ReadCloser PullImage consumes.
func (d dockerAPI) ImagePull(ctx context.Context, ref string, opts dockerclient.ImagePullOptions) (io.ReadCloser, error) {
	resp, err := d.Client.ImagePull(ctx, ref, opts)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

var _ APIClient = dockerAPI{}

// Client wraps the Docker API client for swarm operations.
type Client struct {
	raw APIClient
}

// New returns a Client connected to the given Docker host.
func New(host string) (*Client, error) {
	cli, err := dockerclient.New(dockerclient.WithHost(host))
	if err != nil {
		return nil, err
	}
	return &Client{raw: dockerAPI{cli}}, nil
}

// NewWithAPI returns a Client backed by an existing APIClient implementation.
// It exists so dependent packages' tests can inject fakes for the daemon.
func NewWithAPI(raw APIClient) *Client {
	return &Client{raw: raw}
}

// Ping verifies connectivity to the Docker daemon.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.raw.Ping(ctx, dockerclient.PingOptions{})
	return err
}

// ListServices returns all services in the swarm.
func (c *Client) ListServices(ctx context.Context) ([]swarm.Service, error) {
	result, err := c.raw.ServiceList(ctx, dockerclient.ServiceListOptions{})
	return result.Items, err
}

// CreateService creates a new swarm service.
func (c *Client) CreateService(ctx context.Context, spec swarm.ServiceSpec) (string, error) {
	resp, err := c.raw.ServiceCreate(ctx, dockerclient.ServiceCreateOptions{Spec: spec})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// UpdateService updates a service at the given version. Callers send the
// FULL desired spec, so on a version conflict ("update out of sequence" —
// another writer or a swarm-side rollback bumped the version between our
// read and write) it re-reads the live version and retries with the same
// desired spec, up to three attempts.
func (c *Client) UpdateService(ctx context.Context, id string, version uint64, spec swarm.ServiceSpec) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		_, lastErr = c.raw.ServiceUpdate(ctx, id, dockerclient.ServiceUpdateOptions{
			Version: swarm.Version{Index: version},
			Spec:    spec,
		})
		if lastErr == nil {
			return nil
		}
		if !isOutOfSequenceErr(lastErr) {
			return lastErr
		}
		live, err := c.GetService(ctx, id)
		if err != nil {
			return lastErr
		}
		version = live.Version.Index
	}
	return lastErr
}

func isOutOfSequenceErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "out of sequence")
}

// RemoveService removes a service.
func (c *Client) RemoveService(ctx context.Context, id string) error {
	_, err := c.raw.ServiceRemove(ctx, id, dockerclient.ServiceRemoveOptions{})
	return err
}

// ListNodes returns all nodes in the swarm.
func (c *Client) ListNodes(ctx context.Context) ([]swarm.Node, error) {
	result, err := c.raw.NodeList(ctx, dockerclient.NodeListOptions{})
	return result.Items, err
}

// CreateNetwork creates an overlay network.
func (c *Client) CreateNetwork(ctx context.Context, name string) (string, error) {
	resp, err := c.raw.NetworkCreate(ctx, name, dockerclient.NetworkCreateOptions{Driver: "overlay"})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// ListNetworks returns all networks in the swarm.
func (c *Client) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	result, err := c.raw.NetworkList(ctx, dockerclient.NetworkListOptions{})
	return result.Items, err
}

// RemoveNetwork removes a network.
func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	_, err := c.raw.NetworkRemove(ctx, id, dockerclient.NetworkRemoveOptions{})
	return err
}

// CreateSecret stores a new secret in the swarm.
func (c *Client) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error) {
	resp, err := c.raw.SecretCreate(ctx, dockerclient.SecretCreateOptions{Spec: spec})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// ListSecrets returns all secrets in the swarm.
func (c *Client) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	result, err := c.raw.SecretList(ctx, dockerclient.SecretListOptions{})
	return result.Items, err
}

// RemoveSecret removes a secret.
func (c *Client) RemoveSecret(ctx context.Context, id string) error {
	_, err := c.raw.SecretRemove(ctx, id, dockerclient.SecretRemoveOptions{})
	return err
}

// CreateConfig stores a config, replacing any stale copy with the same name.
func (c *Client) CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error) {
	resp, err := c.raw.ConfigCreate(ctx, dockerclient.ConfigCreateOptions{Spec: spec})
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// ListConfigs returns all configs in the swarm.
func (c *Client) ListConfigs(ctx context.Context) ([]swarm.Config, error) {
	result, err := c.raw.ConfigList(ctx, dockerclient.ConfigListOptions{})
	return result.Items, err
}

// RemoveConfig removes a config.
func (c *Client) RemoveConfig(ctx context.Context, id string) error {
	_, err := c.raw.ConfigRemove(ctx, id, dockerclient.ConfigRemoveOptions{})
	return err
}

// EnsureConfig creates the named swarm config with data, or replaces it when
// its payload differs. Config payloads are immutable in Swarm, so an update
// is a remove-then-create; if the old config is still referenced by a
// service, the remove error is returned.
func (c *Client) EnsureConfig(ctx context.Context, name string, data []byte) error {
	result, err := c.raw.ConfigList(ctx, dockerclient.ConfigListOptions{
		Filters: dockerclient.Filters{}.Add("name", name),
	})
	if err != nil {
		return fmt.Errorf("list config %s: %w", name, err)
	}
	var existing *swarm.Config
	for i := range result.Items {
		if result.Items[i].Spec.Name == name {
			existing = &result.Items[i]
			break
		}
	}
	if existing != nil && bytes.Equal(existing.Spec.Data, data) {
		return nil
	}
	if existing != nil {
		if err := c.RemoveConfig(ctx, existing.ID); err != nil {
			return fmt.Errorf("remove stale config %s: %w", name, err)
		}
	}
	spec := swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: name},
		Data:        data,
	}
	if _, err := c.CreateConfig(ctx, spec); err != nil {
		return fmt.Errorf("create config %s: %w", name, err)
	}
	return nil
}

// PullImage pulls an image on every node that runs the service.
func (c *Client) PullImage(ctx context.Context, ref string) error {
	r, err := c.raw.ImagePull(ctx, ref, dockerclient.ImagePullOptions{})
	if err != nil {
		return err
	}
	return r.Close()
}

// ContainerLogs streams a container's logs to the client.
func (c *Client) ContainerLogs(ctx context.Context, id string) error {
	r, err := c.raw.ContainerLogs(ctx, id, dockerclient.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: "200"})
	if err != nil {
		return err
	}
	return r.Close()
}

// ListTasks returns the tasks of a service.
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

// GetService returns a single service by ID or name.
func (c *Client) GetService(ctx context.Context, id string) (swarm.Service, error) {
	result, err := c.raw.ServiceInspect(ctx, id, dockerclient.ServiceInspectOptions{})
	return result.Service, err
}

// ServiceLogsOptions selects which service log output to stream.
type ServiceLogsOptions struct {
	ShowStdout bool
	ShowStderr bool
	Since      string
	Until      string
	Timestamps bool
	Follow     bool
	Tail       string // number of lines, or "all"
}

// ServiceLogs returns a multiplexed stdout/stderr log stream for the tasks of
// a service. The stream uses the Docker multiplexed frame format when the
// daemon does not attach TTYs; callers own the returned reader and must close
// it. The stream is closed automatically when ctx is canceled.
func (c *Client) ServiceLogs(ctx context.Context, id string, opts ServiceLogsOptions) (io.ReadCloser, error) {
	return c.raw.ServiceLogs(ctx, id, dockerclient.ServiceLogsOptions{
		ShowStdout: opts.ShowStdout,
		ShowStderr: opts.ShowStderr,
		Since:      opts.Since,
		Until:      opts.Until,
		Timestamps: opts.Timestamps,
		Follow:     opts.Follow,
		Tail:       opts.Tail,
	})
}

// GetTask returns a single task by ID.
func (c *Client) GetTask(ctx context.Context, taskID string) (swarm.Task, error) {
	result, err := c.raw.TaskInspect(ctx, taskID, dockerclient.TaskInspectOptions{})
	return result.Task, err
}

// UpdateNode updates a node's spec at the given object version. Node
// versions also move on swarm-side status changes, so a version conflict
// re-reads the live version and retries with the caller's full spec.
func (c *Client) UpdateNode(ctx context.Context, nodeID string, version uint64, spec swarm.NodeSpec) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		_, lastErr = c.raw.NodeUpdate(ctx, nodeID, dockerclient.NodeUpdateOptions{
			Version: swarm.Version{Index: version},
			Spec:    spec,
		})
		if lastErr == nil {
			return nil
		}
		if !isOutOfSequenceErr(lastErr) {
			return lastErr
		}
		live, err := c.GetNode(ctx, nodeID)
		if err != nil {
			return lastErr
		}
		version = live.Version.Index
	}
	return lastErr
}

// RemoveNode removes a node from the swarm; force removes it even if it is
// unreachable.
func (c *Client) RemoveNode(ctx context.Context, nodeID string, force bool) error {
	_, err := c.raw.NodeRemove(ctx, nodeID, dockerclient.NodeRemoveOptions{Force: force})
	return err
}

// GetSecret returns a single secret by ID or name (data is never included).
func (c *Client) GetSecret(ctx context.Context, id string) (swarm.Secret, error) {
	result, err := c.raw.SecretInspect(ctx, id, dockerclient.SecretInspectOptions{})
	return result.Secret, err
}

// UpdateSecret rotates secret metadata/labels at the given object version.
// The daemon does not allow changing secret data in place; rotating values
// requires creating a new secret and updating referencing services.
func (c *Client) UpdateSecret(ctx context.Context, id string, version uint64, spec swarm.SecretSpec) error {
	_, err := c.raw.SecretUpdate(ctx, id, dockerclient.SecretUpdateOptions{
		Version: swarm.Version{Index: version},
		Spec:    spec,
	})
	return err
}

// GetConfig returns a single config by ID or name, including its data.
func (c *Client) GetConfig(ctx context.Context, id string) (swarm.Config, error) {
	result, err := c.raw.ConfigInspect(ctx, id, dockerclient.ConfigInspectOptions{})
	return result.Config, err
}

// UpdateConfig replaces config data/metadata at the given object version.
func (c *Client) UpdateConfig(ctx context.Context, id string, version uint64, spec swarm.ConfigSpec) error {
	_, err := c.raw.ConfigUpdate(ctx, id, dockerclient.ConfigUpdateOptions{
		Version: swarm.Version{Index: version},
		Spec:    spec,
	})
	return err
}

// Event is a swarm-scoped system event for one of the cluster object types.
type Event struct {
	Type       events.Type
	Action     events.Action
	ID         string
	Name       string
	Attributes map[string]string
}

// EventHandler receives typed swarm events. Nil callbacks are skipped;
// a callback returning an error terminates the subscription.
type EventHandler struct {
	OnService func(Event) error
	OnNode    func(Event) error
	OnSecret  func(Event) error
	OnConfig  func(Event) error
	OnNetwork func(Event) error
}

// Events subscribes to docker system events filtered to scope=swarm for the
// service/node/secret/config/network object types and dispatches typed
// callbacks until ctx is canceled, the stream errors, or a callback fails.
func (c *Client) Events(ctx context.Context, handler EventHandler) error {
	filters := make(dockerclient.Filters).
		Add("scope", "swarm").
		Add("type", "service", "node", "secret", "config", "network")
	result := c.raw.Events(ctx, dockerclient.EventsListOptions{Filters: filters})
	for {
		select {
		case err := <-result.Err:
			if errors.Is(err, context.Canceled) {
				return ctx.Err()
			}
			return fmt.Errorf("swarm event stream: %w", err)
		case msg := <-result.Messages:
			var fn func(Event) error
			switch msg.Type {
			case events.ServiceEventType:
				fn = handler.OnService
			case events.NodeEventType:
				fn = handler.OnNode
			case events.SecretEventType:
				fn = handler.OnSecret
			case events.ConfigEventType:
				fn = handler.OnConfig
			case events.NetworkEventType:
				fn = handler.OnNetwork
			default:
				continue
			}
			if fn == nil {
				continue
			}
			if err := fn(Event{
				Type:       msg.Type,
				Action:     msg.Action,
				ID:         msg.Actor.ID,
				Name:       msg.Actor.Attributes["name"],
				Attributes: msg.Actor.Attributes,
			}); err != nil {
				return fmt.Errorf("swarm event handler for %s/%s: %w", msg.Type, msg.Action, err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// InspectNetwork returns a single swarm network by ID or name.
func (c *Client) InspectNetwork(ctx context.Context, id string) (network.Inspect, error) {
	result, err := c.raw.NetworkInspect(ctx, id, dockerclient.NetworkInspectOptions{})
	if err != nil {
		return network.Inspect{}, fmt.Errorf("inspect network %s: %w", id, err)
	}
	return result.Network, nil
}
