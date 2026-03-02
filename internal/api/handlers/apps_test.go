package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lholliger/hive/internal/testutil"
)

func TestCreateAppHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`INSERT INTO app`).
		WithArgs("p-1", "my-app", "image", "nginx:latest", "", "main", "Dockerfile", "", 3000, 1, []byte(nil), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("app-1", now, now))

	body := `{"name":"my-app","deploy_type":"image","image":"nginx:latest"}`
	req := httptest.NewRequest("POST", "/api/v1/projects/p-1/apps", strings.NewReader(body))
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "p-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	CreateApp(nil)(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Equal(t, "my-app", resp["name"])
}

func TestCreateAppMissingName(t *testing.T) {
	s, _, err := testutil.NewMockStore()
	require.NoError(t, err)

	body := `{"deploy_type":"image"}`
	req := httptest.NewRequest("POST", "/api/v1/projects/p-1/apps", strings.NewReader(body))
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "p-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	CreateApp(nil)(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestListAppsHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM app WHERE project_id`).
		WithArgs("p-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "deploy_type", "image", "git_repo", "git_branch", "dockerfile_path", "domain", "port", "replicas", "env_encrypted", "status", "cpu_limit", "memory_limit", "health_check_path", "health_check_interval", "homepage_labels", "extra_labels", "placement_constraints", "placement_preferences", "update_strategy", "update_parallelism", "update_delay", "update_failure_action", "update_order", "build_cache_enabled", "auto_deploy_branch", "preview_environments", "template_name", "template_version", "created_at", "updated_at"}).
			AddRow("a1", "p-1", "app-a", "image", "nginx", "", "", "", "", 80, 1, nil, "running", 0.0, int64(0), "", 30, []byte("{}"), []byte("{}"), []byte("[]"), []byte("[]"), "rolling", 1, "5s", "rollback", "stop-first", true, "main", false, "", "", now, now))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/apps", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "p-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	ListApps(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAppHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM app WHERE`).
		WithArgs("app-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "deploy_type", "image", "git_repo", "git_branch", "dockerfile_path", "domain", "port", "replicas", "env_encrypted", "status", "cpu_limit", "memory_limit", "health_check_path", "health_check_interval", "homepage_labels", "extra_labels", "placement_constraints", "placement_preferences", "update_strategy", "update_parallelism", "update_delay", "update_failure_action", "update_order", "template_name", "template_version", "created_at", "updated_at"}).
			AddRow("app-1", "p-1", "my-app", "image", "nginx", "", "main", "", "", 3000, 1, nil, "running", 0.0, int64(0), "", 30, []byte("{}"), []byte("{}"), []byte("[]"), []byte("[]"), "rolling", 1, "5s", "rollback", "stop-first", "", "", now, now))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/apps/app-1", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"appId": "app-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	GetApp(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAppNotFound(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT .+ FROM app WHERE`).
		WithArgs("nonexistent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "deploy_type", "image", "git_repo", "git_branch", "dockerfile_path", "domain", "port", "replicas", "env_encrypted", "status", "cpu_limit", "memory_limit", "health_check_path", "health_check_interval", "homepage_labels", "extra_labels", "placement_constraints", "placement_preferences", "update_strategy", "update_parallelism", "update_delay", "update_failure_action", "update_order", "template_name", "template_version", "created_at", "updated_at"}))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/apps/nonexistent", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"appId": "nonexistent"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	GetApp(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestUpdateAppDomainsHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectExec(`UPDATE app SET domain`).
		WithArgs("new.example.com", "app-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	body := `{"domain":"new.example.com"}`
	req := httptest.NewRequest("PUT", "/api/v1/projects/p-1/apps/app-1/domains", strings.NewReader(body))
	req = testutil.RequestWithChiParams(req, map[string]string{"appId": "app-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	UpdateAppDomains(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListDeploymentsHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM deployment WHERE app_id`).
		WithArgs("app-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "app_id", "status", "commit_sha", "image_digest", "logs", "started_at", "finished_at"}).
			AddRow("dep-1", "app-1", "success", "abc123", "", "ok", now, now))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/apps/app-1/deployments", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"appId": "app-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	ListDeployments(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}
