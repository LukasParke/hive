package swarm

import (
	"context"
	"errors"

	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"
)

// stubAPIClient implements APIClient with scripted results. Each method has a
// paired result/error field; retry-sensitive methods additionally script
// per-call sequences.
type stubAPIClient struct {
	pingRes dockerclient.PingResult
	pingErr error

	svcList    []swarm.Service
	svcListErr error

	svcCreateID  string
	svcCreateErr error

	svcUpdateErrs []error // consumed in order; last repeats
	svcUpdateN    int

	svcRemoveErr error

	svcInspect    swarm.Service
	svcInspectErr error

	svcLogs    io.ReadCloser
	svcLogsErr error

	nodeList    []swarm.Node
	nodeListErr error

	nodeInspect    swarm.Node
	nodeInspectErr error

	nodeUpdateErrs []error // consumed in order; last repeats
	nodeUpdateN    int

	nodeRemoveErr error

	netCreateID  string
	netCreateErr error

	netList    []network.Summary
	netListErr error

	netInspect    network.Inspect
	netInspectErr error

	netRemoveErr error

	secretCreateID  string
	secretCreateErr error

	secretList    []swarm.Secret
	secretListErr error

	secretInspect    swarm.Secret
	secretInspectErr error

	secretUpdateErr error
	secretRemoveErr error

	configCreateID  string
	configCreateErr error

	configList    []swarm.Config
	configListErr error

	configInspect    swarm.Config
	configInspectErr error

	configUpdateErr error
	configRemoveErr error

	taskList    []swarm.Task
	taskListErr error

	taskInspect    swarm.Task
	taskInspectErr error

	pull    io.ReadCloser
	pullErr error

	containerLogs    io.ReadCloser
	containerLogsErr error

	events dockerclient.EventsResult
}

func (s *stubAPIClient) Ping(ctx context.Context, opts dockerclient.PingOptions) (dockerclient.PingResult, error) {
	return s.pingRes, s.pingErr
}

func (s *stubAPIClient) ServiceList(ctx context.Context, opts dockerclient.ServiceListOptions) (dockerclient.ServiceListResult, error) {
	return dockerclient.ServiceListResult{Items: s.svcList}, s.svcListErr
}

func (s *stubAPIClient) ServiceCreate(ctx context.Context, opts dockerclient.ServiceCreateOptions) (dockerclient.ServiceCreateResult, error) {
	return dockerclient.ServiceCreateResult{ID: s.svcCreateID}, s.svcCreateErr
}

func (s *stubAPIClient) ServiceUpdate(ctx context.Context, serviceID string, opts dockerclient.ServiceUpdateOptions) (dockerclient.ServiceUpdateResult, error) {
	i := s.svcUpdateN
	s.svcUpdateN++
	if i < len(s.svcUpdateErrs) {
		return dockerclient.ServiceUpdateResult{}, s.svcUpdateErrs[i]
	}
	return dockerclient.ServiceUpdateResult{}, nil
}

func (s *stubAPIClient) ServiceRemove(ctx context.Context, serviceID string, opts dockerclient.ServiceRemoveOptions) (dockerclient.ServiceRemoveResult, error) {
	return dockerclient.ServiceRemoveResult{}, s.svcRemoveErr
}

func (s *stubAPIClient) ServiceInspect(ctx context.Context, serviceID string, opts dockerclient.ServiceInspectOptions) (dockerclient.ServiceInspectResult, error) {
	return dockerclient.ServiceInspectResult{Service: s.svcInspect}, s.svcInspectErr
}

func (s *stubAPIClient) ServiceLogs(ctx context.Context, serviceID string, opts dockerclient.ServiceLogsOptions) (dockerclient.ServiceLogsResult, error) {
	return s.svcLogs, s.svcLogsErr
}

func (s *stubAPIClient) NodeList(ctx context.Context, opts dockerclient.NodeListOptions) (dockerclient.NodeListResult, error) {
	return dockerclient.NodeListResult{Items: s.nodeList}, s.nodeListErr
}

func (s *stubAPIClient) NodeInspect(ctx context.Context, nodeID string, opts dockerclient.NodeInspectOptions) (dockerclient.NodeInspectResult, error) {
	return dockerclient.NodeInspectResult{Node: s.nodeInspect}, s.nodeInspectErr
}

func (s *stubAPIClient) NodeUpdate(ctx context.Context, nodeID string, opts dockerclient.NodeUpdateOptions) (dockerclient.NodeUpdateResult, error) {
	i := s.nodeUpdateN
	s.nodeUpdateN++
	if i < len(s.nodeUpdateErrs) {
		return dockerclient.NodeUpdateResult{}, s.nodeUpdateErrs[i]
	}
	return dockerclient.NodeUpdateResult{}, nil
}

func (s *stubAPIClient) NodeRemove(ctx context.Context, nodeID string, opts dockerclient.NodeRemoveOptions) (dockerclient.NodeRemoveResult, error) {
	return dockerclient.NodeRemoveResult{}, s.nodeRemoveErr
}

func (s *stubAPIClient) NetworkCreate(ctx context.Context, name string, opts dockerclient.NetworkCreateOptions) (dockerclient.NetworkCreateResult, error) {
	return dockerclient.NetworkCreateResult{ID: s.netCreateID}, s.netCreateErr
}

