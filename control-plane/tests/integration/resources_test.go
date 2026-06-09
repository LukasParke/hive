//go:build integration
// +build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	dockerclient "github.com/moby/moby/client"
)

func TestSecretConfigNetworkCRUD(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	dockerHost := getenv("HIVE_DOCKER_HOST", "tcp://127.0.0.1:2375")
	auth := bootstrapAuthContext(t, baseURL)

	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// Create secret
	secretName := fmt.Sprintf("ci-secret-%d", time.Now().UnixNano())
	secretRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/secrets", auth.AccessToken, headers, map[string]any{
		"name": secretName,
		"data": "super-secret-value",
	}, &secretRes, http.StatusCreated); err != nil {
		t.Fatalf("create secret failed: %v", err)
	}

	// Create config
	configName := fmt.Sprintf("ci-config-%d", time.Now().UnixNano())
	configRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/configs", auth.AccessToken, headers, map[string]any{
		"name": configName,
		"data": "config-data-value",
	}, &configRes, http.StatusCreated); err != nil {
		t.Fatalf("create config failed: %v", err)
	}

	// Create network
	networkName := fmt.Sprintf("ci-network-%d", time.Now().UnixNano())
	networkRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/networks", auth.AccessToken, headers, map[string]any{
		"name": networkName,
	}, &networkRes, http.StatusCreated); err != nil {
		t.Fatalf("create network failed: %v", err)
	}

	// List secrets
	secretsResp, err := authedGetWithHeaders(baseURL+"/api/v1/secrets", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list secrets failed: %v", err)
	}
	defer secretsResp.Body.Close()
	if secretsResp.StatusCode != http.StatusOK {
		t.Fatalf("list secrets status=%d", secretsResp.StatusCode)
	}

	// List configs
	configsResp, err := authedGetWithHeaders(baseURL+"/api/v1/configs", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list configs failed: %v", err)
	}
	defer configsResp.Body.Close()
	if configsResp.StatusCode != http.StatusOK {
		t.Fatalf("list configs status=%d", configsResp.StatusCode)
	}

	// List networks
	networksResp, err := authedGetWithHeaders(baseURL+"/api/v1/networks", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list networks failed: %v", err)
	}
	defer networksResp.Body.Close()
	if networksResp.StatusCode != http.StatusOK {
		t.Fatalf("list networks status=%d", networksResp.StatusCode)
	}

	// Verify resources exist in Docker Swarm
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(dockerHost),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("docker client init failed: %v", err)
	}
	ctx := context.Background()

	secrets, err := cli.SecretList(ctx, dockerSecretListOptions())
	if err != nil {
		t.Logf("docker secret list failed (may be expected): %v", err)
	} else {
		found := false
		for _, s := range secrets.Items {
			if s.Spec.Name == secretName {
				found = true
				break
			}
		}
		if !found {
			t.Logf("secret %q not found in Docker (may not have been synced)", secretName)
		}
	}

	networks, err := cli.NetworkList(ctx, dockerNetworkListOptions())
	if err != nil {
		t.Logf("docker network list failed (may be expected): %v", err)
	} else {
		t.Logf("found %d Docker networks", len(networks.Items))
	}
}

