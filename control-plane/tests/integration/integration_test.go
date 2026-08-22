//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	dockerclient "github.com/moby/moby/client"
)

// TestMain gates the whole suite on cluster health: after a control-plane
// redeploy the API can reset connections for a few seconds while booting,
// and per-test bootstrap logins would otherwise race that window.
func TestMain(m *testing.M) {
	healthURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000") + "/api/v1/health"
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(healthURL) //nolint:gosec
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	os.Exit(m.Run())
}

func TestClusterBootstrap(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	healthURL := baseURL + "/api/v1/health"

	err := retry(60, 2*time.Second, func() error {
		resp, err := http.Get(healthURL) //nolint:gosec
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health returned %d", resp.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return err
		}
		if body["status"] != "ok" {
			return fmt.Errorf("unexpected status: %q", body["status"])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cluster did not become healthy: %v", err)
	}
}

func TestAuthOrgRBACFlow(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	email := fmt.Sprintf("ci-%d@example.com", time.Now().UnixNano())
	password := "passw0rd!"

	registerRes := map[string]any{}
	if err := postJSON(baseURL+"/api/v1/auth/register", map[string]any{
		"email":       email,
		"password":    password,
		"displayName": "CI",
	}, &registerRes, http.StatusCreated); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	loginRes := map[string]any{}
	if err := postJSON(baseURL+"/api/v1/auth/login", map[string]any{
		"email":    email,
		"password": password,
	}, &loginRes, http.StatusOK); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	accessToken := asString(loginRes["accessToken"])
	refreshToken := asString(loginRes["refreshToken"])
	if accessToken == "" || refreshToken == "" {
		t.Fatalf("missing tokens in login response: %#v", loginRes)
	}

	meReq, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+accessToken)
	meResp, err := http.DefaultClient.Do(meReq)
	if err != nil {
		t.Fatalf("auth me request failed: %v", err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("auth me status=%d", meResp.StatusCode)
	}

	createOrgRes := map[string]any{}
	if err := authedPostJSON(baseURL+"/api/v1/organizations", accessToken, map[string]any{
		"name": fmt.Sprintf("org-%d", time.Now().UnixNano()),
		"slug": fmt.Sprintf("org-%d", time.Now().UnixNano()),
	}, &createOrgRes, http.StatusCreated); err != nil {
		t.Fatalf("create organization failed: %v", err)
	}
	orgID := asString(createOrgRes["id"])
	if orgID == "" {
		t.Fatalf("organization id missing")
	}

	protectedReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/projects", bytes.NewReader([]byte(`{"name":"no-auth-project"}`)))
	protectedReq.Header.Set("Content-Type", "application/json")
	protectedResp, err := http.DefaultClient.Do(protectedReq)
	if err != nil {
		t.Fatalf("unauthorized project request failed: %v", err)
	}
	defer protectedResp.Body.Close()
	if protectedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without token, got %d", protectedResp.StatusCode)
	}

	projectRes := map[string]any{}
	projectName := fmt.Sprintf("authz-project-%d", time.Now().UnixNano())
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", accessToken, map[string]string{
		"X-Organization-Id": orgID,
	}, map[string]any{"name": projectName}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project as owner failed: %v", err)
	}

	apiKeyRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/organizations/"+orgID+"/api-keys", accessToken, map[string]string{
		"X-Organization-Id": orgID,
	}, map[string]any{"name": "ci-key"}, &apiKeyRes, http.StatusCreated); err != nil {
		t.Fatalf("create api key failed: %v", err)
	}
	token := asString(apiKeyRes["token"])
	if token == "" {
		t.Fatalf("api key token missing")
	}
	apiKeyReq, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/projects", nil)
	apiKeyReq.Header.Set("X-API-Key", token)
	apiKeyReq.Header.Set("X-Organization-Id", orgID)
	apiKeyResp, err := http.DefaultClient.Do(apiKeyReq)
	if err != nil {
		t.Fatalf("api key list projects failed: %v", err)
	}
	defer apiKeyResp.Body.Close()
	if apiKeyResp.StatusCode != http.StatusOK {
		t.Fatalf("api key list projects status=%d", apiKeyResp.StatusCode)
	}

	refreshRes := map[string]any{}
	if err := postJSON(baseURL+"/api/v1/auth/refresh", map[string]string{
		"refreshToken": refreshToken,
	}, &refreshRes, http.StatusOK); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
}