func (s *stubAPIClient) NetworkList(ctx context.Context, opts dockerclient.NetworkListOptions) (dockerclient.NetworkListResult, error) {
	return dockerclient.NetworkListResult{Items: s.netList}, s.netListErr
}

func (s *stubAPIClient) NetworkInspect(ctx context.Context, name string, opts dockerclient.NetworkInspectOptions) (dockerclient.NetworkInspectResult, error) {
	return dockerclient.NetworkInspectResult{Network: s.netInspect}, s.netInspectErr
}

func (s *stubAPIClient) NetworkRemove(ctx context.Context, name string, opts dockerclient.NetworkRemoveOptions) (dockerclient.NetworkRemoveResult, error) {
	return dockerclient.NetworkRemoveResult{}, s.netRemoveErr
}

func (s *stubAPIClient) SecretCreate(ctx context.Context, opts dockerclient.SecretCreateOptions) (dockerclient.SecretCreateResult, error) {
	return dockerclient.SecretCreateResult{ID: s.secretCreateID}, s.secretCreateErr
}

func (s *stubAPIClient) SecretList(ctx context.Context, opts dockerclient.SecretListOptions) (dockerclient.SecretListResult, error) {
	return dockerclient.SecretListResult{Items: s.secretList}, s.secretListErr
}

func (s *stubAPIClient) SecretInspect(ctx context.Context, id string, opts dockerclient.SecretInspectOptions) (dockerclient.SecretInspectResult, error) {
	return dockerclient.SecretInspectResult{Secret: s.secretInspect}, s.secretInspectErr
}

func (s *stubAPIClient) SecretUpdate(ctx context.Context, id string, opts dockerclient.SecretUpdateOptions) (dockerclient.SecretUpdateResult, error) {
	return dockerclient.SecretUpdateResult{}, s.secretUpdateErr
}

func (s *stubAPIClient) SecretRemove(ctx context.Context, id string, opts dockerclient.SecretRemoveOptions) (dockerclient.SecretRemoveResult, error) {
	return dockerclient.SecretRemoveResult{}, s.secretRemoveErr
}

func (s *stubAPIClient) ConfigCreate(ctx context.Context, opts dockerclient.ConfigCreateOptions) (dockerclient.ConfigCreateResult, error) {
	return dockerclient.ConfigCreateResult{ID: s.configCreateID}, s.configCreateErr
}

func (s *stubAPIClient) ConfigList(ctx context.Context, opts dockerclient.ConfigListOptions) (dockerclient.ConfigListResult, error) {
	return dockerclient.ConfigListResult{Items: s.configList}, s.configListErr
}

func (s *stubAPIClient) ConfigInspect(ctx context.Context, id string, opts dockerclient.ConfigInspectOptions) (dockerclient.ConfigInspectResult, error) {
	return dockerclient.ConfigInspectResult{Config: s.configInspect}, s.configInspectErr
}

func (s *stubAPIClient) ConfigUpdate(ctx context.Context, id string, opts dockerclient.ConfigUpdateOptions) (dockerclient.ConfigUpdateResult, error) {
	return dockerclient.ConfigUpdateResult{}, s.configUpdateErr
}

func (s *stubAPIClient) ConfigRemove(ctx context.Context, id string, opts dockerclient.ConfigRemoveOptions) (dockerclient.ConfigRemoveResult, error) {
	return dockerclient.ConfigRemoveResult{}, s.configRemoveErr
}

func (s *stubAPIClient) TaskList(ctx context.Context, opts dockerclient.TaskListOptions) (dockerclient.TaskListResult, error) {
	return dockerclient.TaskListResult{Items: s.taskList}, s.taskListErr
}

func (s *stubAPIClient) TaskInspect(ctx context.Context, taskID string, opts dockerclient.TaskInspectOptions) (dockerclient.TaskInspectResult, error) {
	return dockerclient.TaskInspectResult{Task: s.taskInspect}, s.taskInspectErr
}

func (s *stubAPIClient) ImagePull(ctx context.Context, ref string, opts dockerclient.ImagePullOptions) (io.ReadCloser, error) {
	return s.pull, s.pullErr
}

func (s *stubAPIClient) ContainerLogs(ctx context.Context, containerID string, opts dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error) {
	return s.containerLogs, s.containerLogsErr
}

func (s *stubAPIClient) Events(ctx context.Context, opts dockerclient.EventsListOptions) dockerclient.EventsResult {
	return s.events
}

var _ APIClient = (*stubAPIClient)(nil)

// nopCloser is a bare io.ReadCloser for stubbed log/pull streams.
type nopCloser struct{ io.Reader }

func (nopCloser) Close() error { return nil }

var (
	errBoom          = errors.New("boom")
	errOutOfSequence = errors.New("rpc error: update out of sequence")
)

func newStub(t *testing.T) (*Client, *stubAPIClient) {
	t.Helper()
	s := &stubAPIClient{}
	return NewWithAPI(s), s
}

func TestPing(t *testing.T) {
	c, s := newStub(t)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	s.pingErr = errBoom
	if err := c.Ping(context.Background()); !errors.Is(err, errBoom) {
		t.Fatalf("Ping err = %v, want errBoom", err)
	}
}