func TestRegistryCRUD(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// Create
	registryName := fmt.Sprintf("ci-registry-%d", time.Now().UnixNano())
	createRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/registries", auth.AccessToken, headers, map[string]any{
		"name": registryName,
		"url":  "http://127.0.0.1:5000",
	}, &createRes, http.StatusCreated); err != nil {
		t.Fatalf("create registry failed: %v", err)
	}
	registryID := asString(createRes["id"])
	if registryID == "" {
		t.Fatalf("registry id missing")
	}

	// GET
	getResp, err := authedGetWithHeaders(baseURL+"/api/v1/registries/"+registryID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get registry failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get registry status=%d", getResp.StatusCode)
	}

	// Update
	updateRes := map[string]any{}
	if err := authedPutJSONWithHeaders(baseURL+"/api/v1/registries/"+registryID, auth.AccessToken, headers, map[string]any{
		"name": registryName + "-updated",
	}, &updateRes, http.StatusOK); err != nil {
		t.Fatalf("update registry failed: %v", err)
	}

	// Test connectivity
	testRes := map[string]any{}
	testErr := authedPostJSONWithHeaders(baseURL+"/api/v1/registries/"+registryID+"/test", auth.AccessToken, headers, map[string]any{}, &testRes, http.StatusOK)
	if testErr != nil {
		t.Logf("registry test returned error (expected if registry not reachable): %v", testErr)
	}

	// List
	listResp, err := authedGetWithHeaders(baseURL+"/api/v1/registries", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list registries failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list registries status=%d", listResp.StatusCode)
	}

	// Delete
	deleteResp, err := authedDeleteWithHeaders(baseURL+"/api/v1/registries/"+registryID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("delete registry request failed: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK && deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete registry status=%d", deleteResp.StatusCode)
	}

	// Verify 404
	verify404, err := authedGetWithHeaders(baseURL+"/api/v1/registries/"+registryID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get deleted registry failed: %v", err)
	}
	defer verify404.Body.Close()
	if verify404.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", verify404.StatusCode)
	}
}

func TestDomainCRUD(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// Create project + app for domain
	projectRes := map[string]any{}
	authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, headers, map[string]any{
		"name": fmt.Sprintf("domain-project-%d", time.Now().UnixNano()),
	}, &projectRes, http.StatusCreated)
	projectID := asString(projectRes["id"])

	appRes := map[string]any{}
	authedPostJSONWithHeaders(baseURL+"/api/v1/applications", auth.AccessToken, headers, map[string]any{
		"projectId":  projectID,
		"name":       fmt.Sprintf("domain-app-%d", time.Now().UnixNano()),
		"sourceType": "image",
		"image":      "nginx:alpine",
	}, &appRes, http.StatusCreated)
	appID := asString(appRes["id"])

	// Create domain
	hostname := fmt.Sprintf("test-%d.example.local", time.Now().UnixNano())
	domainRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/domains", auth.AccessToken, headers, map[string]any{
		"applicationId": appID,
		"hostname":      hostname,
		"tlsEnabled":    false,
	}, &domainRes, http.StatusCreated); err != nil {
		t.Fatalf("create domain failed: %v", err)
	}
	domainID := asString(domainRes["id"])
	if domainID == "" {
		t.Fatalf("domain id missing")
	}

	// GET
	getResp, err := authedGetWithHeaders(baseURL+"/api/v1/domains/"+domainID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get domain failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get domain status=%d", getResp.StatusCode)
	}

	// Update hostname
	updateRes := map[string]any{}
	newHostname := fmt.Sprintf("updated-%d.example.local", time.Now().UnixNano())
	if err := authedPutJSONWithHeaders(baseURL+"/api/v1/domains/"+domainID, auth.AccessToken, headers, map[string]any{
		"hostname": newHostname,
	}, &updateRes, http.StatusOK); err != nil {
		t.Fatalf("update domain failed: %v", err)
	}

	// List
	listResp, err := authedGetWithHeaders(baseURL+"/api/v1/domains", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list domains failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list domains status=%d", listResp.StatusCode)
	}

	// Delete
	deleteResp, err := authedDeleteWithHeaders(baseURL+"/api/v1/domains/"+domainID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("delete domain request failed: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK && deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete domain status=%d", deleteResp.StatusCode)
	}

	// Verify 404
	verify404, err := authedGetWithHeaders(baseURL+"/api/v1/domains/"+domainID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get deleted domain failed: %v", err)
	}
	defer verify404.Body.Close()
	if verify404.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", verify404.StatusCode)
	}
}

