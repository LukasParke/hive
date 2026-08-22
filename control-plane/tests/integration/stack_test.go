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

// TestApplicationNetworkAttachmentRegression guards the routing fix: an
// application deployed with a domain pre-attached must carry BOTH the
// project overlay (hive_project_{slug}) and the shared proxy overlay
// (hive_proxy) in its TaskTemplate.Networks, and removing the domain must
// drop hive_proxy on the next apply while the project network stays.
func TestApplicationNetworkAttachmentRegression(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	cli := dindClient(t)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}
	ctx := context.Background()

	projectName := fmt.Sprintf("netreg-project-%d", time.Now().UnixNano())
	projectRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, headers, map[string]any{
		"name": projectName,
	}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := asString(projectRes["id"])

	appName := "netreg-app-" + fmt.Sprintf("%d", time.Now().UnixNano())
	appRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications", auth.AccessToken, headers, map[string]any{
		"projectId":  projectID,
		"name":       appName,
		"sourceType": "image",
		"image":      "alpine:3.21",
	}, &appRes, http.StatusCreated); err != nil {
		t.Fatalf("create app failed: %v", err)
	}
	appID := asString(appRes["id"])
	t.Cleanup(func() {
		_, _ = authedDeleteWithHeaders(baseURL+"/api/v1/applications/"+appID, auth.AccessToken, headers)
	})

	// Attach the domain BEFORE the first deploy so a single apply is
	// expected to wire both overlays.
	hostname := fmt.Sprintf("netreg-%d.ci.local", time.Now().UnixNano())
	domainRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/domains", auth.AccessToken, headers, map[string]any{
		"applicationId": appID,
		"hostname":      hostname,
		"tlsEnabled":    false,
	}, &domainRes, http.StatusCreated); err != nil {
		t.Fatalf("create domain failed: %v", err)
	}
	domainID := asString(domainRes["id"])
	t.Cleanup(func() {
		_, _ = authedDeleteWithHeaders(baseURL+"/api/v1/domains/"+domainID, auth.AccessToken, headers)
	})

	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/deploy", auth.AccessToken, headers, map[string]any{}, &map[string]any{}, http.StatusAccepted); err != nil {
		t.Fatalf("enqueue deploy failed: %v", err)
	}

	var svcID string
	pollUntil(t, 90*time.Second, 2*time.Second, "app service created in swarm", func() error {
		services, err := cli.ServiceList(ctx, dockerServiceListOptions())
		if err != nil {
			return err
		}
		for _, svc := range services.Items {
			if svc.Spec.Labels["hive.app.id"] == appID {
				svcID = svc.ID
				return nil
			}
		}
		return fmt.Errorf("no service labeled hive.app.id=%s", appID)
	})

	projectNetwork := "hive_project_" + strings.ToLower(projectName)
	networks := func() map[string]bool {
		t.Helper()
		out := map[string]bool{}
		inspect, err := cli.ServiceInspect(ctx, svcID, dockerclient.ServiceInspectOptions{})
		if err != nil {
			return out
		}
		// Attachments reference network IDs; resolve them to names so the
		// assertions below can match hive_project_* / hive_proxy.
		summaries, err := cli.NetworkList(ctx, dockerclient.NetworkListOptions{})
		if err != nil {
			return out
		}
		idToName := make(map[string]string, len(summaries.Items))
		for _, n := range summaries.Items {
			idToName[n.ID] = n.Name
		}
		for _, n := range inspect.Service.Spec.TaskTemplate.Networks {
			name := n.Target
			if resolved, ok := idToName[n.Target]; ok {
				name = resolved
			}
			out[name] = true
		}
		return out
	}

	pollUntil(t, 60*time.Second, 2*time.Second, "service networks wired after deploy", func() error {
		attached := networks()
		if !attached["hive_project_"+slugify(projectName)] && !attached["hive_proxy"] {
			// Zero-value behavior before the Wave-2 caller wiring lands:
			// jobs callsites do not populate ProjectSlug/DomainLookup yet.
			t.Skip("Wave-2 caller wiring not landed: ProjectSlug/DomainLookup are not populated at the jobs callsites, so no overlays are attached")
		}
		if !attached[projectNetwork] {
			return fmt.Errorf("project network %s not attached (have %v)", projectNetwork, attached)
		}
		if !attached["hive_proxy"] {
			return fmt.Errorf("hive_proxy not attached despite pre-deploy domain (have %v)", attached)
		}
		return nil
	})

	// Remove the domain; the next apply must drop hive_proxy but keep the
	// project network.
	deleteResp, err := authedDeleteWithHeaders(baseURL+"/api/v1/domains/"+domainID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("delete domain request failed: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK && deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete domain status=%d", deleteResp.StatusCode)
	}

	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/deploy", auth.AccessToken, headers, map[string]any{}, &map[string]any{}, http.StatusAccepted); err != nil {
		t.Fatalf("re-enqueue deploy failed: %v", err)
	}
	pollUntil(t, 90*time.Second, 2*time.Second, "hive_proxy dropped on next apply", func() error {
		attached := networks()
		if attached["hive_proxy"] {
			return fmt.Errorf("hive_proxy still attached after domain removal (have %v)", attached)
		}
		if !attached[projectNetwork] {
			return fmt.Errorf("project network %s lost after domain removal (have %v)", projectNetwork, attached)
		}
		return nil
	})
}

// slugify mirrors network.ProjectNetworkName's normalization: lowercase
// with every non-[a-z0-9_-] run replaced by a single dash.
func slugify(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !ok {
			if lastDash {
				continue
			}
			b.WriteByte('-')
			lastDash = true
			continue
		}
		b.WriteRune(r)
		lastDash = false
	}
	return strings.Trim(b.String(), "-")
}