func TestListServices(t *testing.T) {
	c, s := newStub(t)
	s.svcList = []swarm.Service{{ID: "svc1"}}
	got, err := c.ListServices(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != "svc1" {
		t.Fatalf("ListServices = %v, %v", got, err)
	}
	s.svcListErr = errBoom
	if _, err := c.ListServices(context.Background()); !errors.Is(err, errBoom) {
		t.Fatalf("ListServices err = %v", err)
	}
}

func TestCreateService(t *testing.T) {
	c, s := newStub(t)
	s.svcCreateID = "new-svc"
	id, err := c.CreateService(context.Background(), swarm.ServiceSpec{})
	if err != nil || id != "new-svc" {
		t.Fatalf("CreateService = %q, %v", id, err)
	}
	s.svcCreateErr = errBoom
	if _, err := c.CreateService(context.Background(), swarm.ServiceSpec{}); !errors.Is(err, errBoom) {
		t.Fatalf("CreateService err = %v", err)
	}
}

func TestUpdateService(t *testing.T) {
	ctx := context.Background()

	t.Run("immediate success", func(t *testing.T) {
		c, _ := newStub(t)
		if err := c.UpdateService(ctx, "svc1", 7, swarm.ServiceSpec{}); err != nil {
			t.Fatalf("UpdateService: %v", err)
		}
	})

	t.Run("out of sequence then success uses live version", func(t *testing.T) {
		c, s := newStub(t)
		s.svcUpdateErrs = []error{errOutOfSequence, nil}
		s.svcInspect = swarm.Service{ID: "svc1", Meta: swarm.Meta{Version: swarm.Version{Index: 42}}}
		if err := c.UpdateService(ctx, "svc1", 7, swarm.ServiceSpec{}); err != nil {
			t.Fatalf("UpdateService: %v", err)
		}
	})

	t.Run("persistent conflict exhausts attempts", func(t *testing.T) {
		c, s := newStub(t)
		s.svcUpdateErrs = []error{errOutOfSequence, errOutOfSequence, errOutOfSequence}
		s.svcInspect = swarm.Service{Meta: swarm.Meta{Version: swarm.Version{Index: 42}}}
		err := c.UpdateService(ctx, "svc1", 7, swarm.ServiceSpec{})
		if !errors.Is(err, errOutOfSequence) {
			t.Fatalf("UpdateService err = %v, want out-of-sequence", err)
		}
		if s.svcUpdateN != 3 {
			t.Fatalf("attempts = %d, want 3", s.svcUpdateN)
		}
	})

	t.Run("non-conflict error propagates immediately", func(t *testing.T) {
		c, s := newStub(t)
		s.svcUpdateErrs = []error{errBoom}
		if err := c.UpdateService(ctx, "svc1", 7, swarm.ServiceSpec{}); !errors.Is(err, errBoom) {
			t.Fatalf("UpdateService err = %v", err)
		}
		if s.svcUpdateN != 1 {
			t.Fatalf("attempts = %d, want 1", s.svcUpdateN)
		}
	})

	t.Run("inspect failure during retry returns last error", func(t *testing.T) {
		c, s := newStub(t)
		s.svcUpdateErrs = []error{errOutOfSequence}
		s.svcInspectErr = errBoom
		if err := c.UpdateService(ctx, "svc1", 7, swarm.ServiceSpec{}); !errors.Is(err, errOutOfSequence) {
			t.Fatalf("UpdateService err = %v, want last out-of-sequence", err)
		}
	})
}

func TestIsOutOfSequenceErr(t *testing.T) {
	if isOutOfSequenceErr(nil) {
		t.Fatal("nil error must not be out of sequence")
	}
	if !isOutOfSequenceErr(errOutOfSequence) {
		t.Fatal("out of sequence error not detected")
	}
	if isOutOfSequenceErr(errBoom) {
		t.Fatal("unrelated error detected as out of sequence")
	}
}

func TestRemoveService(t *testing.T) {
	c, s := newStub(t)
	if err := c.RemoveService(context.Background(), "svc1"); err != nil {
		t.Fatalf("RemoveService: %v", err)
	}
	s.svcRemoveErr = errBoom
	if err := c.RemoveService(context.Background(), "svc1"); !errors.Is(err, errBoom) {
		t.Fatalf("RemoveService err = %v", err)
	}
}

func TestGetService(t *testing.T) {
	c, s := newStub(t)
	s.svcInspect = swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}}}
	got, err := c.GetService(context.Background(), "web")
	if err != nil || got.ID != "svc1" {
		t.Fatalf("GetService = %v, %v", got, err)
	}
	s.svcInspectErr = errBoom
	if _, err := c.GetService(context.Background(), "web"); !errors.Is(err, errBoom) {
		t.Fatalf("GetService err = %v", err)
	}
}

