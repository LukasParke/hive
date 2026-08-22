package maintenance

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	v1 "github.com/luke/hive/proto/gen/agent/v1"
	agentv1connect "github.com/luke/hive/proto/gen/agent/v1/agentv1connect"
	mswarm "github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"

	"github.com/luke/hive/control-plane/internal/agentclient"
	"github.com/luke/hive/control-plane/internal/swarm"
)

var errBoom = errors.New("boom")

// fakeSwarm embeds swarm.APIClient so only the daemon slices the handler
// reaches (service/task listing for agent-address resolution, node listing
// for rolling maintenance) need overriding.
type fakeSwarm struct {
	swarm.APIClient

	services []mswarm.Service
	svcErr   error
	tasks    []mswarm.Task
	tasksErr error
	nodes    []mswarm.Node
	nodesErr error
}

func (f *fakeSwarm) ServiceList(ctx context.Context, opts dockerclient.ServiceListOptions) (dockerclient.ServiceListResult, error) {
	return dockerclient.ServiceListResult{Items: f.services}, f.svcErr
}

func (f *fakeSwarm) TaskList(ctx context.Context, opts dockerclient.TaskListOptions) (dockerclient.TaskListResult, error) {
	return dockerclient.TaskListResult{Items: f.tasks}, f.tasksErr
}

func (f *fakeSwarm) NodeList(ctx context.Context, opts dockerclient.NodeListOptions) (dockerclient.NodeListResult, error) {
	return dockerclient.NodeListResult{Items: f.nodes}, f.nodesErr
}

// withAgentOnNode scripts the swarm view so the agent service's running task
// sits on nodeID with overlay IP 127.0.0.1.
func (f *fakeSwarm) withAgentOnNode(nodeID string) *fakeSwarm {
	f.services = []mswarm.Service{{
		ID: "svc-agent",
		Spec: mswarm.ServiceSpec{
			Annotations: mswarm.Annotations{Labels: map[string]string{"hive.service": "agent"}},
		},
	}}
	f.tasks = append(f.tasks, mswarm.Task{
		ID:           "t-" + nodeID,
		NodeID:       nodeID,
		DesiredState: mswarm.TaskStateRunning,
		NetworksAttachments: []mswarm.NetworkAttachment{{
			Network:   mswarm.Network{Spec: mswarm.NetworkSpec{Annotations: mswarm.Annotations{Name: "hive_internal"}}},
			Addresses: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
		}},
	})
	return f
}

// fakeAgent is a scripted AgentService handler recording received operations.
type fakeAgent struct {
	agentv1connect.UnimplementedAgentServiceHandler

	mu             sync.Mutex
	healthErr      error
	failHealthFrom int // 1-based Health call number from which every call fails
	execErr        error
	rebootRequired bool
	healthCalls    int
	execOps        []v1.HostOperation
	pkgChecks      int
}

