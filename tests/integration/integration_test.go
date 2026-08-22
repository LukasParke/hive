//go:build integration
// +build integration

package integration

import (
	"os"
	"testing"
)

// TestIntegrationSuiteEntrypoint documents that the real integration suite
// lives in control-plane/tests/integration and runs from there in CI (see
// .github/workflows/integration-swarm.yml). The root module only keeps the
// agent RPC tests (agent_test.go). This test never runs real work; it skips
// unless someone explicitly points it at a dind manager, in which case it
// reminds the runner where the suite actually executes.
func TestIntegrationSuiteEntrypoint(t *testing.T) {
	if os.Getenv("DIND_MANAGER_HOST") == "" && os.Getenv("HIVE_DOCKER_HOST") == "" {
		t.Skip("placeholder entrypoint: the swarm integration suite runs from control-plane/tests/integration (set DIND_MANAGER_HOST to acknowledge a dind cluster)")
	}
	t.Log("Root tests/integration holds only agent RPC tests; run go test -tags integration ./... inside control-plane for the full suite.")
}