func TestListNodes(t *testing.T) {
	c, s := newStub(t)
	s.nodeList = []swarm.Node{{ID: "n1"}}
	got, err := c.ListNodes(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != "n1" {
		t.Fatalf("ListNodes = %v, %v", got, err)
	}
	s.nodeListErr = errBoom
	if _, err := c.ListNodes(context.Background()); !errors.Is(err, errBoom) {
		t.Fatalf("ListNodes err = %v", err)
	}
}

func TestGetNode(t *testing.T) {
	c, s := newStub(t)
	s.nodeInspect = swarm.Node{ID: "n1"}
	got, err := c.GetNode(context.Background(), "n1")
	if err != nil || got.ID != "n1" {
		t.Fatalf("GetNode = %v, %v", got, err)
	}
	s.nodeInspectErr = errBoom
	if _, err := c.GetNode(context.Background(), "n1"); !errors.Is(err, errBoom) {
		t.Fatalf("GetNode err = %v", err)
	}
}

func TestUpdateNode(t *testing.T) {
	ctx := context.Background()

	t.Run("immediate success", func(t *testing.T) {
		c, _ := newStub(t)
		if err := c.UpdateNode(ctx, "n1", 3, swarm.NodeSpec{}); err != nil {
			t.Fatalf("UpdateNode: %v", err)
		}
	})

	t.Run("out of sequence then success", func(t *testing.T) {
		c, s := newStub(t)
		s.nodeUpdateErrs = []error{errOutOfSequence, nil}
		s.nodeInspect = swarm.Node{ID: "n1", Meta: swarm.Meta{Version: swarm.Version{Index: 99}}}
		if err := c.UpdateNode(ctx, "n1", 3, swarm.NodeSpec{}); err != nil {
			t.Fatalf("UpdateNode: %v", err)
		}
	})

	t.Run("persistent conflict exhausts attempts", func(t *testing.T) {
		c, s := newStub(t)
		s.nodeUpdateErrs = []error{errOutOfSequence, errOutOfSequence, errOutOfSequence}
		s.nodeInspect = swarm.Node{Meta: swarm.Meta{Version: swarm.Version{Index: 99}}}
		if err := c.UpdateNode(ctx, "n1", 3, swarm.NodeSpec{}); !errors.Is(err, errOutOfSequence) {
			t.Fatalf("UpdateNode err = %v", err)
		}
		if s.nodeUpdateN != 3 {
			t.Fatalf("attempts = %d, want 3", s.nodeUpdateN)
		}
	})

	t.Run("non-conflict error propagates immediately", func(t *testing.T) {
		c, s := newStub(t)
		s.nodeUpdateErrs = []error{errBoom}
		if err := c.UpdateNode(ctx, "n1", 3, swarm.NodeSpec{}); !errors.Is(err, errBoom) {
			t.Fatalf("UpdateNode err = %v", err)
		}
		if s.nodeUpdateN != 1 {
			t.Fatalf("attempts = %d, want 1", s.nodeUpdateN)
		}
	})

	t.Run("inspect failure during retry returns last error", func(t *testing.T) {
		c, s := newStub(t)
		s.nodeUpdateErrs = []error{errOutOfSequence}
		s.nodeInspectErr = errBoom
		if err := c.UpdateNode(ctx, "n1", 3, swarm.NodeSpec{}); !errors.Is(err, errOutOfSequence) {
			t.Fatalf("UpdateNode err = %v", err)
		}
	})
}

func TestRemoveNode(t *testing.T) {
	c, s := newStub(t)
	if err := c.RemoveNode(context.Background(), "n1", true); err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}
	s.nodeRemoveErr = errBoom
	if err := c.RemoveNode(context.Background(), "n1", false); !errors.Is(err, errBoom) {
		t.Fatalf("RemoveNode err = %v", err)
	}
}

func TestNetworks(t *testing.T) {
	ctx := context.Background()
	c, s := newStub(t)

	s.netCreateID = "net-1"
	id, err := c.CreateNetwork(ctx, "overlay_a")
	if err != nil || id != "net-1" {
		t.Fatalf("CreateNetwork = %q, %v", id, err)
	}
	s.netCreateErr = errBoom
	if _, err := c.CreateNetwork(ctx, "overlay_a"); !errors.Is(err, errBoom) {
		t.Fatalf("CreateNetwork err = %v", err)
	}

	s.netList = []network.Summary{{Network: network.Network{ID: "net-1", Name: "overlay_a"}}}
	nets, err := c.ListNetworks(ctx)
	if err != nil || len(nets) != 1 || nets[0].Name != "overlay_a" {
		t.Fatalf("ListNetworks = %v, %v", nets, err)
	}
	s.netListErr = errBoom
	if _, err := c.ListNetworks(ctx); !errors.Is(err, errBoom) {
		t.Fatalf("ListNetworks err = %v", err)
	}

	s.netInspect = network.Inspect{Network: network.Network{ID: "net-1"}}
	insp, err := c.InspectNetwork(ctx, "net-1")
	if err != nil || insp.ID != "net-1" {
		t.Fatalf("InspectNetwork = %v, %v", insp, err)
	}
	s.netInspectErr = errBoom
	if _, err := c.InspectNetwork(ctx, "net-1"); err == nil || !strings.Contains(err.Error(), "inspect network net-1") {
		t.Fatalf("InspectNetwork err = %v, want wrapped", err)
	}

	if err := c.RemoveNetwork(ctx, "net-1"); err != nil {
		t.Fatalf("RemoveNetwork: %v", err)
	}
	s.netRemoveErr = errBoom
	if err := c.RemoveNetwork(ctx, "net-1"); !errors.Is(err, errBoom) {
		t.Fatalf("RemoveNetwork err = %v", err)
	}
}