func (f *fakeAgent) Health(ctx context.Context, req *connect.Request[v1.HealthRequest]) (*connect.Response[v1.HealthResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.healthCalls++
	if f.healthErr != nil {
		return nil, f.healthErr
	}
	if f.failHealthFrom > 0 && f.healthCalls >= f.failHealthFrom {
		return nil, errBoom
	}
	return connect.NewResponse(&v1.HealthResponse{}), nil
}

func (f *fakeAgent) HostExec(ctx context.Context, req *connect.Request[v1.HostOperationRequest]) (*connect.Response[v1.HostOperationResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execOps = append(f.execOps, req.Msg.Operation)
	if f.execErr != nil {
		return nil, f.execErr
	}
	return connect.NewResponse(&v1.HostOperationResponse{ExitCode: 0, Stdout: "ok", DurationMs: 12}), nil
}

func (f *fakeAgent) GetPackageStatus(ctx context.Context, req *connect.Request[v1.PackageStatusRequest]) (*connect.Response[v1.PackageStatusResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pkgChecks++
	return connect.NewResponse(&v1.PackageStatusResponse{RebootRequired: f.rebootRequired}), nil
}

// startFakeAgent runs a fake agent on an ephemeral port and points the
// maintenance resolver at it.
func startFakeAgent(t *testing.T, h agentv1connect.AgentServiceHandler) {
	t.Helper()
	mux := http.NewServeMux()
	path, handler := agentv1connect.NewAgentServiceHandler(h)
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	old := agentAddrPort
	agentAddrPort = ":" + port
	t.Cleanup(func() { agentAddrPort = old })
}

func newTestHandler(t *testing.T, fs *fakeSwarm) *Handler {
	t.Helper()
	dialer, err := agentclient.NewDialer(nil, tls.Certificate{}, false)
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	return NewHandler(dialer, swarm.NewWithAPI(fs))
}

func TestExecuteSuccessWithReboot(t *testing.T) {
	agent := &fakeAgent{rebootRequired: true}
	startFakeAgent(t, agent)

	h := newTestHandler(t, (&fakeSwarm{}).withAgentOnNode("nodeA"))
	res, err := h.Execute(context.Background(), NodeMaintenanceJob{
		NodeID:         "nodeA",
		Operations:     []string{"security_updates", "all_updates", "update_check"},
		RebootIfNeeded: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success || res.Error != "" {
		t.Fatalf("Success=%v Error=%q, want clean success", res.Success, res.Error)
	}
	if res.NodeID != "nodeA" || res.StartedAt.IsZero() || res.FinishedAt.IsZero() {
		t.Fatalf("result metadata incomplete: %+v", res)
	}
	wantOps := []v1.HostOperation{
		v1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_SECURITY,
		v1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_ALL,
		v1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK,
		v1.HostOperation_HOST_OPERATION_REBOOT_SCHEDULE,
	}
	if len(agent.execOps) != len(wantOps) {
		t.Fatalf("exec ops = %v, want %v", agent.execOps, wantOps)
	}
	for i, op := range wantOps {
		if agent.execOps[i] != op {
			t.Errorf("exec op[%d] = %v, want %v", i, agent.execOps[i], op)
		}
	}
	if agent.pkgChecks != 1 {
		t.Fatalf("pkg checks = %d, want 1", agent.pkgChecks)
	}
	if len(res.Steps) != 4 {
		t.Fatalf("steps = %d (%+v), want 4", len(res.Steps), res.Steps)
	}
	for _, s := range res.Steps[:3] {
		if s.ExitCode != 0 || s.Stdout != "ok" || s.DurationMs != 12 || s.Error != "" {
			t.Errorf("step %+v, want mapped exec result", s)
		}
	}
	reboot := res.Steps[3]
	if reboot.Operation != "reboot" || reboot.ExitCode != 0 || reboot.Stdout != "ok" {
		t.Errorf("reboot step = %+v", reboot)
	}
}

func TestExecuteUnknownOperation(t *testing.T) {
	agent := &fakeAgent{}
	startFakeAgent(t, agent)

	h := newTestHandler(t, (&fakeSwarm{}).withAgentOnNode("nodeA"))
	res, err := h.Execute(context.Background(), NodeMaintenanceJob{
		NodeID:     "nodeA",
		Operations: []string{"bogus_op"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatal("unknown ops must not fail the run")
	}
	if len(res.Steps) != 1 || res.Steps[0].Operation != "bogus_op" || res.Steps[0].Error != "unknown operation" {
		t.Fatalf("steps = %+v, want single unknown-op step", res.Steps)
	}
	if len(agent.execOps) != 0 {
		t.Fatalf("exec ops = %v, want none for unknown operation", agent.execOps)
	}
}

func TestExecuteHostExecFailure(t *testing.T) {
	agent := &fakeAgent{execErr: errBoom}
	startFakeAgent(t, agent)

	h := newTestHandler(t, (&fakeSwarm{}).withAgentOnNode("nodeA"))
	res, err := h.Execute(context.Background(), NodeMaintenanceJob{
		NodeID:     "nodeA",
		Operations: []string{"all_updates"},
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want errBoom", err)
	}
	if !strings.Contains(res.Error, "operation all_updates failed") {
		t.Fatalf("result error = %q", res.Error)
	}
	if len(res.Steps) != 1 || res.Steps[0].Error == "" {
		t.Fatalf("steps = %+v, want failing step recorded", res.Steps)
	}
}

func TestExecuteRebootFailureRecorded(t *testing.T) {
	agent := &fakeAgent{rebootRequired: true, execErr: errBoom}
	startFakeAgent(t, agent)

	h := newTestHandler(t, (&fakeSwarm{}).withAgentOnNode("nodeA"))
	res, err := h.Execute(context.Background(), NodeMaintenanceJob{
		NodeID:         "nodeA",
		RebootIfNeeded: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Steps) != 1 || res.Steps[0].Operation != "reboot" || res.Steps[0].Error == "" {
		t.Fatalf("steps = %+v, want failing reboot step", res.Steps)
	}
	if !res.Success {
		t.Fatal("a failed reboot must not fail the overall run")
	}
}

func TestExecutePreFlightHealthFailure(t *testing.T) {
	agent := &fakeAgent{healthErr: errBoom}
	startFakeAgent(t, agent)

	h := newTestHandler(t, (&fakeSwarm{}).withAgentOnNode("nodeA"))
	res, err := h.Execute(context.Background(), NodeMaintenanceJob{NodeID: "nodeA"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want errBoom", err)
	}
	if !strings.Contains(res.Error, "node health check failed") {
		t.Fatalf("result error = %q", res.Error)
	}
}

func TestExecutePostFlightHealthFailure(t *testing.T) {
	// First Health call (pre-flight) succeeds, every later one fails.
	agent := &fakeAgent{failHealthFrom: 2}
	startFakeAgent(t, agent)

	h := newTestHandler(t, (&fakeSwarm{}).withAgentOnNode("nodeA"))
	res, err := h.Execute(context.Background(), NodeMaintenanceJob{NodeID: "nodeA"})
	if err != nil {
		t.Fatalf("err = %v, want nil (result carries failure)", err)
	}
	if res.Success {
		t.Fatal("Success = true, want false after post-health failure")
	}
	if !strings.Contains(res.Error, "post-maintenance health check failed") {
		t.Fatalf("result error = %q", res.Error)
	}
}

func TestExecuteResolveAgentAddrFailure(t *testing.T) {
	agent := &fakeAgent{}
	startFakeAgent(t, agent)

	// No scripted service: agent address resolution must fail.
	h := newTestHandler(t, &fakeSwarm{svcErr: errBoom})
	res, err := h.Execute(context.Background(), NodeMaintenanceJob{NodeID: "nodeA"})
	if err == nil || !strings.Contains(err.Error(), "list networks") && !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want wrapped resolve error", err)
	}
	if !strings.Contains(res.Error, "resolve agent address") {
		t.Fatalf("result error = %q", res.Error)
	}
}

func TestExecuteNoRunningAgentTask(t *testing.T) {
	agent := &fakeAgent{}
	startFakeAgent(t, agent)

	fs := (&fakeSwarm{}).withAgentOnNode("other-node")
	h := newTestHandler(t, fs)
	_, err := h.Execute(context.Background(), NodeMaintenanceJob{NodeID: "nodeA"})
	if err == nil || !strings.Contains(err.Error(), "no running task for hive.service=agent on node nodeA") {
		t.Fatalf("err = %v, want missing-task resolve failure", err)
	}
}

func TestResolveAgentAddrAppendsPort(t *testing.T) {
	h := newTestHandler(t, (&fakeSwarm{}).withAgentOnNode("nodeX"))
	addr, err := h.resolveAgentAddr(context.Background(), "nodeX")
	if err != nil {
		t.Fatalf("resolveAgentAddr: %v", err)
	}
	if addr != "127.0.0.1"+agentAddrPort {
		t.Fatalf("addr = %q, want ip+port", addr)
	}
}

func TestExecuteRollingStopsOnFirstFailure(t *testing.T) {
	// Every pre-flight health check fails, so the very first node stops
	// the rolling run.
	agent := &fakeAgent{healthErr: errBoom}
	startFakeAgent(t, agent)

	fs := (&fakeSwarm{}).withAgentOnNode("wrk1").withAgentOnNode("wrk2").withAgentOnNode("mgr1")
	fs.nodes = []mswarm.Node{
		{ID: "mgr1", Spec: mswarm.NodeSpec{Role: "manager"}},
		{ID: "wrk1"},
		{ID: "wrk2"},
	}
	h := newTestHandler(t, fs)

	results, err := h.ExecuteRolling(context.Background(), NodeMaintenanceJob{Operations: []string{"update_check"}})
	if err != nil {
		t.Fatalf("ExecuteRolling: %v", err)
	}
	if len(results) != 1 || results[0].NodeID != "wrk1" {
		t.Fatalf("results = %+v, want only worker wrk1 before stop", results)
	}
	if results[0].Success {
		t.Fatal("wrk1 should have failed")
	}
}

func TestExecuteRollingAllSucceedWorkersFirst(t *testing.T) {
	agent := &fakeAgent{}
	startFakeAgent(t, agent)

	fs := (&fakeSwarm{}).
		withAgentOnNode("wrk1").
		withAgentOnNode("wrk2").
		withAgentOnNode("mgr1").
		withAgentOnNode("mgr2")
	fs.nodes = []mswarm.Node{
		{ID: "mgr1", Spec: mswarm.NodeSpec{Role: "manager"}},
		{ID: "wrk1"},
		{ID: "wrk2"},
		{ID: "mgr2", Spec: mswarm.NodeSpec{Role: "manager"}},
	}
	h := newTestHandler(t, fs)

	results, err := h.ExecuteRolling(context.Background(), NodeMaintenanceJob{})
	if err != nil {
		t.Fatalf("ExecuteRolling: %v", err)
	}
	got := make([]string, 0, len(results))
	for _, r := range results {
		got = append(got, r.NodeID)
		if !r.Success {
			t.Errorf("node %s failed: %s", r.NodeID, r.Error)
		}
	}
	want := []string{"wrk1", "wrk2", "mgr1", "mgr2"}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (workers before managers)", got, want)
		}
	}
}

func TestExecuteRollingListNodesError(t *testing.T) {
	fs := &fakeSwarm{nodesErr: errBoom}
	h := newTestHandler(t, fs)
	if _, err := h.ExecuteRolling(context.Background(), NodeMaintenanceJob{}); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want list nodes error", err)
	}
}