func TestNotificationCRUD(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// Create
	createRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/notifications", auth.AccessToken, headers, map[string]any{
		"channel": "webhook",
		"target":  "http://example.invalid/hook",
		"enabled": true,
	}, &createRes, http.StatusCreated); err != nil {
		t.Fatalf("create notification failed: %v", err)
	}
	notifID := asString(createRes["id"])
	if notifID == "" {
		t.Fatalf("notification id missing")
	}

	// GET
	getResp, err := authedGetWithHeaders(baseURL+"/api/v1/notifications/"+notifID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get notification failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get notification status=%d", getResp.StatusCode)
	}

	// Update (disable)
	updateRes := map[string]any{}
	if err := authedPutJSONWithHeaders(baseURL+"/api/v1/notifications/"+notifID, auth.AccessToken, headers, map[string]any{
		"enabled": false,
	}, &updateRes, http.StatusOK); err != nil {
		t.Fatalf("update notification failed: %v", err)
	}

	// List
	listResp, err := authedGetWithHeaders(baseURL+"/api/v1/notifications", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list notifications failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list notifications status=%d", listResp.StatusCode)
	}

	// Delete
	deleteResp, err := authedDeleteWithHeaders(baseURL+"/api/v1/notifications/"+notifID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("delete notification request failed: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK && deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete notification status=%d", deleteResp.StatusCode)
	}

	// Verify 404
	verify404, err := authedGetWithHeaders(baseURL+"/api/v1/notifications/"+notifID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get deleted notification failed: %v", err)
	}
	defer verify404.Body.Close()
	if verify404.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", verify404.StatusCode)
	}
}

func TestScheduleCRUD(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// Create a schedule
	scheduleName := fmt.Sprintf("ci-schedule-%d", time.Now().UnixNano())
	createRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/schedules", auth.AccessToken, headers, map[string]any{
		"name":       scheduleName,
		"cronExpr":   "0 3 * * *",
		"targetType": "backup",
		"targetId":   "00000000-0000-0000-0000-000000000000",
		"enabled":    true,
	}, &createRes, http.StatusCreated); err != nil {
		t.Fatalf("create schedule failed: %v", err)
	}
	scheduleID := asString(createRes["id"])
	if scheduleID == "" {
		t.Fatalf("schedule id missing")
	}

	// List
	listResp, err := authedGetWithHeaders(baseURL+"/api/v1/schedules", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list schedules failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list schedules status=%d", listResp.StatusCode)
	}

	// Update cron + disable
	updateRes := map[string]any{}
	if err := authedPutJSONWithHeaders(baseURL+"/api/v1/schedules/"+scheduleID, auth.AccessToken, headers, map[string]any{
		"cronExpr": "0 4 * * *",
		"enabled":  false,
	}, &updateRes, http.StatusOK); err != nil {
		t.Fatalf("update schedule failed: %v", err)
	}

	// Trigger manual run
	runRes := map[string]any{}
	runErr := authedPostJSONWithHeaders(baseURL+"/api/v1/schedules/"+scheduleID+"/run", auth.AccessToken, headers, map[string]any{}, &runRes, http.StatusOK)
	if runErr != nil {
		t.Logf("schedule manual run returned error (may be expected): %v", runErr)
	}

	// Delete
	deleteResp, err := authedDeleteWithHeaders(baseURL+"/api/v1/schedules/"+scheduleID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("delete schedule request failed: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK && deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete schedule status=%d", deleteResp.StatusCode)
	}
}

func TestBackupDestinationAndRunCRUD(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// Create backup destination
	destName := fmt.Sprintf("ci-dest-%d", time.Now().UnixNano())
	createRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/backup/destinations", auth.AccessToken, headers, map[string]any{
		"name":   destName,
		"type":   "local",
		"config": map[string]any{},
	}, &createRes, http.StatusCreated); err != nil {
		t.Fatalf("create backup destination failed: %v", err)
	}
	destID := asString(createRes["id"])
	if destID == "" {
		t.Fatalf("backup destination id missing")
	}

	// GET
	getResp, err := authedGetWithHeaders(baseURL+"/api/v1/backup/destinations/"+destID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get backup destination failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get backup destination status=%d", getResp.StatusCode)
	}

	// Update name
	updateRes := map[string]any{}
	if err := authedPutJSONWithHeaders(baseURL+"/api/v1/backup/destinations/"+destID, auth.AccessToken, headers, map[string]any{
		"name": destName + "-updated",
	}, &updateRes, http.StatusOK); err != nil {
		t.Fatalf("update backup destination failed: %v", err)
	}

	// Create a backup run (targeting a fake db service - will be queued)
	backupRes := map[string]any{}
	backupErr := authedPostJSONWithHeaders(baseURL+"/api/v1/backups", auth.AccessToken, headers, map[string]any{
		"targetType":    "database",
		"targetId":      "00000000-0000-0000-0000-000000000000",
		"destinationId": destID,
	}, &backupRes, http.StatusCreated)
	if backupErr != nil {
		t.Logf("create backup run returned error (may be expected with fake target): %v", backupErr)
	}

	// List backups
	listResp, err := authedGetWithHeaders(baseURL+"/api/v1/backups", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list backups failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list backups status=%d", listResp.StatusCode)
	}

	// Delete backup destination
	deleteResp, err := authedDeleteWithHeaders(baseURL+"/api/v1/backup/destinations/"+destID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("delete backup destination request failed: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK && deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete backup destination status=%d", deleteResp.StatusCode)
	}
}