func TestSecrets(t *testing.T) {
	ctx := context.Background()
	c, s := newStub(t)

	s.secretCreateID = "sec-1"
	id, err := c.CreateSecret(ctx, swarm.SecretSpec{})
	if err != nil || id != "sec-1" {
		t.Fatalf("CreateSecret = %q, %v", id, err)
	}
	s.secretCreateErr = errBoom
	if _, err := c.CreateSecret(ctx, swarm.SecretSpec{}); !errors.Is(err, errBoom) {
		t.Fatalf("CreateSecret err = %v", err)
	}

	s.secretList = []swarm.Secret{{ID: "sec-1"}}
	secs, err := c.ListSecrets(ctx)
	if err != nil || len(secs) != 1 {
		t.Fatalf("ListSecrets = %v, %v", secs, err)
	}
	s.secretListErr = errBoom
	if _, err := c.ListSecrets(ctx); !errors.Is(err, errBoom) {
		t.Fatalf("ListSecrets err = %v", err)
	}

	s.secretInspect = swarm.Secret{ID: "sec-1"}
	sec, err := c.GetSecret(ctx, "sec-1")
	if err != nil || sec.ID != "sec-1" {
		t.Fatalf("GetSecret = %v, %v", sec, err)
	}
	s.secretInspectErr = errBoom
	if _, err := c.GetSecret(ctx, "sec-1"); !errors.Is(err, errBoom) {
		t.Fatalf("GetSecret err = %v", err)
	}

	if err := c.UpdateSecret(ctx, "sec-1", 2, swarm.SecretSpec{}); err != nil {
		t.Fatalf("UpdateSecret: %v", err)
	}
	s.secretUpdateErr = errBoom
	if err := c.UpdateSecret(ctx, "sec-1", 2, swarm.SecretSpec{}); !errors.Is(err, errBoom) {
		t.Fatalf("UpdateSecret err = %v", err)
	}

	if err := c.RemoveSecret(ctx, "sec-1"); err != nil {
		t.Fatalf("RemoveSecret: %v", err)
	}
	s.secretRemoveErr = errBoom
	if err := c.RemoveSecret(ctx, "sec-1"); !errors.Is(err, errBoom) {
		t.Fatalf("RemoveSecret err = %v", err)
	}
}

func TestConfigs(t *testing.T) {
	ctx := context.Background()
	c, s := newStub(t)

	s.configCreateID = "cfg-1"
	id, err := c.CreateConfig(ctx, swarm.ConfigSpec{})
	if err != nil || id != "cfg-1" {
		t.Fatalf("CreateConfig = %q, %v", id, err)
	}
	s.configCreateErr = errBoom
	if _, err := c.CreateConfig(ctx, swarm.ConfigSpec{}); !errors.Is(err, errBoom) {
		t.Fatalf("CreateConfig err = %v", err)
	}

	s.configList = []swarm.Config{{ID: "cfg-1"}}
	cfgs, err := c.ListConfigs(ctx)
	if err != nil || len(cfgs) != 1 {
		t.Fatalf("ListConfigs = %v, %v", cfgs, err)
	}
	s.configListErr = errBoom
	if _, err := c.ListConfigs(ctx); !errors.Is(err, errBoom) {
		t.Fatalf("ListConfigs err = %v", err)
	}

	s.configInspect = swarm.Config{ID: "cfg-1"}
	cfg, err := c.GetConfig(ctx, "cfg-1")
	if err != nil || cfg.ID != "cfg-1" {
		t.Fatalf("GetConfig = %v, %v", cfg, err)
	}
	s.configInspectErr = errBoom
	if _, err := c.GetConfig(ctx, "cfg-1"); !errors.Is(err, errBoom) {
		t.Fatalf("GetConfig err = %v", err)
	}

	if err := c.UpdateConfig(ctx, "cfg-1", 2, swarm.ConfigSpec{}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	s.configUpdateErr = errBoom
	if err := c.UpdateConfig(ctx, "cfg-1", 2, swarm.ConfigSpec{}); !errors.Is(err, errBoom) {
		t.Fatalf("UpdateConfig err = %v", err)
	}

	if err := c.RemoveConfig(ctx, "cfg-1"); err != nil {
		t.Fatalf("RemoveConfig: %v", err)
	}
	s.configRemoveErr = errBoom
	if err := c.RemoveConfig(ctx, "cfg-1"); !errors.Is(err, errBoom) {
		t.Fatalf("RemoveConfig err = %v", err)
	}
}

func TestEnsureConfig(t *testing.T) {
	ctx := context.Background()
	data := []byte("payload")

	t.Run("creates when absent", func(t *testing.T) {
		c, s := newStub(t)
		s.configCreateID = "cfg-new"
		if err := c.EnsureConfig(ctx, "app.conf", data); err != nil {
			t.Fatalf("EnsureConfig: %v", err)
		}
	})

	t.Run("no-op when data matches", func(t *testing.T) {
		c, s := newStub(t)
		s.configList = []swarm.Config{{ID: "cfg-old", Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: "app.conf"},
			Data:        data,
		}}}
		if err := c.EnsureConfig(ctx, "app.conf", data); err != nil {
			t.Fatalf("EnsureConfig: %v", err)
		}
	})

	t.Run("replaces stale copy", func(t *testing.T) {
		c, s := newStub(t)
		s.configList = []swarm.Config{{ID: "cfg-old", Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: "app.conf"},
			Data:        []byte("stale"),
		}}}
		s.configCreateID = "cfg-new"
		if err := c.EnsureConfig(ctx, "app.conf", data); err != nil {
			t.Fatalf("EnsureConfig: %v", err)
		}
	})

	t.Run("remove failure returned", func(t *testing.T) {
		c, s := newStub(t)
		s.configList = []swarm.Config{{ID: "cfg-old", Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: "app.conf"},
			Data:        []byte("stale"),
		}}}
		s.configRemoveErr = errBoom
		err := c.EnsureConfig(ctx, "app.conf", data)
		if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "remove stale config app.conf") {
			t.Fatalf("EnsureConfig err = %v", err)
		}
	})

	t.Run("create failure returned", func(t *testing.T) {
		c := NewWithAPI(&stubAPIClient{configCreateErr: errBoom})
		err := c.EnsureConfig(ctx, "app.conf", data)
		if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "create config app.conf") {
			t.Fatalf("EnsureConfig err = %v", err)
		}
	})

	t.Run("list failure returned", func(t *testing.T) {
		c := NewWithAPI(&stubAPIClient{configListErr: errBoom})
		err := c.EnsureConfig(ctx, "app.conf", data)
		if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "list config app.conf") {
			t.Fatalf("EnsureConfig err = %v", err)
		}
	})

	t.Run("ignores other configs with same name filter", func(t *testing.T) {
		c := NewWithAPI(&stubAPIClient{configList: []swarm.Config{
			{ID: "other", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "other.conf"}, Data: data}},
		}, configCreateID: "cfg-new"})
		if err := c.EnsureConfig(ctx, "app.conf", data); err != nil {
			t.Fatalf("EnsureConfig: %v", err)
		}
	})
}

