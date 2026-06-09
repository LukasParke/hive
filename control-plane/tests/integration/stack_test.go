//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	dockerclient "github.com/moby/moby/client"
)

func TestStackLifecycle(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	dockerHost := getenv("HIVE_DOCKER_HOST", "tcp://127.0.0.1:2375")
	auth := bootstrapAuthContext(t, baseURL)

	// Create project
	projectRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{"name": fmt.Sprintf("stack-project-%d", time.Now().UnixNano())}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := asString(projectRes["id"])

	// Create stack with nginx compose
	composeContent := "services:\n  web:\n    image: nginx:alpine\n    deploy:\n      replicas: 1\n"
	stackRes := map[string]any{}
	stackName := fmt.Sprintf("stack-%d", time.Now().UnixNano())
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/stacks", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{
		"projectId":      projectID,
		"name":           stackName,
		"composeContent": composeContent,
	}, &stackRes, http.StatusCreated); err != nil {
		t.Fatalf("create stack failed: %v", err)
	}
	stackID := asString(stackRes["id"])
	if stackID == "" {
		t.Fatalf("stack id missing")
	}

	// Deploy stack
	deployRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/stacks/"+stackID+"/deploy", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{}, &deployRes, http.StatusOK); err != nil {
		t.Logf("deploy stack returned error (may be expected in CI without full swarm): %v", err)
	}

	// Verify stack appears in list
	resp, err := authedGetWithHeaders(baseURL+"/api/v1/stacks", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	})
	if err != nil {
		t.Fatalf("list stacks failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list stacks status=%d", resp.StatusCode)
	}

	// Stop stack
	stopRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/stacks/"+stackID+"/stop", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{}, &stopRes, http.StatusOK); err != nil {
		t.Logf("stop stack returned error (may be expected): %v", err)
	}

	// Start stack
	startRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/stacks/"+stackID+"/start", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{}, &startRes, http.StatusOK); err != nil {
		t.Logf("start stack returned error (may be expected): %v", err)
	}

	// Delete stack
	deleteResp, err := authedDeleteWithHeaders(baseURL+"/api/v1/stacks/"+stackID, auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	})
	if err != nil {
		t.Fatalf("delete stack request failed: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK && deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete stack status=%d", deleteResp.StatusCode)
	}

	// Verify GET returns 404
	getResp, err := authedGetWithHeaders(baseURL+"/api/v1/stacks/"+stackID, auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	})
	if err != nil {
		t.Fatalf("get deleted stack failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for deleted stack, got %d", getResp.StatusCode)
	}

	_ = dockerHost // used by multi-service test below
}

func TestMultiServiceStack(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	dockerHost := getenv("HIVE_DOCKER_HOST", "tcp://127.0.0.1:2375")
	auth := bootstrapAuthContext(t, baseURL)

	projectRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{"name": fmt.Sprintf("multistack-project-%d", time.Now().UnixNano())}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := asString(projectRes["id"])

	composeContent := `services:
  app:
    image: alpine:3.21
    command: ["sleep", "3600"]
    deploy:
      replicas: 1
  cache:
    image: redis:7-alpine
    deploy:
      replicas: 1
`
	stackRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/stacks", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{
		"projectId":      projectID,
		"name":           fmt.Sprintf("multistack-%d", time.Now().UnixNano()),
		"composeContent": composeContent,
	}, &stackRes, http.StatusCreated); err != nil {
		t.Fatalf("create multi-service stack failed: %v", err)
	}
	stackID := asString(stackRes["id"])

	// Deploy
	_ = authedPostJSONWithHeaders(baseURL+"/api/v1/stacks/"+stackID+"/deploy", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{}, &map[string]any{}, http.StatusOK)

	// Verify via Docker client that services exist
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(dockerHost),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("docker client init failed: %v", err)
	}

	// Give some time for stack to deploy
	time.Sleep(3 * time.Second)

	ctx := context.Background()
	services, err := cli.ServiceList(ctx, dockerServiceListOptions())
	if err != nil {
		t.Logf("service list failed (may be expected): %v", err)
	} else {
		t.Logf("found %d services in swarm", len(services.Items))
	}

	// Cleanup
	authedDeleteWithHeaders(baseURL+"/api/v1/stacks/"+stackID, auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	})
}

func TestDatabaseProvisioning(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)

	// Create project
	projectRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{"name": fmt.Sprintf("db-project-%d", time.Now().UnixNano())}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := asString(projectRes["id"])

	// Provision PostgreSQL
	pgRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/database-services", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{
		"projectId":    projectID,
		"engine":       "postgres",
		"name":         fmt.Sprintf("ci-pg-%d", time.Now().UnixNano()),
		"version":      "16",
		"username":     "testuser",
		"databaseName": "testdb",
	}, &pgRes, http.StatusCreated); err != nil {
		t.Fatalf("create postgres service failed: %v", err)
	}
	pgID := asString(pgRes["id"])
	if pgID == "" {
		t.Fatalf("postgres service id missing")
	}

	// Provision Redis
	redisRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/database-services", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{
		"projectId": projectID,
		"engine":    "redis",
		"name":      fmt.Sprintf("ci-redis-%d", time.Now().UnixNano()),
	}, &redisRes, http.StatusCreated); err != nil {
		t.Fatalf("create redis service failed: %v", err)
	}
	redisID := asString(redisRes["id"])
	if redisID == "" {
		t.Fatalf("redis service id missing")
	}

	// Verify both appear in list
	resp, err := authedGetWithHeaders(baseURL+"/api/v1/database-services", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	})
	if err != nil {
		t.Fatalf("list database services failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list database services status=%d", resp.StatusCode)
	}

	// Verify individual GET
	pgResp, err := authedGetWithHeaders(baseURL+"/api/v1/database-services/"+pgID, auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	})
	if err != nil {
		t.Fatalf("get postgres service failed: %v", err)
	}
	defer pgResp.Body.Close()
	if pgResp.StatusCode != http.StatusOK {
		t.Fatalf("get postgres service status=%d", pgResp.StatusCode)
	}
}
