package agents

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	dockerswarm "github.com/moby/moby/api/types/swarm"

	"github.com/luke/hive/control-plane/internal/agentclient"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/ca"
	"github.com/luke/hive/control-plane/internal/testdb"
	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
	"github.com/luke/hive/proto/gen/agent/v1/agentv1connect"
)

// fakeAgentSwarm implements the SwarmClient seam.
type fakeAgentSwarm struct {
	nodes       []dockerswarm.Node
	services    []dockerswarm.Service
	tasks       []dockerswarm.Task
	taskIP      string
	taskIPErr   error
	listNodeErr error
	listSvcErr  error
	listTaskErr error
}

func (f *fakeAgentSwarm) ListNodes(context.Context) ([]dockerswarm.Node, error) {
	return f.nodes, f.listNodeErr
}

func (f *fakeAgentSwarm) ListServices(context.Context) ([]dockerswarm.Service, error) {
	return f.services, f.listSvcErr
}

func (f *fakeAgentSwarm) ListAllTasks(context.Context) ([]dockerswarm.Task, error) {
	return f.tasks, f.listTaskErr
}

func (f *fakeAgentSwarm) ServiceTaskIPOnNetwork(context.Context, string, string, string, string) (string, error) {
	if f.taskIPErr != nil {
		return "", f.taskIPErr
	}
	if f.taskIP == "" {
		return "127.0.0.1", nil
	}
	return f.taskIP, nil
}

func agentNode(id, hostname, state string) dockerswarm.Node {
	nodeState := dockerswarm.NodeStateReady
	if state != "" {
		nodeState = dockerswarm.NodeState(state)
	}
	return dockerswarm.Node{
		ID:          id,
		Description: dockerswarm.NodeDescription{Hostname: hostname},
		Status:      dockerswarm.NodeStatus{State: nodeState},
	}
}

func agentService(id, name string) dockerswarm.Service {
	return dockerswarm.Service{
		ID:   id,
		Spec: dockerswarm.ServiceSpec{Annotations: dockerswarm.Annotations{Name: name}},
	}
}

func containerTask(id, nodeID, containerID string) dockerswarm.Task {
	return dockerswarm.Task{
		ID:     id,
		NodeID: nodeID,
		Status: dockerswarm.TaskStatus{
			State:           dockerswarm.TaskStateRunning,
			ContainerStatus: &dockerswarm.ContainerStatus{ContainerID: containerID},
		},
	}
}

// recordedExec captures the ExecInput frames the fake agent receives.
type recordedExec struct {
	started  *agentv1.ExecStart
	resizes  []*agentv1.ResizeTerminal
	stdins   [][]byte
	startedC chan struct{} // signaled on first Start
}

func newRecordedExec() *recordedExec {
	return &recordedExec{startedC: make(chan struct{}, 1)}
}

// fakeAgent is a connect AgentServiceHandler double served over a real HTTP
// server so the handler's connect clients talk to something real.
type fakeAgent struct {
	agentv1connect.AgentServiceHandler // embedded; unimplemented RPCs panic if reached

	metrics    *agentv1.HostMetricsResponse
	pkgStatus  *agentv1.PackageStatusResponse
	execErr    error
	logsErr    error
	logs       []*agentv1.LogChunk
	streamFail bool // ExecStream fails without reading

	exec *recordedExec
}

func (f *fakeAgent) ExecStream(ctx context.Context, s *connect.BidiStream[agentv1.ExecInput, agentv1.ExecOutput]) error {
	if f.streamFail {
		return errors.New("exec refused")
	}
	for {
		in, err := s.Receive()
		if err != nil {
			return nil // client hung up; normal end
		}
		switch body := in.Body.(type) {
		case *agentv1.ExecInput_Start:
			f.exec.started = body.Start
			select {
			case f.exec.startedC <- struct{}{}:
			default:
			}
			_ = s.Send(&agentv1.ExecOutput{Body: &agentv1.ExecOutput_Stdout{Stdout: []byte("hello")}})
			_ = s.Send(&agentv1.ExecOutput{Body: &agentv1.ExecOutput_Stderr{Stderr: []byte("oops")}})
		case *agentv1.ExecInput_Resize:
			f.exec.resizes = append(f.exec.resizes, body.Resize)
		case *agentv1.ExecInput_Stdin:
			f.exec.stdins = append(f.exec.stdins, body.Stdin)
			// The session ends after the first stdin frame.
			_ = s.Send(&agentv1.ExecOutput{Body: &agentv1.ExecOutput_ExitCode{ExitCode: 0}})
			return nil
		}
	}
}

