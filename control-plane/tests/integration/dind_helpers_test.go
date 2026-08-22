//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"
)

// dindClient builds a docker client pointed at the dind manager socket
// (HIVE_DOCKER_HOST), matching the pattern used by the existing tests.
func dindClient(t *testing.T) *dockerclient.Client {
	t.Helper()
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(getenv("HIVE_DOCKER_HOST", "tcp://127.0.0.1:2375")),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("docker client init failed: %v", err)
	}
	return cli
}

// testDB opens a pgx pool against the CI database.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Direct Postgres, not PgBouncer: advisory-lock introspection and
	// session-level reads need a real session connection.
	pool, err := pgxpool.New(ctx, getenv("HIVE_DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5432/hive?sslmode=disable"))
	if err != nil {
		t.Fatalf("create pgx pool failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// pollUntil retries fn on a deadline: fn returns a non-nil error while the
// awaited condition does not hold yet. It fails the test with desc when the
// deadline passes. No sleep-only waits: every wait is bounded and re-checks.
func pollUntil(t *testing.T, timeout, interval time.Duration, desc string, fn func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		lastErr = fn()
		if lastErr == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: timed out after %s: last error: %v", desc, timeout, lastErr)
		}
		time.Sleep(interval)
	}
}

// serviceTaskContainers lists the running task containers of a swarm service
// by its fully qualified name (e.g. hive_control-plane).
func serviceTaskContainers(ctx context.Context, cli *dockerclient.Client, serviceName string) ([]string, error) {
	result, err := cli.ContainerList(ctx, dockerclient.ContainerListOptions{
		Filters: dockerclient.Filters{}.Add("label", "com.docker.swarm.service.name="+serviceName),
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(result.Items))
	for _, c := range result.Items {
		ids = append(ids, c.ID)
	}
	return ids, nil
}

// containerIPv4 returns the container's IPv4 address on the given network.
func containerIPv4(ctx context.Context, cli *dockerclient.Client, containerID, networkName string) (string, error) {
	inspect, err := cli.ContainerInspect(ctx, containerID, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return "", err
	}
	if inspect.Container.NetworkSettings == nil {
		return "", fmt.Errorf("container %s has no network settings", containerID)
	}
	for name, ep := range inspect.Container.NetworkSettings.Networks {
		if networkName == "" || strings.EqualFold(name, networkName) {
			if ep.IPAddress.IsValid() {
				return ep.IPAddress.String(), nil
			}
		}
	}
	return "", fmt.Errorf("container %s has no IPv4 address on network %q", containerID, networkName)
}

// containerExec runs a command inside a task container and returns its
// combined output. A non-zero exit is returned as an error carrying the
// output so callers can surface remote failures.
func containerExec(ctx context.Context, cli *dockerclient.Client, containerID string, cmd []string) (string, error) {
	create, err := cli.ExecCreate(ctx, containerID, dockerclient.ExecCreateOptions{
		Cmd:          cmd,
		TTY:          true, // single multiplexed stream; no stdcopy demux needed
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("exec create %v: %w", cmd, err)
	}
	attach, err := cli.ExecAttach(ctx, create.ID, dockerclient.ExecAttachOptions{TTY: true})
	if err != nil {
		return "", fmt.Errorf("exec attach %v: %w", cmd, err)
	}
	defer attach.Close()
	out, _ := io.ReadAll(attach.Reader)

	deadline := time.Now().Add(60 * time.Second)
	for {
		inspect, err := cli.ExecInspect(ctx, create.ID, dockerclient.ExecInspectOptions{})
		if err == nil && !inspect.Running {
			output := strings.TrimSpace(string(out))
			if inspect.ExitCode != 0 {
				return output, fmt.Errorf("exec %v exited %d: %s", cmd, inspect.ExitCode, output)
			}
			return output, nil
		}
		if time.Now().After(deadline) {
			return string(out), fmt.Errorf("exec %v did not finish in time", cmd)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// scaleService re-inspects the service for a fresh version and sets the
// replica count, avoiding update-out-of-sequence errors.
func scaleService(ctx context.Context, cli *dockerclient.Client, serviceID string, replicas uint64) error {
	inspect, err := cli.ServiceInspect(ctx, serviceID, dockerclient.ServiceInspectOptions{})
	if err != nil {
		return err
	}
	spec := inspect.Service.Spec
	if spec.Mode.Replicated == nil {
		spec.Mode.Replicated = &swarm.ReplicatedService{}
	}
	spec.Mode.Replicated.Replicas = &replicas
	_, err = cli.ServiceUpdate(ctx, serviceID, dockerclient.ServiceUpdateOptions{
		Version: inspect.Service.Version,
		Spec:    spec,
	})
	return err
}

// assertServiceRunningReplicasMax returns nil when the service has at most
// max running tasks (used to await scale-down).
func assertServiceRunningReplicasMax(ctx context.Context, cli *dockerclient.Client, serviceName string, max int) error {
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
	if running > max {
		return fmt.Errorf("service %s still has %d running tasks, want <= %d", serviceName, running, max)
	}
	return nil
}
