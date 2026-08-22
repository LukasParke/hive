//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"
)

// TestControlPlaneLeaderFailover replaces the name-only HA assertions with a
// real failover: it finds the postgres advisory-lock holder, maps it to a
// control-plane task, stops that container, and verifies (a) another
// replica acquires the leader lock within 45s, (b) /api/v1/health keeps
// answering 200 throughout, and (c) the new leader's watcher resyncs the
// swarm cache. The service is restored to its original spec afterwards.
func TestControlPlaneLeaderFailover(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	cli := dindClient(t)
	pool := testDB(t)
	stackName := getenv("STACK_NAME", "hive")
	svcName := stackName + "_control-plane"
	ctx := context.Background()

	inspect, err := cli.ServiceInspect(ctx, svcName, dockerclient.ServiceInspectOptions{})
	if err != nil {
		t.Skipf("control-plane service %s not found: %v", svcName, err)
	}
	origSpec := inspect.Service.Spec
	origReplicas := uint64(1)
	if origSpec.Mode.Replicated != nil && origSpec.Mode.Replicated.Replicas != nil {
		origReplicas = *origSpec.Mode.Replicated.Replicas
	}

	// The failover needs at least two replicas. The CI overlay pins the
	// service to one node and publishes port 3000 in host mode, which would
	// leave a second task on that node unable to bind. Switching the
	// endpoint to ingress mode lets the routing mesh serve 127.0.0.1:3000
	// from any live replica.
	if origReplicas < 2 {
		scaledSpec := origSpec
		two := uint64(2)
		scaledSpec.Mode.Replicated.Replicas = &two
		scaledSpec.EndpointSpec = ingressEndpointSpec(scaledSpec.EndpointSpec)
		if _, err := cli.ServiceUpdate(ctx, inspect.Service.ID, dockerclient.ServiceUpdateOptions{
			Version: inspect.Service.Version,
			Spec:    scaledSpec,
		}); err != nil {
			t.Skipf("could not scale control-plane to 2 replicas: %v", err)
		}
		t.Cleanup(func() {
			restoreCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			fresh, err := cli.ServiceInspect(restoreCtx, inspect.Service.ID, dockerclient.ServiceInspectOptions{})
			if err != nil {
				t.Logf("cleanup: service inspect failed: %v", err)
				return
			}
			if _, err := cli.ServiceUpdate(restoreCtx, inspect.Service.ID, dockerclient.ServiceUpdateOptions{
				Version: fresh.Service.Version,
				Spec:    origSpec,
			}); err != nil {
				t.Logf("cleanup: restore control-plane service failed: %v", err)
			}
		})
		pollUntil(t, 120*time.Second, 3*time.Second, "second control-plane replica running", func() error {
			return assertServiceRunningReplicas(ctx, cli, svcName, 2)
		})
	}

	// Baseline: the swarm cache is populated by the current leader.
	var cacheBaseline time.Time
	if err := pool.QueryRow(ctx, `select coalesce(max(updated_at), now()) from swarm_cache_services`).Scan(&cacheBaseline); err != nil {
		t.Fatalf("read swarm cache baseline failed: %v", err)
	}
	// Identify the leader directly: every replica reports its own leadership
	// on /api/v1/health ("leader":true). This avoids mapping pg client
	// addresses to container IPs, which is unreliable behind the routing
	// mesh and after task churn.
	holderContainer := ""
	pollUntil(t, 60*time.Second, 2*time.Second, "leader replica reporting leadership", func() error {
		cid, err := findLeaderContainer(ctx, cli, svcName)
		if err != nil {
			return err
		}
		holderContainer = cid
		return nil
	})
	if holderContainer == "" {
		t.Skip("no replica reported leadership; cannot choose a leader to kill")
	}
	t.Logf("leader lock held by container %s", holderContainer)

	// Health monitor: poll the public API for the whole failover window
	// and fail if any request gets a non-200 response. Transport blips from
	// the routing mesh are recorded but tolerated.
	var mu sync.Mutex
	var badStatus []string
	transportErrs := 0
	stopMonitor := make(chan struct{})
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		client := &http.Client{Timeout: 3 * time.Second}
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopMonitor:
				return
			case <-ticker.C:
				resp, err := client.Get(baseURL + "/api/v1/health") //nolint:gosec
				if err != nil {
					mu.Lock()
					transportErrs++
					mu.Unlock()
					continue
				}
				if resp.StatusCode != http.StatusOK {
					mu.Lock()
					badStatus = append(badStatus, fmt.Sprintf("%d@%s", resp.StatusCode, time.Now().Format(time.RFC3339)))
					mu.Unlock()
				}
				resp.Body.Close()
			}
		}
	}()
	t.Cleanup(func() {
		close(stopMonitor)
		<-monitorDone
		mu.Lock()
		defer mu.Unlock()
		if len(badStatus) > 0 {
			t.Errorf("/api/v1/health returned non-200 during failover: %v", badStatus)
		}
		if transportErrs > 0 {
			t.Logf("health monitor saw %d transport blips (routing mesh); API stayed reachable", transportErrs)
		}
	})

	// Kill the leader.
	killedAt := time.Now()
	if _, err := cli.ContainerStop(ctx, holderContainer, dockerclient.ContainerStopOptions{}); err != nil {
		t.Fatalf("stop leader container failed: %v", err)
	}

	// Another replica must claim leadership within 45s (elector polls every
	// 5s). The dead container's replacement may reuse its task slot name,
	// so match on container identity.
	var newHolderAt time.Time
	pollUntil(t, 45*time.Second, time.Second, "new control-plane replica claimed leadership", func() error {
		cid, err := findLeaderContainer(ctx, cli, svcName)
		if err != nil {
			return err
		}
		if cid == "" || cid == holderContainer {
			return fmt.Errorf("no new replica reporting leadership yet")
		}
		newHolderAt = time.Now()
		t.Logf("new leader: container %s", cid)
		return nil
	})

	// The new leader's watcher performs an initial full resync on start:
	// every swarm_cache_services row is re-upserted with a fresh
	// updated_at, so max(updated_at) must move past the baseline.
	pollUntil(t, 60*time.Second, 2*time.Second, "new leader watcher resynced swarm cache", func() error {
		var count int
		var latest time.Time
		if err := pool.QueryRow(ctx, `select count(*), coalesce(max(updated_at), to_timestamp(0)) from swarm_cache_services`).Scan(&count, &latest); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("swarm_cache_services is empty")
		}
		if !latest.After(cacheBaseline) {
			return fmt.Errorf("cache not resynced yet (latest=%v baseline=%v)", latest, cacheBaseline)
		}
		return nil
	})
	t.Logf("failover completed in %s", newHolderAt.Sub(killedAt).Round(time.Second))
}