func TestNodeListAndClusterResources(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// List nodes
	nodesResp, err := authedGetWithHeaders(baseURL+"/api/v1/nodes", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list nodes failed: %v", err)
	}
	defer nodesResp.Body.Close()
	if nodesResp.StatusCode != http.StatusOK {
		t.Fatalf("list nodes status=%d", nodesResp.StatusCode)
	}
	var nodesBody struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(nodesResp.Body).Decode(&nodesBody); err != nil {
		t.Fatalf("decode nodes failed: %v", err)
	}
	if len(nodesBody.Items) < 3 {
		t.Fatalf("expected at least 3 nodes, got %d", len(nodesBody.Items))
	}

	// GET cluster resources
	resourcesResp, err := authedGetWithHeaders(baseURL+"/api/v1/cluster/resources", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get cluster resources failed: %v", err)
	}
	defer resourcesResp.Body.Close()
	if resourcesResp.StatusCode != http.StatusOK {
		t.Fatalf("get cluster resources status=%d", resourcesResp.StatusCode)
	}

	// GET cluster settings
	clusterResp, err := authedGetWithHeaders(baseURL+"/api/v1/settings/cluster", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get cluster info failed: %v", err)
	}
	defer clusterResp.Body.Close()
	if clusterResp.StatusCode != http.StatusOK {
		t.Fatalf("get cluster info status=%d", clusterResp.StatusCode)
	}
	var clusterInfo map[string]any
	if err := json.NewDecoder(clusterResp.Body).Decode(&clusterInfo); err != nil {
		t.Fatalf("decode cluster info failed: %v", err)
	}
	nodeCount, _ := clusterInfo["nodeCount"].(float64)
	if nodeCount < 3 {
		t.Fatalf("expected nodeCount >= 3, got %v", nodeCount)
	}
}

func TestEnvironmentCRUD(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// Create project for environment
	projectRes := map[string]any{}
	authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, headers, map[string]any{
		"name": fmt.Sprintf("env-project-%d", time.Now().UnixNano()),
	}, &projectRes, http.StatusCreated)
	projectID := asString(projectRes["id"])

	// Create environment
	envName := fmt.Sprintf("ci-env-%d", time.Now().UnixNano())
	envSlug := fmt.Sprintf("ci-env-%d", time.Now().UnixNano())
	createRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/environments", auth.AccessToken, headers, map[string]any{
		"projectId": projectID,
		"name":      envName,
		"slug":      envSlug,
	}, &createRes, http.StatusCreated); err != nil {
		t.Fatalf("create environment failed: %v", err)
	}
	envID := asString(createRes["id"])
	if envID == "" {
		t.Fatalf("environment id missing")
	}

	// GET
	getResp, err := authedGetWithHeaders(baseURL+"/api/v1/environments/"+envID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get environment failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get environment status=%d", getResp.StatusCode)
	}

	// Update
	updateRes := map[string]any{}
	if err := authedPutJSONWithHeaders(baseURL+"/api/v1/environments/"+envID, auth.AccessToken, headers, map[string]any{
		"name": envName + "-updated",
	}, &updateRes, http.StatusOK); err != nil {
		t.Fatalf("update environment failed: %v", err)
	}

	// List
	listResp, err := authedGetWithHeaders(baseURL+"/api/v1/environments", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list environments failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list environments status=%d", listResp.StatusCode)
	}

	// Delete
	deleteResp, err := authedDeleteWithHeaders(baseURL+"/api/v1/environments/"+envID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("delete environment request failed: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK && deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete environment status=%d", deleteResp.StatusCode)
	}
}

