//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestAppEnvVarCRUD(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	headers := map[string]string{"X-Organization-Id": auth.OrganizationID}

	// Create project.
	projectRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, headers,
		map[string]any{"name": fmt.Sprintf("env-project-%d", time.Now().UnixNano())}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := asString(projectRes["id"])

	// Create application.
	appRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications", auth.AccessToken, headers,
		map[string]any{
			"projectId":  projectID,
			"name":       fmt.Sprintf("env-app-%d", time.Now().UnixNano()),
			"sourceType": "image",
			"image":      "alpine:3.21",
		}, &appRes, http.StatusCreated); err != nil {
		t.Fatalf("create app failed: %v", err)
	}
	appID := asString(appRes["id"])

	// 1. Create plain env var.
	plainRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env", auth.AccessToken, headers,
		map[string]any{"key": "APP_PORT", "value": "8080", "isSecret": false}, &plainRes, http.StatusCreated); err != nil {
		t.Fatalf("create plain env var failed: %v", err)
	}
	plainVarID := asString(plainRes["id"])
	if plainVarID == "" {
		t.Fatalf("plain var id missing")
	}

	// 2. Create secret env var.
	secretRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env", auth.AccessToken, headers,
		map[string]any{"key": "DB_PASSWORD", "value": "s3cret", "isSecret": true}, &secretRes, http.StatusCreated); err != nil {
		t.Fatalf("create secret env var failed: %v", err)
	}
	secretVarID := asString(secretRes["id"])
	if secretVarID == "" {
		t.Fatalf("secret var id missing")
	}

	// 3. List env vars — should have 2 items.
	listResp, err := authedGetWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list env vars failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list env vars status=%d", listResp.StatusCode)
	}
	var listBody struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list failed: %v", err)
	}
	if len(listBody.Items) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(listBody.Items))
	}
	for _, item := range listBody.Items {
		key := asString(item["key"])
		if key == "APP_PORT" {
			if asString(item["value"]) != "8080" {
				t.Fatalf("expected APP_PORT value 8080, got %v", item["value"])
			}
			if item["isSecret"] != false {
				t.Fatalf("expected APP_PORT isSecret=false")
			}
		} else if key == "DB_PASSWORD" {
			if item["value"] != nil {
				t.Fatalf("expected DB_PASSWORD value to be null, got %v", item["value"])
			}
			if item["isSecret"] != true {
				t.Fatalf("expected DB_PASSWORD isSecret=true")
			}
		}
	}

	// 4. Update plain var.
	updatePlainRes := map[string]any{}
	if err := authedPutJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env/"+plainVarID, auth.AccessToken, headers,
		map[string]any{"value": "9090"}, &updatePlainRes, http.StatusOK); err != nil {
		t.Fatalf("update plain env var failed: %v", err)
	}

	// 5. Verify update.
	listResp2, err := authedGetWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list env vars after update failed: %v", err)
	}
	defer listResp2.Body.Close()
	var listBody2 struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(listResp2.Body).Decode(&listBody2); err != nil {
		t.Fatalf("decode list failed: %v", err)
	}
	for _, item := range listBody2.Items {
		if asString(item["key"]) == "APP_PORT" && asString(item["value"]) != "9090" {
			t.Fatalf("expected APP_PORT value 9090 after update, got %v", item["value"])
		}
	}

	// 6. Update secret var.
	updateSecretRes := map[string]any{}
	if err := authedPutJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env/"+secretVarID, auth.AccessToken, headers,
		map[string]any{"value": "n3w-s3cret"}, &updateSecretRes, http.StatusOK); err != nil {
		t.Fatalf("update secret env var failed: %v", err)
	}

	// 7. Verify secret still null in list.
	listResp3, err := authedGetWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list env vars after secret update failed: %v", err)
	}
	defer listResp3.Body.Close()
	var listBody3 struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(listResp3.Body).Decode(&listBody3); err != nil {
		t.Fatalf("decode list failed: %v", err)
	}
	for _, item := range listBody3.Items {
		if asString(item["key"]) == "DB_PASSWORD" && item["value"] != nil {
			t.Fatalf("expected DB_PASSWORD value still null after secret update, got %v", item["value"])
		}
	}

	// 8. Delete secret var.
	delResp, err := authedDeleteWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env/"+secretVarID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("delete secret env var failed: %v", err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete secret env var status=%d", delResp.StatusCode)
	}

	// 9. Verify only 1 item left.
	listResp4, err := authedGetWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list after delete failed: %v", err)
	}
	defer listResp4.Body.Close()
	var listBody4 struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(listResp4.Body).Decode(&listBody4); err != nil {
		t.Fatalf("decode list failed: %v", err)
	}
	if len(listBody4.Items) != 1 {
		t.Fatalf("expected 1 env var after delete, got %d", len(listBody4.Items))
	}

	// 10. Delete plain var.
	delResp2, err := authedDeleteWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env/"+plainVarID, auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("delete plain env var failed: %v", err)
	}
	defer delResp2.Body.Close()
	if delResp2.StatusCode != http.StatusOK {
		t.Fatalf("delete plain env var status=%d", delResp2.StatusCode)
	}

	// 11. Verify empty.
	listResp5, err := authedGetWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env", auth.AccessToken, headers)
	if err != nil {
		t.Fatalf("list after all deletes failed: %v", err)
	}
	defer listResp5.Body.Close()
	var listBody5 struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(listResp5.Body).Decode(&listBody5); err != nil {
		t.Fatalf("decode list failed: %v", err)
	}
	if len(listBody5.Items) != 0 {
		t.Fatalf("expected 0 env vars after all deletes, got %d", len(listBody5.Items))
	}

	// 12. Duplicate key returns error.
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env", auth.AccessToken, headers,
		map[string]any{"key": "DUP_KEY", "value": "a", "isSecret": false}, &map[string]any{}, http.StatusCreated); err != nil {
		t.Fatalf("create first DUP_KEY failed: %v", err)
	}
	dupErr := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env", auth.AccessToken, headers,
		map[string]any{"key": "DUP_KEY", "value": "b", "isSecret": false}, &map[string]any{}, http.StatusBadRequest)
	if dupErr == nil {
		t.Fatalf("expected duplicate key to return 400")
	}

	// 13. Invalid key returns error.
	invalidErr := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/env", auth.AccessToken, headers,
		map[string]any{"key": "123BAD", "value": "x", "isSecret": false}, &map[string]any{}, http.StatusBadRequest)
	if invalidErr == nil {
		t.Fatalf("expected invalid key to return 400")
	}
}
