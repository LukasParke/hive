//go:build integration

package integration

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"

	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
	"github.com/luke/hive/proto/gen/agent/v1/agentv1connect"
)

// agentAddr returns the agent address from the environment, defaulting to localhost:9090.
func agentAddr() string {
	if addr := os.Getenv("AGENT_ADDR"); addr != "" {
		return addr
	}
	return "http://127.0.0.1:9090"
}

func TestAgentHealthRPC(t *testing.T) {
	addr := agentAddr()
	client := agentv1connect.NewAgentServiceClient(
		http.DefaultClient,
		addr,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test Health RPC
	resp, err := client.Health(ctx, connect.NewRequest(&agentv1.HealthRequest{}))
	if err != nil {
		t.Fatalf("Health RPC failed (agent at %s): %v", addr, err)
	}
	if resp.Msg.AgentVersion == "" {
		t.Error("expected non-empty agent version")
	}
	t.Logf("Health: version=%s docker=%s node=%s", resp.Msg.AgentVersion, resp.Msg.DockerVersion, resp.Msg.NodeId)

	// Test GetHostMetrics RPC
	metricsResp, err := client.GetHostMetrics(ctx, connect.NewRequest(&agentv1.HostMetricsRequest{}))
	if err != nil {
		t.Logf("GetHostMetrics may fail without /proc access: %v", err)
	} else {
		t.Logf("HostMetrics: hostname=%s cpuCores=%d memTotal=%d",
			metricsResp.Msg.Hostname, len(metricsResp.Msg.CpuCores), metricsResp.Msg.MemoryTotal)
	}
}