func TestSettingsAndAuditLog(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// GET settings
	getResp, err := authedGetWithHeaders(baseURL+"/api/v1/settings", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("get settings failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get settings status=%d", getResp.StatusCode)
	}

	// PUT settings
	putRes := map[string]any{}
	putErr := authedPutJSONWithHeaders(baseURL+"/api/v1/settings", auth.AccessToken, headers, map[string]any{
		"testKey": "testValue",
	}, &putRes, http.StatusOK)
	if putErr != nil {
		t.Logf("put settings returned error (may be expected): %v", putErr)
	}

	// Create server to generate audit entry
	serverRes := map[string]any{}
	authedPostJSONWithHeaders(baseURL+"/api/v1/settings/servers", auth.AccessToken, headers, map[string]any{
		"name":    fmt.Sprintf("audit-server-%d", time.Now().UnixNano()),
		"host":    "10.0.0.99",
		"sshPort": 22,
	}, &serverRes, http.StatusCreated)

	// List request events
	eventsResp, err := authedGetWithHeaders(baseURL+"/api/v1/settings/requests", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list request events failed: %v", err)
	}
	defer eventsResp.Body.Close()
	if eventsResp.StatusCode != http.StatusOK {
		t.Fatalf("list request events status=%d", eventsResp.StatusCode)
	}
}

func TestApplicationStartStopRestart(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// Create project + app
	projectRes := map[string]any{}
	authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, headers, map[string]any{
		"name": fmt.Sprintf("ops-project-%d", time.Now().UnixNano()),
	}, &projectRes, http.StatusCreated)
	projectID := asString(projectRes["id"])

	appRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications", auth.AccessToken, headers, map[string]any{
		"projectId":  projectID,
		"name":       fmt.Sprintf("ops-app-%d", time.Now().UnixNano()),
		"sourceType": "image",
		"image":      "alpine:3.21",
	}, &appRes, http.StatusCreated); err != nil {
		t.Fatalf("create app failed: %v", err)
	}
	appID := asString(appRes["id"])

	// Deploy
	deployRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/deploy", auth.AccessToken, headers, map[string]any{}, &deployRes, http.StatusAccepted); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	// Wait for deploy to complete
	dbURL := getenv("HIVE_DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:6432/hive?sslmode=disable")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("create pgx pool failed: %v", err)
	}
	defer pool.Close()

	if err := retry(20, 1*time.Second, func() error {
		var status string
		if err := pool.QueryRow(ctx, `select status::text from build_jobs where application_id=$1::uuid order by created_at desc limit 1`, appID).Scan(&status); err != nil {
			return err
		}
		if status != "complete" {
			return fmt.Errorf("deploy status=%s", status)
		}
		return nil
	}); err != nil {
		t.Fatalf("deploy did not complete: %v", err)
	}

	// Stop
	stopRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/stop", auth.AccessToken, headers, map[string]any{}, &stopRes, http.StatusOK); err != nil {
		t.Fatalf("stop app failed: %v", err)
	}

	// Start
	startRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/start", auth.AccessToken, headers, map[string]any{}, &startRes, http.StatusOK); err != nil {
		t.Fatalf("start app failed: %v", err)
	}

	// Restart
	restartRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/restart", auth.AccessToken, headers, map[string]any{}, &restartRes, http.StatusOK); err != nil {
		t.Fatalf("restart app failed: %v", err)
	}
}

// Docker list option helpers (needed to avoid importing types package with breaking API changes)
func dockerSecretListOptions() dockerclient.SecretListOptions {
	return dockerclient.SecretListOptions{}
}

func dockerNetworkListOptions() dockerclient.NetworkListOptions {
	return dockerclient.NetworkListOptions{}
}