func TestApplicationGitDeploy(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)

	projectName := "ci-project-" + fmt.Sprintf("%d", time.Now().UnixNano())
	projectRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]string{"name": projectName}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := asString(projectRes["id"])
	if projectID == "" {
		t.Fatalf("project id missing in response: %#v", projectRes)
	}

	appName := "ci-app-" + fmt.Sprintf("%d", time.Now().UnixNano())
	appRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{
		"projectId":  projectID,
		"name":       appName,
		"sourceType": "image",
		"image":      "alpine:3.21",
	}, &appRes, http.StatusCreated); err != nil {
		t.Fatalf("create app failed: %v", err)
	}
	appID := asString(appRes["id"])
	if appID == "" {
		t.Fatalf("app id missing in response: %#v", appRes)
	}

	deployRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/deploy", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{}, &deployRes, http.StatusAccepted); err != nil {
		t.Fatalf("enqueue deploy failed: %v", err)
	}

	dbURL := getenv("HIVE_DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5432/hive?sslmode=disable")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("create pgx pool failed: %v", err)
	}
	defer pool.Close()

	err = retry(20, 1*time.Second, func() error {
		var status string
		err := pool.QueryRow(ctx, `
			select status::text
			from build_jobs
			where application_id = $1::uuid
			order by created_at desc
			limit 1
		`, appID).Scan(&status)
		if err != nil {
			return err
		}
		if status != "complete" {
			return fmt.Errorf("job status=%s", status)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("deploy job did not complete: %v", err)
	}

	err = retry(20, 1*time.Second, func() error {
		var count int
		err := pool.QueryRow(ctx, `select count(*) from deployments where application_id = $1::uuid`, appID).Scan(&count)
		if err != nil {
			return err
		}
		if count < 1 {
			return fmt.Errorf("deployment history not recorded")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("deployment history check failed: %v", err)
	}
}

func TestRollbackQueueFlow(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)

	projectRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]string{"name": "rollback-project-" + fmt.Sprintf("%d", time.Now().UnixNano())}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := asString(projectRes["id"])
	appRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{
		"projectId":  projectID,
		"name":       "rollback-app-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"sourceType": "image",
		"image":      "alpine:3.21",
	}, &appRes, http.StatusCreated); err != nil {
		t.Fatalf("create app failed: %v", err)
	}
	appID := asString(appRes["id"])

	// The platform enforces ONE active build per application (partial unique
	// index on queued/building rows), so rapid double-deploys are rejected.
	// Deploy, wait for completion, then deploy again to get two deployments.
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/deploy", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{}, &map[string]any{}, http.StatusAccepted); err != nil {
		t.Fatalf("first deploy enqueue failed: %v", err)
	}
	prePool, err := pgxpool.New(context.Background(), getenv("HIVE_DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5432/hive?sslmode=disable"))
	if err != nil {
		t.Fatalf("create pgx pool failed: %v", err)
	}
	if err := retry(30, 1*time.Second, func() error {
		var status string
		if err := prePool.QueryRow(context.Background(), `select status::text from build_jobs where application_id=$1::uuid order by created_at desc limit 1`, appID).Scan(&status); err != nil {
			return err
		}
		if status != "complete" && status != "failed" {
			return fmt.Errorf("first build still %s", status)
		}
		return nil
	}); err != nil {
		prePool.Close()
		t.Fatalf("first build did not finish: %v", err)
	}
	prePool.Close()
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/deploy", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{}, &map[string]any{}, http.StatusAccepted); err != nil {
		t.Fatalf("second deploy enqueue failed: %v", err)
	}

	dbURL := getenv("HIVE_DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5432/hive?sslmode=disable")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("create pgx pool failed: %v", err)
	}
	defer pool.Close()

	if err := retry(30, 1*time.Second, func() error {
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from deployments where application_id=$1::uuid`, appID).Scan(&count); err != nil {
			return err
		}
		if count < 2 {
			return fmt.Errorf("need at least two deployments, got %d", count)
		}
		return nil
	}); err != nil {
		t.Fatalf("deployments not ready: %v", err)
	}

	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/rollback", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{}, &map[string]any{}, http.StatusAccepted); err != nil {
		t.Fatalf("rollback enqueue failed: %v", err)
	}

	if err := retry(30, 1*time.Second, func() error {
		var status string
		if err := pool.QueryRow(ctx, `select status::text from build_jobs where application_id=$1::uuid and trigger='rollback' order by created_at desc limit 1`, appID).Scan(&status); err != nil {
			return err
		}
		if status != "complete" {
			return fmt.Errorf("rollback status=%s", status)
		}
		return nil
	}); err != nil {
		t.Fatalf("rollback job did not complete: %v", err)
	}
}

func TestControlPlaneHA(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	dockerHost := getenv("HIVE_DOCKER_HOST", "tcp://127.0.0.1:2375")
	stackName := getenv("STACK_NAME", "hive")
	auth := bootstrapAuthContext(t, baseURL)

	resp, err := authedGet(baseURL+"/api/v1/nodes", auth.AccessToken)
	if err != nil {
		t.Fatalf("list nodes request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list nodes status=%d", resp.StatusCode)
	}
	var nodesBody struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&nodesBody); err != nil {
		t.Fatalf("decode nodes failed: %v", err)
	}
	if len(nodesBody.Items) < 3 {
		t.Fatalf("expected at least 3 nodes, got %d", len(nodesBody.Items))
	}

	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(dockerHost),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("docker client init failed: %v", err)
	}
	services, err := cli.ServiceList(context.Background(), dockerclient.ServiceListOptions{})
	if err != nil {
		t.Fatalf("service list failed: %v", err)
	}

	controlPlaneName := stackName + "_control-plane"
	found := false
	for _, svc := range services.Items {
		if svc.Spec.Name == controlPlaneName {
			found = true
			if svc.Spec.UpdateConfig == nil || svc.Spec.UpdateConfig.FailureAction != "rollback" {
				t.Fatalf("expected rollback failure action on control-plane update config")
			}
		}
	}
	if !found {
		t.Fatalf("service %s not found", controlPlaneName)
	}
}

func TestPlatformDomainEndpoints(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)

	for _, path := range []string{
		"/api/v1/domains",
		"/api/v1/registries",
		"/api/v1/stacks",
		"/api/v1/builds",
		"/api/v1/settings",
		"/api/v1/backups",
		"/api/v1/git/providers",
		"/api/v1/notifications",
	} {
		resp, err := authedGetWithHeaders(baseURL+path, auth.AccessToken, map[string]string{
			"X-Organization-Id": auth.OrganizationID,
		})
		if err != nil {
			t.Fatalf("get %s failed: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get %s expected 200 got %d", path, resp.StatusCode)
		}
	}
}

func TestGithubWebhookSignatureFlow(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	secret := "ci-webhook-secret"
	providerRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/git/providers", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{
		"type":          "github",
		"name":          "ci-github",
		"baseUrl":       "https://github.com",
		"webhookSecret": secret,
		"enabled":       true,
	}, &providerRes, http.StatusCreated); err != nil {
		t.Fatalf("create provider failed: %v", err)
	}

	projectRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{"name": "webhook-project-" + fmt.Sprintf("%d", time.Now().UnixNano())}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := asString(projectRes["id"])
	appRes := map[string]any{}
	repoURL := "https://github.com/example/repo.git"
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{
		"projectId":     projectID,
		"name":          "webhook-app-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"sourceType":    "image",
		"image":         "alpine:3.21",
		"repositoryUrl": repoURL,
		"gitRef":        "main",
	}, &appRes, http.StatusCreated); err != nil {
		t.Fatalf("create app failed: %v", err)
	}
	appID := asString(appRes["id"])

	payload := map[string]any{
		"ref": "refs/heads/main",
		"repository": map[string]any{
			"clone_url": repoURL,
		},
		"commits": []map[string]any{{"modified": []string{"README.md"}}},
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(raw)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/webhooks/github", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signature)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("webhook request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("webhook status=%d", resp.StatusCode)
	}

	dbURL := getenv("HIVE_DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5432/hive?sslmode=disable")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("create pgx pool failed: %v", err)
	}
	defer pool.Close()
	if err := retry(30, 1*time.Second, func() error {
		var status string
		if err := pool.QueryRow(ctx, `select status::text from build_jobs where application_id=$1::uuid and trigger='webhook' order by created_at desc limit 1`, appID).Scan(&status); err != nil {
			return err
		}
		if status != "complete" {
			return fmt.Errorf("webhook status=%s", status)
		}
		return nil
	}); err != nil {
		t.Fatalf("webhook job did not complete: %v", err)
	}
}

func retry(attempts int, sleep time.Duration, fn func() error) error {
	var last error
	for i := 0; i < attempts; i++ {
		last = fn()
		if last == nil {
			return nil
		}
		time.Sleep(sleep)
	}
	return last
}

func postJSON(url string, payload any, out any, expectedStatus int) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("status=%d expected=%d", resp.StatusCode, expectedStatus)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func authedPostJSON(url, accessToken string, payload any, out any, expectedStatus int) error {
	return authedPostJSONWithHeaders(url, accessToken, map[string]string{}, payload, out, expectedStatus)
}

func authedGet(url, accessToken string) (*http.Response, error) {
	return authedGetWithHeaders(url, accessToken, nil)
}

func authedGetWithHeaders(url, accessToken string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return http.DefaultClient.Do(req)
}

func authedPostJSONWithHeaders(url, accessToken string, headers map[string]string, payload any, out any, expectedStatus int) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		return statusError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// statusError builds an error carrying the status code plus a short
// response-body snippet so failures point at the actual API error.
func statusError(resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(snippet)))
}

func authedPutJSONWithHeaders(url, accessToken string, headers map[string]string, payload any, out any, expectedStatus int) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("status=%d expected=%d", resp.StatusCode, expectedStatus)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func authedDeleteWithHeaders(url, accessToken string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return http.DefaultClient.Do(req)
}

func dockerServiceListOptions() dockerclient.ServiceListOptions {
	return dockerclient.ServiceListOptions{}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func getenv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

type authContext struct {
	AccessToken    string
	RefreshToken   string
	OrganizationID string
}

func bootstrapAuthContext(t *testing.T, baseURL string) authContext {
	t.Helper()
	email := fmt.Sprintf("ci-auth-%d@example.com", time.Now().UnixNano())
	password := "passw0rd!"
	_ = postJSON(baseURL+"/api/v1/auth/register", map[string]any{
		"email":       email,
		"password":    password,
		"displayName": "CI",
	}, &map[string]any{}, http.StatusCreated)

	loginRes := map[string]any{}
	// The public auth endpoints are rate limited; the full suite logs in
	// once per test, which can trip the bucket. Retry 429s with backoff —
	// the same behavior production clients need.
	var loginErr error
	for attempt := 0; attempt < 8; attempt++ {
		loginErr = postJSON(baseURL+"/api/v1/auth/login", map[string]any{
			"email":    email,
			"password": password,
		}, &loginRes, http.StatusOK)
		if loginErr == nil || !strings.Contains(loginErr.Error(), "status=429") {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	if loginErr != nil {
		t.Fatalf("login bootstrap failed: %v", loginErr)
	}
	accessToken := asString(loginRes["accessToken"])
	refreshToken := asString(loginRes["refreshToken"])

	createOrgRes := map[string]any{}
	if err := authedPostJSON(baseURL+"/api/v1/organizations", accessToken, map[string]any{
		"name": fmt.Sprintf("org-%d", time.Now().UnixNano()),
		"slug": fmt.Sprintf("org-%d", time.Now().UnixNano()),
	}, &createOrgRes, http.StatusCreated); err != nil {
		t.Fatalf("create organization bootstrap failed: %v", err)
	}
	return authContext{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		OrganizationID: asString(createOrgRes["id"]),
	}
}
