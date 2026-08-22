//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"
)

// TestNodeDrain exercises the node lifecycle end to end: deploy a
// 3-replica stack, drain a non-critical node through the API, verify every
// task of the service is rescheduled off the drained node, then undrain and
// confirm the node is active again.
func TestNodeDrain(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	cli := dindClient(t)
	stackName := getenv("STACK_NAME", "hive")
	ctx := context.Background()

	// Pick a victim node that hosts nothing critical. Draining a node that
	// carries control-plane/postgres would evict the platform itself.
	nodes, err := cli.NodeList(ctx, dockerclient.NodeListOptions{})
	if err != nil {
		t.Fatalf("node list failed: %v", err)
	}
	criticalNodes := map[string]bool{}
	for _, svc := range []string{"control-plane", "postgres", "pgbouncer", "buildkit", "registry", "traefik", "agent"} {
		tasks, err := cli.TaskList(ctx, dockerclient.TaskListOptions{
			Filters: dockerclient.Filters{}.Add("service", stackName+"_"+svc),
		})
		if err != nil {
			// buildkit is no longer a stack service (host-level container
			// instead); tolerate missing services.
			if strings.Contains(err.Error(), "not found") {
				continue
			}
			t.Fatalf("task list for %s failed: %v", svc, err)
		}
		for _, task := range tasks.Items {
			if task.NodeID != "" {
				criticalNodes[task.NodeID] = true
			}
		}
	}

	var victim *swarm.Node
	for i := range nodes.Items {
		node := nodes.Items[i]
		if criticalNodes[node.ID] || node.Spec.Availability == swarm.NodeAvailabilityDrain {
			continue
		}
		if victim == nil || node.Spec.Role == swarm.NodeRoleWorker {
			victim = &nodes.Items[i]
			if node.Spec.Role == swarm.NodeRoleWorker {
				break // prefer a worker when one is available
			}
		}
	}
	if victim == nil {
		t.Skip("no drainable node: every active node hosts a critical hive service")
	}
	victimID := victim.ID
	t.Logf("draining node %s (%s)", victimID, victim.Description.Hostname)

	// Undrain no matter how the test exits; a left-behind drain poisons
	// every later scheduling test.
	t.Cleanup(func() {
		undrainNode(ctx, t, cli, victimID)
	})

	// Deploy a 3-replica stack BEFORE draining so the rescheduling has real
	// work to move.
	projectRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{"name": fmt.Sprintf("drain-project-%d", time.Now().UnixNano())}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := asString(projectRes["id"])

	composeContent := `services:
  sleeper:
    image: alpine:3.21
    command: ["sleep", "3600"]
    deploy:
      replicas: 3
`
	stackNameVar := fmt.Sprintf("drain-stack-%d", time.Now().UnixNano())
	stackRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/stacks", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{
		"projectId":      projectID,
		"name":           stackNameVar,
		"composeContent": composeContent,
	}, &stackRes, http.StatusCreated); err != nil {
		t.Fatalf("create stack failed: %v", err)
	}
	stackID := asString(stackRes["id"])
	t.Cleanup(func() {
		_, _ = authedDeleteWithHeaders(baseURL+"/api/v1/stacks/"+stackID, auth.AccessToken, map[string]string{
			"X-Organization-Id": auth.OrganizationID,
		})
	})

	deployRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/stacks/"+stackID+"/deploy", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{}, &deployRes, http.StatusAccepted); err != nil {
		t.Fatalf("deploy stack failed: %v", err)
	}

	svcName := stackNameVar + "_sleeper"

	pollUntil(t, 90*time.Second, 2*time.Second, "stack service reaching 3 running replicas", func() error {
		return assertServiceRunningReplicas(ctx, cli, svcName, 3)
	})

	// Drain via the API.
	if err := authedPostJSON(baseURL+"/api/v1/nodes/"+victimID+"/drain", auth.AccessToken, map[string]any{}, &map[string]any{}, http.StatusOK); err != nil {
		t.Fatalf("drain request failed: %v", err)
	}

	// Every running task must leave the drained node while the service
	// keeps its full replica count elsewhere.
	pollUntil(t, 120*time.Second, 2*time.Second, "tasks rescheduled off drained node", func() error {
		tasks, err := cli.TaskList(ctx, dockerclient.TaskListOptions{
			Filters: dockerclient.Filters{}.Add("service", svcName),
		})
		if err != nil {
			return err
		}
		onVictim := 0
		for _, task := range tasks.Items {
			if task.Status.State == swarm.TaskStateRunning && task.NodeID == victimID {
				onVictim++
			}
		}
		if onVictim > 0 {
			return fmt.Errorf("%d task(s) still running on drained node", onVictim)
		}
		return assertServiceRunningReplicas(ctx, cli, svcName, 3)
	})

	// Undrain and verify the node is active again.
	undrainNode(ctx, t, cli, victimID)
	pollUntil(t, 30*time.Second, 2*time.Second, "node availability back to active", func() error {
		inspect, err := cli.NodeInspect(ctx, victimID, dockerclient.NodeInspectOptions{})
		if err != nil {
			return err
		}
		if inspect.Node.Spec.Availability != swarm.NodeAvailabilityActive {
			return fmt.Errorf("availability=%q", inspect.Node.Spec.Availability)
		}
		return nil
	})
}

// undrainNode best-effort sets a node back to active availability.
func undrainNode(ctx context.Context, t *testing.T, cli *dockerclient.Client, nodeID string) {
	t.Helper()
	inspect, err := cli.NodeInspect(ctx, nodeID, dockerclient.NodeInspectOptions{})
	if err != nil {
		t.Logf("cleanup: inspect node %s failed: %v", nodeID, err)
		return
	}
	spec := inspect.Node.Spec
	if spec.Availability == swarm.NodeAvailabilityActive {
		return
	}
	spec.Availability = swarm.NodeAvailabilityActive
	if _, err := cli.NodeUpdate(ctx, nodeID, dockerclient.NodeUpdateOptions{
		Version: inspect.Node.Version,
		Spec:    spec,
	}); err != nil {
		t.Logf("cleanup: undrain node %s failed: %v", nodeID, err)
	}
}

// assertServiceRunningReplicas returns nil when the service currently has
// at least want running tasks.
func assertServiceRunningReplicas(ctx context.Context, cli *dockerclient.Client, serviceName string, want int) error {
	tasks, err := cli.TaskList(ctx, dockerclient.TaskListOptions{
		Filters: dockerclient.Filters{}.Add("service", serviceName),
	})
	if err != nil {
		return err
	}
	running := 0
	for _, task := range tasks.Items {
		if task.Status.State == swarm.TaskStateRunning {
			running++
		}
	}
	if running < want {
		return fmt.Errorf("service %s has %d running tasks, want %d", serviceName, running, want)
	}
	return nil
}