func TestPullImage(t *testing.T) {
	c, s := newStub(t)
	s.pull = nopCloser{strings.NewReader(`{"status":"ok"}`)}
	if err := c.PullImage(context.Background(), "ghcr.io/x/y:1"); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	s.pullErr = errBoom
	if err := c.PullImage(context.Background(), "ghcr.io/x/y:1"); !errors.Is(err, errBoom) {
		t.Fatalf("PullImage err = %v", err)
	}
}

func TestContainerLogs(t *testing.T) {
	c, s := newStub(t)
	s.containerLogs = nopCloser{strings.NewReader("log line")}
	if err := c.ContainerLogs(context.Background(), "ctr1"); err != nil {
		t.Fatalf("ContainerLogs: %v", err)
	}
	s.containerLogsErr = errBoom
	if err := c.ContainerLogs(context.Background(), "ctr1"); !errors.Is(err, errBoom) {
		t.Fatalf("ContainerLogs err = %v", err)
	}
}

func TestServiceLogs(t *testing.T) {
	c, s := newStub(t)
	s.svcLogs = nopCloser{strings.NewReader("svc log")}
	r, err := c.ServiceLogs(context.Background(), "svc1", ServiceLogsOptions{ShowStdout: true, Tail: "100"})
	if err != nil {
		t.Fatalf("ServiceLogs: %v", err)
	}
	defer func() { _ = r.Close() }()
	body, _ := io.ReadAll(r)
	if string(body) != "svc log" {
		t.Fatalf("ServiceLogs body = %q", body)
	}
	s.svcLogsErr = errBoom
	if _, err := c.ServiceLogs(context.Background(), "svc1", ServiceLogsOptions{}); !errors.Is(err, errBoom) {
		t.Fatalf("ServiceLogs err = %v", err)
	}
}

func TestTasks(t *testing.T) {
	ctx := context.Background()
	c, s := newStub(t)

	s.taskList = []swarm.Task{{ID: "t1"}}
	tasks, err := c.ListTasks(ctx, "svc1")
	if err != nil || len(tasks) != 1 || tasks[0].ID != "t1" {
		t.Fatalf("ListTasks = %v, %v", tasks, err)
	}
	all, err := c.ListAllTasks(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListAllTasks = %v, %v", all, err)
	}
	s.taskListErr = errBoom
	if _, err := c.ListTasks(ctx, "svc1"); !errors.Is(err, errBoom) {
		t.Fatalf("ListTasks err = %v", err)
	}
	if _, err := c.ListAllTasks(ctx); !errors.Is(err, errBoom) {
		t.Fatalf("ListAllTasks err = %v", err)
	}

	s.taskListErr = nil
	s.taskInspect = swarm.Task{ID: "t1"}
	task, err := c.GetTask(ctx, "t1")
	if err != nil || task.ID != "t1" {
		t.Fatalf("GetTask = %v, %v", task, err)
	}
	s.taskInspectErr = errBoom
	if _, err := c.GetTask(ctx, "t1"); !errors.Is(err, errBoom) {
		t.Fatalf("GetTask err = %v", err)
	}
}