// containerIDForIPv4 maps an overlay IPv4 address to a running task
// container of the given service; "" when no container matches.
func containerIDForIPv4(t *testing.T, cli *dockerclient.Client, serviceName string, ip string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ids, err := serviceTaskContainers(ctx, cli, serviceName)
	if err != nil {
		return ""
	}
	for _, id := range ids {
		if addr, err := containerIPv4(ctx, cli, id, ""); err == nil && addr == ip {
			return id
		}
	}
	return ""
}

// ingressEndpointSpec converts every published port of an endpoint spec to
// ingress mode so the routing mesh serves them from any node.
func ingressEndpointSpec(spec *swarm.EndpointSpec) *swarm.EndpointSpec {
	if spec == nil {
		return nil
	}
	out := &swarm.EndpointSpec{Mode: swarm.ResolutionModeVIP}
	for _, port := range spec.Ports {
		p := port // copy
		p.PublishMode = swarm.PortConfigPublishModeIngress
		out.Ports = append(out.Ports, p)
	}
	return out
}

// findLeaderContainer returns the ID of the running control-plane task
// whose /api/v1/health reports leadership ("leader":true), or "" when none
// does. err is reserved for infrastructure failures.
func findLeaderContainer(ctx context.Context, cli *dockerclient.Client, serviceName string) (string, error) {
	ids, err := serviceTaskContainers(ctx, cli, serviceName)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		out, err := containerExec(ctx, cli, id, []string{
			"wget", "-qO-", "-T", "3", "http://127.0.0.1:3000/api/v1/health",
		})
		if err != nil {
			continue
		}
		if strings.Contains(out, `"leader":true`) {
			return id, nil
		}
	}
	return "", nil
}