func (f *fakeAgent) StreamContainerLogs(_ context.Context, _ *connect.Request[agentv1.LogRequest], stream *connect.ServerStream[agentv1.LogChunk]) error {
	if f.logsErr != nil {
		return f.logsErr
	}
	for _, chunk := range f.logs {
		if err := stream.Send(chunk); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeAgent) GetHostMetrics(context.Context, *connect.Request[agentv1.HostMetricsRequest]) (*connect.Response[agentv1.HostMetricsResponse], error) {
	if f.metrics == nil {
		return nil, errors.New("metrics unavailable")
	}
	return connect.NewResponse(f.metrics), nil
}

func (f *fakeAgent) GetPackageStatus(context.Context, *connect.Request[agentv1.PackageStatusRequest]) (*connect.Response[agentv1.PackageStatusResponse], error) {
	if f.pkgStatus == nil {
		return nil, errors.New("package status unavailable")
	}
	return connect.NewResponse(f.pkgStatus), nil
}

func (f *fakeAgent) HostExec(_ context.Context, req *connect.Request[agentv1.HostOperationRequest]) (*connect.Response[agentv1.HostOperationResponse], error) {
	if f.execErr != nil {
		return nil, f.execErr
	}
	return connect.NewResponse(&agentv1.HostOperationResponse{
		ExitCode:   0,
		Stdout:     "ran " + req.Msg.Operation.String(),
		DurationMs: 12,
	}), nil
}

// startFakeAgent serves the fake over HTTP/2 (required for connect
// bidirectional streaming) and returns a client wired to trust it.
func startFakeAgent(t *testing.T, fake *fakeAgent) agentv1connect.AgentServiceClient {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewAgentServiceHandler(fake))
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return agentv1connect.NewAgentServiceClient(srv.Client(), srv.URL)
}

func newAgentsRouter(t *testing.T, fake *fakeAgentSwarm, dialer *agentclient.Dialer, authority *ca.Authority, bootstrapToken string) (http.Handler, *Handler) {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool, fake, dialer, authority, bootstrapToken)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/nodes", h.ListNodes)
		gr.Get("/api/v1/services", h.ListServices)
		gr.Get("/api/v1/nodes/{id}/metrics", h.GetNodeMetrics)
		gr.Get("/api/v1/nodes/{id}/packages", h.GetNodePackages)
		gr.Post("/api/v1/nodes/{id}/packages/check", h.TriggerPackageCheck)
		gr.Post("/api/v1/nodes/{id}/maintain", h.TriggerNodeMaintenance)
		gr.Get("/api/v1/cluster/resources", h.GetClusterResources)
		gr.Post("/internal/agent/register", h.RegisterAgent)
		gr.Get("/api/v1/ws/terminal/{containerID}", h.WsTerminal)
		gr.Get("/api/v1/ws/logs/{containerID}", h.WsLogs)
	})
	// Settings subresources live outside the authed group in production tests
	// only for direct-handler coverage; expose them here with the middleware.
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/settings/servers", h.ListServers)
		gr.Post("/api/v1/settings/servers", h.CreateServer)
		gr.Get("/api/v1/settings/cluster", h.ClusterInfo)
	})
	return r, h
}