func TestServiceTaskIPOnNetwork(t *testing.T) {
	ctx := context.Background()

	service := swarm.Service{ID: "svc-agent", Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Labels: map[string]string{"hive.service": "agent"}},
	}}
	running := swarm.Task{
		ID:           "t1",
		NodeID:       "nodeA",
		DesiredState: swarm.TaskStateRunning,
		NetworksAttachments: []swarm.NetworkAttachment{{
			Network:   swarm.Network{Spec: swarm.NetworkSpec{Annotations: swarm.Annotations{Name: "hive_internal"}}},
			Addresses: []netip.Prefix{netip.MustParsePrefix("10.0.1.5/24")},
		}},
	}

	t.Run("resolves running task address", func(t *testing.T) {
		c, s := newStub(t)
		s.svcList = []swarm.Service{service}
		s.taskList = []swarm.Task{running}
		ip, err := c.ServiceTaskIPOnNetwork(ctx, "hive.service", "agent", "nodeA", "hive_internal")
		if err != nil || ip != "10.0.1.5" {
			t.Fatalf("ServiceTaskIPOnNetwork = %q, %v", ip, err)
		}
	})

	t.Run("no matching service", func(t *testing.T) {
		c, s := newStub(t)
		s.svcList = []swarm.Service{{ID: "other"}}
		_, err := c.ServiceTaskIPOnNetwork(ctx, "hive.service", "agent", "nodeA", "hive_internal")
		if err == nil || !strings.Contains(err.Error(), "no service found with label") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("service list failure", func(t *testing.T) {
		c, s := newStub(t)
		s.svcListErr = errBoom
		if _, err := c.ServiceTaskIPOnNetwork(ctx, "hive.service", "agent", "nodeA", "hive_internal"); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("task list failure", func(t *testing.T) {
		c, s := newStub(t)
		s.svcList = []swarm.Service{service}
		s.taskListErr = errBoom
		if _, err := c.ServiceTaskIPOnNetwork(ctx, "hive.service", "agent", "nodeA", "hive_internal"); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("no running task on node or network", func(t *testing.T) {
		c, s := newStub(t)
		s.svcList = []swarm.Service{service}
		s.taskList = []swarm.Task{
			{ID: "t-wrong-node", NodeID: "nodeB", DesiredState: swarm.TaskStateRunning},
			{ID: "t-not-running", NodeID: "nodeA", DesiredState: swarm.TaskStateShutdown},
			{ID: "t-wrong-network", NodeID: "nodeA", DesiredState: swarm.TaskStateRunning,
				NetworksAttachments: []swarm.NetworkAttachment{{
					Network:   swarm.Network{Spec: swarm.NetworkSpec{Annotations: swarm.Annotations{Name: "other_net"}}},
					Addresses: []netip.Prefix{netip.MustParsePrefix("10.0.2.5/24")},
				}}},
			{ID: "t-no-addresses", NodeID: "nodeA", DesiredState: swarm.TaskStateRunning,
				NetworksAttachments: []swarm.NetworkAttachment{{
					Network: swarm.Network{Spec: swarm.NetworkSpec{Annotations: swarm.Annotations{Name: "hive_internal"}}},
				}}},
		}
		_, err := c.ServiceTaskIPOnNetwork(ctx, "hive.service", "agent", "nodeA", "hive_internal")
		if err == nil || !strings.Contains(err.Error(), "no running task") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestEvents(t *testing.T) {
	msgs := make(chan events.Message, 8)
	errs := make(chan error, 1)
	c, s := newStub(t)
	s.events = dockerclient.EventsResult{Messages: msgs, Err: errs}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got []Event
	handler := EventHandler{
		OnService: func(e Event) error { got = append(got, e); return nil },
		OnNode: func(e Event) error {
			if e.ID == "n2" {
				return errBoom
			}
			got = append(got, e)
			return nil
		},
		OnSecret:  func(e Event) error { got = append(got, e); return nil },
		OnConfig:  func(e Event) error { got = append(got, e); return nil },
		OnNetwork: func(e Event) error { got = append(got, e); return nil },
	}

	done := make(chan error, 1)
	go func() { done <- c.Events(ctx, handler) }()

	send := func(typ events.Type, action events.Action, id string) {
		msgs <- events.Message{
			Type:   typ,
			Action: action,
			Actor: events.Actor{
				ID:         id,
				Attributes: map[string]string{"name": "obj-" + id},
			},
		}
	}
	send(events.ServiceEventType, events.ActionCreate, "s1")
	send(events.NodeEventType, events.ActionUpdate, "n1")
	send(events.SecretEventType, events.ActionCreate, "sec1")
	send(events.ConfigEventType, events.ActionCreate, "cfg1")
	send(events.NetworkEventType, events.ActionRemove, "net1")
	// Unhandled types and nil callbacks are skipped silently.
	msgs <- events.Message{Type: events.ImageEventType, Actor: events.Actor{ID: "img1"}}
	msgs <- events.Message{Type: events.ServiceEventType, Actor: events.Actor{ID: "s2"}}

	// Handler failure terminates the subscription.
	send(events.NodeEventType, events.ActionDestroy, "n2")
	// Wait for the loop to observe the failure before closing channels.
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "swarm event handler for node/destroy") {
			t.Fatalf("Events err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Events did not terminate after handler error")
	}
	if len(got) != 6 {
		t.Fatalf("dispatched = %d events, want 6", len(got))
	}
	if got[0].ID != "s1" || got[0].Name != "obj-s1" || got[0].Action != events.ActionCreate {
		t.Fatalf("first event = %+v", got[0])
	}
}

func TestEventsHandlerErrorWrapped(t *testing.T) {
	msgs := make(chan events.Message, 1)
	errs := make(chan error, 1)
	c := NewWithAPI(&stubAPIClient{events: dockerclient.EventsResult{Messages: msgs, Err: errs}})

	handler := EventHandler{
		OnService: func(e Event) error { return errors.New("kaboom") },
	}
	msgs <- events.Message{
		Type:   events.ServiceEventType,
		Action: events.ActionCreate,
		Actor:  events.Actor{ID: "s1"},
	}
	err := c.Events(context.Background(), handler)
	if err == nil || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("Events err = %v", err)
	}
}

func TestEventsNilCallbackSkipped(t *testing.T) {
	// Unbuffered: the send below completes only once the Events loop has
	// received the message, making the subsequent cancel deterministic.
	msgs := make(chan events.Message)
	errs := make(chan error)
	c := NewWithAPI(&stubAPIClient{events: dockerclient.EventsResult{Messages: msgs, Err: errs}})

	// Only OnService is registered; node events are skipped without error.
	handler := EventHandler{
		OnService: func(e Event) error { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Events(ctx, handler) }()

	msgs <- events.Message{
		Type:   events.NodeEventType,
		Action: events.ActionUpdate,
		Actor:  events.Actor{ID: "n1"},
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Events err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Events did not return on ctx.Done")
	}
}

func TestEventsStreamError(t *testing.T) {
	// Buffered + no cancellation: the only ready case in the loop is the
	// stream error, so the dispatch is deterministic.
	msgs := make(chan events.Message)
	errs := make(chan error, 1)
	c := NewWithAPI(&stubAPIClient{events: dockerclient.EventsResult{Messages: msgs, Err: errs}})

	errs <- errBoom
	err := c.Events(context.Background(), EventHandler{})
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "swarm event stream") {
		t.Fatalf("Events err = %v", err)
	}
}

func TestEventsContextCanceled(t *testing.T) {
	// The context is already canceled, so whichever case the loop takes —
	// the stream error reporting cancellation, or ctx.Done — Events must
	// return context.Canceled without blocking.
	msgs := make(chan events.Message)
	errs := make(chan error, 1)
	c := NewWithAPI(&stubAPIClient{events: dockerclient.EventsResult{Messages: msgs, Err: errs}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	errs <- context.Canceled

	done := make(chan error, 1)
	go func() { done <- c.Events(ctx, EventHandler{}) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Events err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Events did not return after stream cancellation")
	}
}

func TestEventsCtxDone(t *testing.T) {
	msgs := make(chan events.Message)
	errs := make(chan error)
	c := NewWithAPI(&stubAPIClient{events: dockerclient.EventsResult{Messages: msgs, Err: errs}})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Events(ctx, EventHandler{}) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Events err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Events did not return on ctx.Done")
	}
}

func TestNew(t *testing.T) {
	if _, err := New("::bogus host::"); err == nil {
		t.Fatal("New with invalid host must fail")
	}
	c, err := New("unix:///tmp/nonexistent-docker.sock")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c == nil {
		t.Fatal("New returned nil client")
	}
	// Ping against a dead socket must surface a connection error.
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("Ping on dead socket must fail")
	}
}

// fakeDockerDaemon serves just enough of the Docker HTTP API to exercise the
// dockerAPI adapter end to end.
func fakeDockerDaemon(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/images/create") {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDockerAPIImagePull(t *testing.T) {
	t.Run("success returns stream", func(t *testing.T) {
		srv := fakeDockerDaemon(t, http.StatusOK, `{"status":"pulled"}`)
		cli, err := dockerclient.New(dockerclient.WithHost("tcp://" + strings.TrimPrefix(srv.URL, "http://")))
		if err != nil {
			t.Fatalf("dockerclient.New: %v", err)
		}
		c := &Client{raw: dockerAPI{cli}}
		if err := c.PullImage(context.Background(), "ghcr.io/hive/agent:1"); err != nil {
			t.Fatalf("PullImage: %v", err)
		}
	})

	t.Run("daemon error propagates", func(t *testing.T) {
		srv := fakeDockerDaemon(t, http.StatusInternalServerError, "pull denied")
		cli, err := dockerclient.New(dockerclient.WithHost("tcp://" + strings.TrimPrefix(srv.URL, "http://")))
		if err != nil {
			t.Fatalf("dockerclient.New: %v", err)
		}
		c := &Client{raw: dockerAPI{cli}}
		err = c.PullImage(context.Background(), "ghcr.io/hive/agent:1")
		if err == nil || !strings.Contains(err.Error(), "pull denied") {
			t.Fatalf("PullImage err = %v", err)
		}
	})
}
