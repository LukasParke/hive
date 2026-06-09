package server

import (
	"context"
	"io"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/system"
	dockerclient "github.com/moby/moby/client"

	"github.com/luke/hive/agent/internal/docker"
	"github.com/luke/hive/agent/internal/hostmetrics"
	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
)

// mockDocker implements docker.DockerOperations for testing.
type mockDocker struct {
	infoResult  system.Info
	infoErr     error
	statsResult []*docker.ContainerStat
	statsErr    error
}

func (m *mockDocker) Info(ctx context.Context) (system.Info, error) {
	return m.infoResult, m.infoErr
}

func (m *mockDocker) ContainerStats(ctx context.Context, ids []string) ([]*docker.ContainerStat, error) {
	return m.statsResult, m.statsErr
}

func (m *mockDocker) StreamLogs(ctx context.Context, id string, follow bool, tail int32, timestamps bool) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockDocker) ExecCreate(ctx context.Context, id string, cmd []string, tty bool) (string, error) {
	return "exec-123", nil
}

func (m *mockDocker) ExecAttach(ctx context.Context, execID string, tty bool) (dockerclient.HijackedResponse, error) {
	return dockerclient.HijackedResponse{}, nil
}

func (m *mockDocker) ExecResize(ctx context.Context, execID string, rows, cols uint) error {
	return nil
}

func (m *mockDocker) ExecInspect(ctx context.Context, execID string) (dockerclient.ExecInspectResult, error) {
	return dockerclient.ExecInspectResult{ExitCode: 0}, nil
}

func (m *mockDocker) ListContainers(ctx context.Context) ([]container.Summary, error) {
	return nil, nil
}

func (m *mockDocker) Close() error { return nil }

func TestHealth(t *testing.T) {
	mock := &mockDocker{
		infoResult: system.Info{
			ServerVersion: "24.0.0",
		},
	}

	collector := hostmetrics.NewCollector("", false)
	executor := hostmetrics.NewExecutor("", false)
	metrics := NewMetricsWithRegistry(prometheus.NewRegistry())
	srv := New(mock, collector, executor, metrics)

	resp, err := srv.Health(context.Background(), connect.NewRequest(&agentv1.HealthRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.DockerVersion != "24.0.0" {
		t.Errorf("expected docker version 24.0.0, got %s", resp.Msg.DockerVersion)
	}
	if resp.Msg.AgentVersion != "dev" {
		t.Errorf("expected agent version dev, got %s", resp.Msg.AgentVersion)
	}
	if resp.Msg.CpuCount <= 0 {
		t.Error("expected positive cpu count")
	}
}

func TestGetContainerStats(t *testing.T) {
	mock := &mockDocker{
		statsResult: []*docker.ContainerStat{
			{
				ContainerID: "abc123",
				Name:        "test-container",
				CPUPercent:  25.5,
				MemoryUsage: 1024 * 1024 * 100,
				MemoryLimit: 1024 * 1024 * 512,
			},
		},
	}

	collector := hostmetrics.NewCollector("", false)
	executor := hostmetrics.NewExecutor("", false)
	metrics := NewMetricsWithRegistry(prometheus.NewRegistry())
	srv := New(mock, collector, executor, metrics)

	resp, err := srv.GetContainerStats(context.Background(), connect.NewRequest(&agentv1.StatsRequest{
		ContainerIds: []string{"abc123"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Msg.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Msg.Items))
	}
	if resp.Msg.Items[0].CpuPercent != 25.5 {
		t.Errorf("expected 25.5%% CPU, got %f", resp.Msg.Items[0].CpuPercent)
	}
}

func TestHostExecDisabled(t *testing.T) {
	mock := &mockDocker{}
	collector := hostmetrics.NewCollector("", false) // host mgmt disabled
	executor := hostmetrics.NewExecutor("", false)
	metrics := NewMetricsWithRegistry(prometheus.NewRegistry())
	srv := New(mock, collector, executor, metrics)

	_, err := srv.HostExec(context.Background(), connect.NewRequest(&agentv1.HostOperationRequest{
		Operation: agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK,
	}))
	if err == nil {
		t.Fatal("expected error when host mgmt disabled")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("expected 'not enabled' error, got: %v", err)
	}
}