func doJSON(t *testing.T, router http.Handler, method, path string, headers http.Header, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestListServersEmptyPopulatedAndCreate(t *testing.T) {
	fake := &fakeAgentSwarm{}
	router, _ := newAgentsRouter(t, fake, nil, nil, "")
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/settings/servers", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("empty servers status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/api/v1/settings/servers", org.Headers,
		`{"name":"worker-1","host":"10.0.0.5","description":"edge node"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("decode id: %v %s", err, rec.Body.String())
	}

	// Default SSH port applied and request event recorded.
	var sshPort int
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select ssh_port from servers where id=$1::uuid`, resp.ID).Scan(&sshPort); err != nil {
		t.Fatalf("server row missing: %v", err)
	}
	if sshPort != 22 {
		t.Fatalf("ssh_port = %d, want default 22", sshPort)
	}
	if n := testdb.QueryCount(t, `select count(*) from request_events where category='server'`); n != 1 {
		t.Fatalf("request_events rows = %d, want 1", n)
	}

	rec = doJSON(t, router, http.MethodGet, "/api/v1/settings/servers", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"worker-1"`) {
		t.Fatalf("list after create status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateServerValidationAndFailures(t *testing.T) {
	fake := &fakeAgentSwarm{}
	router, _ := newAgentsRouter(t, fake, nil, nil, "")
	org := testdb.SeedOrg(t)

	cases := []struct {
		name     string
		body     string
		wantCode int
		wantMsg  string
	}{
		{"malformed json", `{broken`, http.StatusBadRequest, "invalid payload"},
		{"missing host", `{"name":"only-name"}`, http.StatusBadRequest, "required"},
		{"missing name", `{"host":"h"}`, http.StatusBadRequest, "required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPost, "/api/v1/settings/servers", org.Headers, tc.body)
			if rec.Code != tc.wantCode || !strings.Contains(rec.Body.String(), tc.wantMsg) {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	// Insert failure surfaces as 400 (closed pool).
	h := NewHandler(deadAgentsPool(t), fake, nil, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/servers", strings.NewReader(`{"name":"x","host":"y"}`))
	req.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.CreateServer(rec2, req)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("insert failure status = %d, want 400", rec2.Code)
	}
}

func TestClusterInfoCountsNodesAndServices(t *testing.T) {
	fake := &fakeAgentSwarm{
		nodes:    []dockerswarm.Node{agentNode("n1", "host-1", "ready"), agentNode("n2", "host-2", "ready")},
		services: []dockerswarm.Service{agentService("s1", "api"), agentService("s2", "web")},
	}
	router, _ := newAgentsRouter(t, fake, nil, nil, "")
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/settings/cluster", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		NodeCount    int               `json:"nodeCount"`
		ServiceCount int               `json:"serviceCount"`
		Nodes        []json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NodeCount != 2 || resp.ServiceCount != 2 || len(resp.Nodes) != 2 {
		t.Fatalf("cluster info = %s", rec.Body.String())
	}

	// Swarm failure → 500.
	fake.listNodeErr = errors.New("swarm down")
	rec = doJSON(t, router, http.MethodGet, "/api/v1/settings/cluster", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("error status = %d, want 500", rec.Code)
	}
}

func TestListNodesAndServicesPassthrough(t *testing.T) {
	fake := &fakeAgentSwarm{
		nodes: []dockerswarm.Node{
			agentNode("n1", "alpha", "ready"),
			agentNode("n2", "beta", "down"),
		},
		services: []dockerswarm.Service{agentService("s1", "api")},
	}
	router, _ := newAgentsRouter(t, fake, nil, nil, "")
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/nodes", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("nodes status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID       string `json:"id"`
			Hostname string `json:"hostname"`
			Status   string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 || resp.Items[0].ID != "n1" || resp.Items[0].Hostname != "alpha" || resp.Items[0].Status != "ready" {
		t.Fatalf("items = %s", rec.Body.String())
	}
	if resp.Items[1].Status != "down" {
		t.Fatalf("second node = %+v", resp.Items[1])
	}

	rec = doJSON(t, router, http.MethodGet, "/api/v1/services", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"api"`) {
		t.Fatalf("services status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Swarm failures map to 500.
	fake.listNodeErr = errors.New("nodes down")
	rec = doJSON(t, router, http.MethodGet, "/api/v1/nodes", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("node error status = %d, want 500", rec.Code)
	}
	fake.listNodeErr = nil
	fake.listSvcErr = errors.New("services down")
	rec = doJSON(t, router, http.MethodGet, "/api/v1/services", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("service error status = %d, want 500", rec.Code)
	}
}

func TestAgentRPCWithoutDialerReturns502(t *testing.T) {
	fake := &fakeAgentSwarm{taskIPErr: errors.New("no agent task")}
	router, _ := newAgentsRouter(t, fake, nil, nil, "")
	org := testdb.SeedOrg(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/nodes/n1/metrics"},
		{http.MethodGet, "/api/v1/nodes/n1/packages"},
		{http.MethodPost, "/api/v1/nodes/n1/packages/check"},
		{http.MethodPost, "/api/v1/nodes/n1/maintain"},
	} {
		rec := doJSON(t, router, tc.method, tc.path, org.Headers, `{}`)
		if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "agent_unavailable") {
			t.Fatalf("%s %s status = %d body=%s, want 502 agent_unavailable", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestAgentRPCHappyPathAgainstFakeAgent(t *testing.T) {
	fake := &fakeAgentSwarm{}
	agentClient := startFakeAgent(t, &fakeAgent{
		metrics: &agentv1.HostMetricsResponse{
			CpuCores:        []*agentv1.CpuCoreUsage{{Core: 0}, {Core: 1}, {Core: 2}, {Core: 3}},
			CpuTotalPercent: 42.5,
			MemoryTotal:     1000,
			MemoryUsed:      400,
			Filesystems: []*agentv1.FilesystemInfo{{
				MountPoint: "/",
				TotalBytes: 5000,
				UsedBytes:  2500,
			}},
		},
		pkgStatus: &agentv1.PackageStatusResponse{RebootRequired: false, UpgradableCount: 3},
	})
	dialer, err := agentclient.NewDialer(nil, tls.Certificate{}, false)
	if err != nil {
		t.Fatalf("new dialer: %v", err)
	}
	router, h := newAgentsRouter(t, fake, dialer, nil, "")
	h.agentClientOverride = agentClient
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/nodes/n1/metrics", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"cpu_total_percent":42.5`) {
		t.Fatalf("metrics status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/api/v1/nodes/n1/packages", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"upgradable_count":3`) {
		t.Fatalf("packages status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/api/v1/nodes/n1/packages/check", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "PACKAGE_UPDATE_CHECK") {
		t.Fatalf("package check status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetClusterResourcesAggregatesMetrics(t *testing.T) {
	fake := &fakeAgentSwarm{
		nodes: []dockerswarm.Node{agentNode("n1", "ready-node", "ready"), agentNode("n2", "down-node", "down")},
	}
	agentClient := startFakeAgent(t, &fakeAgent{
		metrics: &agentv1.HostMetricsResponse{
			CpuCores:        []*agentv1.CpuCoreUsage{{Core: 0}, {Core: 1}},
			CpuTotalPercent: 10,
			MemoryTotal:     100,
			MemoryUsed:      40,
			Filesystems: []*agentv1.FilesystemInfo{
				{MountPoint: "/data", TotalBytes: 9999, UsedBytes: 8888},
				{MountPoint: "/", TotalBytes: 5000, UsedBytes: 1000},
			},
		},
	})
	dialer, err := agentclient.NewDialer(nil, tls.Certificate{}, false)
	if err != nil {
		t.Fatalf("new dialer: %v", err)
	}
	router, h := newAgentsRouter(t, fake, dialer, nil, "")
	h.agentClientOverride = agentClient
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/cluster/resources", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Nodes []struct {
			NodeID   string `json:"node_id"`
			CPUCores int32  `json:"cpu_cores"`
			MemTotal uint64 `json:"memory_total"`
			DiskUsed uint64 `json:"disk_used"`
		} `json:"nodes"`
		Cluster struct {
			TotalCPUCores int32  `json:"total_cpu_cores"`
			TotalMemory   uint64 `json:"total_memory_bytes"`
			UsedMemory    uint64 `json:"used_memory_bytes"`
			TotalDisk     uint64 `json:"total_disk_bytes"`
			UsedDisk      uint64 `json:"used_disk_bytes"`
			NodeCount     int    `json:"node_count"`
		} `json:"cluster"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 2 || resp.Nodes[0].NodeID != "n1" || resp.Nodes[0].CPUCores != 2 {
		t.Fatalf("nodes = %s", rec.Body.String())
	}
	// The down node contributes zeros.
	if resp.Nodes[1].NodeID != "n2" || resp.Nodes[1].CPUCores != 0 {
		t.Fatalf("down node = %+v", resp.Nodes[1])
	}
	c := resp.Cluster
	if c.TotalCPUCores != 2 || c.TotalMemory != 100 || c.UsedMemory != 40 ||
		c.TotalDisk != 5000 || c.UsedDisk != 1000 || c.NodeCount != 2 {
		t.Fatalf("cluster totals = %+v", c)
	}

	// Swarm failure → 500.
	fake.listNodeErr = errors.New("swarm down")
	rec = doJSON(t, router, http.MethodGet, "/api/v1/cluster/resources", org.Headers, "")
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "swarm_error") {
		t.Fatalf("error status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTriggerNodeMaintenance(t *testing.T) {
	fake := &fakeAgentSwarm{}
	exec := newRecordedExec()
	agent := &fakeAgent{
		pkgStatus: &agentv1.PackageStatusResponse{RebootRequired: true},
		exec:      exec,
	}
	agentClient := startFakeAgent(t, agent)
	dialer, err := agentclient.NewDialer(nil, tls.Certificate{}, false)
	if err != nil {
		t.Fatalf("new dialer: %v", err)
	}
	router, h := newAgentsRouter(t, fake, dialer, nil, "")
	h.agentClientOverride = agentClient
	org := testdb.SeedOrg(t)

	// Malformed JSON → 400.
	rec := doJSON(t, router, http.MethodPost, "/api/v1/nodes/n1/maintain", org.Headers, `{broken`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d body=%s", rec.Code, rec.Body.String())
	}

	body := `{"operations":["security_updates","all_updates","update_check","bogus_op"],"reboot_if_needed":true}`
	rec = doJSON(t, router, http.MethodPost, "/api/v1/nodes/n1/maintain", org.Headers, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("maintain status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Three known operations + scheduled reboot; unknown op skipped.
	if len(resp.Results) != 4 {
		t.Fatalf("results = %s", rec.Body.String())
	}
	last := resp.Results[3]
	if last["operation"] != "reboot" {
		t.Fatalf("last result = %v, want reboot", last)
	}

	// HostExec failures surface per-operation errors.
	agent.execErr = errors.New("exec down")
	rec = doJSON(t, router, http.MethodPost, "/api/v1/nodes/n1/maintain", org.Headers,
		`{"operations":["update_check"],"reboot_if_needed":false}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "exec down") {
		t.Fatalf("exec failure status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegisterAgent(t *testing.T) {
	fake := &fakeAgentSwarm{}
	authority, err := ca.LoadOrCreate(context.Background(), nil)
	if err != nil {
		t.Fatalf("load authority: %v", err)
	}
	router, _ := newAgentsRouter(t, fake, nil, authority, "boot-token")
	org := testdb.SeedOrg(t)

	// Malformed JSON.
	rec := doJSON(t, router, http.MethodPost, "/internal/agent/register", org.Headers, `{broken`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d", rec.Code)
	}

	// Wrong bootstrap token → 401.
	rec = doJSON(t, router, http.MethodPost, "/internal/agent/register", org.Headers,
		`{"nodeId":"n1","bootstrapToken":"wrong","csr":""}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d body=%s, want 401", rec.Code, rec.Body.String())
	}

	// Empty CSR → 400.
	rec = doJSON(t, router, http.MethodPost, "/internal/agent/register", org.Headers,
		`{"nodeId":"n1","bootstrapToken":"boot-token"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid csr") {
		t.Fatalf("empty csr status = %d body=%s", rec.Code, rec.Body.String())
	}

	// PEM block that is not a CSR → 400.
	garbage := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: []byte("not really")})
	rec = doJSON(t, router, http.MethodPost, "/internal/agent/register", org.Headers, fmt.Sprintf(
		`{"nodeId":"n1","bootstrapToken":"boot-token","csr":%q}`, string(garbage)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("garbage csr status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Real CSR → signed certificate.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	csrTemplate := &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: "agent-n1"},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	rec = doJSON(t, router, http.MethodPost, "/internal/agent/register", org.Headers, fmt.Sprintf(
		`{"nodeId":"n1","bootstrapToken":"boot-token","csr":%q}`, string(csrPEM)))
	if rec.Code != http.StatusOK {
		t.Fatalf("happy path status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Cert   string `json:"cert"`
		CACert string `json:"caCert"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp.Cert, "BEGIN CERTIFICATE") || !strings.Contains(resp.CACert, "BEGIN CERTIFICATE") {
		t.Fatalf("cert response missing PEM: cert=%q ca=%q", resp.Cert, resp.CACert)
	}
	block, _ := pem.Decode([]byte(resp.Cert))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse signed cert: %v", err)
	}
	if cert.Subject.CommonName != "agent-n1" {
		t.Fatalf("cert CN = %q, want agent-n1", cert.Subject.CommonName)
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:       poolWithCert(authority.CertPEM()),
		CurrentTime: time.Now(),
	}); err != nil {
		t.Fatalf("signed cert does not verify against CA: %v", err)
	}
}

func poolWithCert(caPEM []byte) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	return pool
}

func dialWS(t *testing.T, rawURL string, headers http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	dialer := &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	return dialer.Dial(rawURL, headers)
}

func TestWsTerminalContainerNotFoundAndNoDialer(t *testing.T) {
	fake := &fakeAgentSwarm{tasks: []dockerswarm.Task{containerTask("t1", "n1", "abcdef123")}}
	router, _ := newAgentsRouter(t, fake, nil, nil, "")
	org := testdb.SeedOrg(t)
	wsBase := wsHeaders(org)

	// Unknown container → 404 before any upgrade.
	rec := doJSON(t, router, http.MethodGet, "/api/v1/ws/terminal/doesnotexist", wsBase, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown container status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}

	// Known container but no dialer configured → 502.
	rec = doJSON(t, router, http.MethodGet, "/api/v1/ws/terminal/abcdef123", wsBase, "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "agent_unavailable") {
		t.Fatalf("no dialer status = %d body=%s, want 502", rec.Code, rec.Body.String())
	}

	// Task listing failure → 404.
	fake.listTaskErr = errors.New("tasks down")
	rec = doJSON(t, router, http.MethodGet, "/api/v1/ws/terminal/abcdef123", wsBase, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tasks failure status = %d, want 404", rec.Code)
	}
}

// wsHeaders returns auth headers for websocket handshake requests.
func wsHeaders(org *testdb.OrgFixture) http.Header {
	h := http.Header{}
	for k, vs := range org.Headers {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	return h
}

func TestWsTerminalBridgesExecStream(t *testing.T) {
	fake := &fakeAgentSwarm{tasks: []dockerswarm.Task{containerTask("t1", "n1", "abcdef123")}}
	exec := newRecordedExec()
	agentClient := startFakeAgent(t, &fakeAgent{exec: exec})
	dialer, err := agentclient.NewDialer(nil, tls.Certificate{}, false)
	if err != nil {
		t.Fatalf("new dialer: %v", err)
	}
	router, h := newAgentsRouter(t, fake, dialer, nil, "")
	h.agentClientOverride = agentClient
	org := testdb.SeedOrg(t)

	ts := httptest.NewServer(router)
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/terminal/abcdef123?shell=/bin/bash&rows=40&cols=120"
	conn, resp, err := dialWS(t, url, wsHeaders(org))
	if err != nil {
		t.Fatalf("ws dial: %v (status %d)", err, resp.StatusCode)
	}
	defer func() { _ = conn.Close() }()

	// Agent stdout/stderr arrive as binary messages.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for _, want := range []string{"hello", "oops"} {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read %s: %v", want, err)
		}
		if string(data) != want {
			t.Fatalf("frame = %q, want %q", data, want)
		}
	}

	// Resize is interpreted as a control message; raw input is forwarded.
	if err := conn.WriteJSON(map[string]any{"type": "resize", "rows": 50, "cols": 200}); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("ls\n")); err != nil {
		t.Fatalf("send stdin: %v", err)
	}

	// The fake agent ends the session after the first stdin frame; the exit
	// code arrives as a JSON text frame.
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read exit code: %v", err)
	}
	if !strings.Contains(string(data), `"exitCode":0`) {
		t.Fatalf("exit frame = %s", data)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(exec.resizes) > 0 && len(exec.stdins) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(exec.resizes) != 1 || exec.resizes[0].Rows != 50 || exec.resizes[0].Cols != 200 {
		t.Fatalf("resizes = %+v", exec.resizes)
	}
	if len(exec.stdins) != 1 || string(exec.stdins[0]) != "ls\n" {
		t.Fatalf("stdins = %q", exec.stdins)
	}
	if exec.started == nil || exec.started.Command == nil || exec.started.Command[0] != "/bin/bash" ||
		exec.started.Rows != 40 || exec.started.Cols != 120 || !exec.started.Tty {
		t.Fatalf("start = %+v", exec.started)
	}

	// Client hang-up closes the stream cleanly.
	_ = conn.Close() //nolint:errcheck
	time.Sleep(100 * time.Millisecond)
}

func TestWsLogsBridgesChunksAndReportsErrors(t *testing.T) {
	fake := &fakeAgentSwarm{tasks: []dockerswarm.Task{containerTask("t1", "n1", "log123456")}}
	agentClient := startFakeAgent(t, &fakeAgent{logs: []*agentv1.LogChunk{
		{Data: []byte("line one\n")},
		{Data: []byte("line two\n")},
	}})
	dialer, err := agentclient.NewDialer(nil, tls.Certificate{}, false)
	if err != nil {
		t.Fatalf("new dialer: %v", err)
	}
	router, h := newAgentsRouter(t, fake, dialer, nil, "")
	h.agentClientOverride = agentClient
	org := testdb.SeedOrg(t)

	ts := httptest.NewServer(router)
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/logs/log123456?follow=true&tail=50"
	conn, resp, err := dialWS(t, url, wsHeaders(org))
	if err != nil {
		t.Fatalf("ws dial: %v (status %d)", err, resp.StatusCode)
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for _, want := range []string{"line one\n", "line two\n"} {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read log chunk: %v", err)
		}
		if string(data) != want {
			t.Fatalf("chunk = %q, want %q", data, want)
		}
	}

	// Unknown container and no-dialer branches.
	rec := doJSON(t, router, http.MethodGet, "/api/v1/ws/logs/doesnotexist", wsHeaders(org), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown container logs status = %d, want 404", rec.Code)
	}
}

func TestWsLogsNoDialerReturns502(t *testing.T) {
	fake := &fakeAgentSwarm{tasks: []dockerswarm.Task{containerTask("t1", "n1", "nod123456")}}
	router, _ := newAgentsRouter(t, fake, nil, nil, "")
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/ws/logs/nod123456", wsHeaders(org), "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

// deadAgentsPool returns a closed pool so any query fails immediately.
func deadAgentsPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(testdb.Get(t).Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatalf("parse conn string: %v", err)
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	p.Close()
	return p
}

func TestListServersQueryFailureReturns500(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(deadAgentsPool(t), &fakeAgentSwarm{}, nil, nil, "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/servers", nil)
	rec := httptest.NewRecorder()
	h.ListServers(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// TestAgentRPCAgainstUnreachableAgent drives the real dialer path: the swarm
// seam resolves an agent IP, but nothing listens on the agent port, so every
// RPC surfaces as a 502 agent_error.
func TestAgentRPCAgainstUnreachableAgent(t *testing.T) {
	fake := &fakeAgentSwarm{taskIP: "127.0.0.1"}
	dialer, err := agentclient.NewDialer(nil, tls.Certificate{}, false)
	if err != nil {
		t.Fatalf("new dialer: %v", err)
	}
	router, _ := newAgentsRouter(t, fake, dialer, nil, "")
	org := testdb.SeedOrg(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/nodes/n1/metrics"},
		{http.MethodGet, "/api/v1/nodes/n1/packages"},
		{http.MethodPost, "/api/v1/nodes/n1/packages/check"},
	} {
		rec := doJSON(t, router, tc.method, tc.path, org.Headers, "")
		if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "agent_error") {
			t.Fatalf("%s %s status = %d body=%s, want 502 agent_error", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	rec := doJSON(t, router, http.MethodPost, "/api/v1/nodes/n1/maintain", org.Headers,
		`{"operations":["update_check"],"reboot_if_needed":false}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "agent_error") &&
		!strings.Contains(rec.Body.String(), "error") {
		t.Fatalf("maintain against dead agent status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Results) != 1 {
		t.Fatalf("results = %s", rec.Body.String())
	}
	if _, has := resp.Results[0]["error"]; !has {
		t.Fatalf("expected error entry in results: %s", rec.Body.String())
	}
}

func TestRegisterAgentRejectsTamperedSignature(t *testing.T) {
	fake := &fakeAgentSwarm{}
	authority, err := ca.LoadOrCreate(context.Background(), nil)
	if err != nil {
		t.Fatalf("load authority: %v", err)
	}
	router, _ := newAgentsRouter(t, fake, nil, authority, "")
	org := testdb.SeedOrg(t)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "agent-n1"},
	}, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	// Corrupt the final signature byte so CheckSignature fails downstream.
	tampered := append([]byte(nil), csrDER...)
	tampered[len(tampered)-1] ^= 0xFF
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: tampered})

	rec := doJSON(t, router, http.MethodPost, "/internal/agent/register", org.Headers, fmt.Sprintf(
		`{"nodeId":"n1","csr":%q}`, string(csrPEM)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("tampered csr status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}
