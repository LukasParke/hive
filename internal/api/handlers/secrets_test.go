package handlers

import (
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

func TestListSecretsHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM secret WHERE project_id`).
		WithArgs("p-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "docker_secret_id", "description", "created_at", "updated_at"}).
			AddRow("sec-1", "p-1", "db-pass", "docker-id", "database password", now, now))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/secrets", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "p-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	ListSecrets(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "db-pass")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListSecretsEmpty(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT .+ FROM secret WHERE project_id`).
		WithArgs("p-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "docker_secret_id", "description", "created_at", "updated_at"}))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/secrets", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "p-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	ListSecrets(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "[]")
}

func TestDeleteSecretHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM secret WHERE id`).
		WithArgs("sec-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "docker_secret_id", "description", "created_at", "updated_at"}).
			AddRow("sec-1", "p-1", "db-pass", "", "desc", now, now))

	mock.ExpectExec(`DELETE FROM secret`).
		WithArgs("sec-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest("DELETE", "/api/v1/projects/p-1/secrets/sec-1", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"secretId": "sec-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	DeleteSecret(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteSecretNotFound(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT .+ FROM secret WHERE id`).
		WithArgs("nonexistent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "docker_secret_id", "description", "created_at", "updated_at"}))

	req := httptest.NewRequest("DELETE", "/api/v1/projects/p-1/secrets/nonexistent", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"secretId": "nonexistent"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	DeleteSecret(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestAttachSecretHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectExec(`INSERT INTO app_secret`).
		WithArgs("app-1", "sec-1", "password.txt", "0", "0", 292).
		WillReturnResult(sqlmock.NewResult(0, 1))

	body := `{"target":"password.txt"}`
	req := httptest.NewRequest("POST", "/api/v1/projects/p-1/secrets/sec-1/attach/app-1", strings.NewReader(body))
	req = testutil.RequestWithChiParams(req, map[string]string{"secretId": "sec-1", "appId": "app-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	AttachSecret(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteSecretWithDockerID(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM secret WHERE id`).
		WithArgs("sec-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "docker_secret_id", "description", "created_at", "updated_at"}).
			AddRow("sec-1", "p-1", "db-pass", "swarm-docker-id-123", "desc", now, now))

	mock.ExpectExec(`DELETE FROM secret`).
		WithArgs("sec-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest("DELETE", "/api/v1/projects/p-1/secrets/sec-1", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"secretId": "sec-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	DeleteSecret(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDetachSecretHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectExec(`DELETE FROM app_secret`).
		WithArgs("app-1", "sec-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest("DELETE", "/api/v1/projects/p-1/secrets/sec-1/detach/app-1", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"secretId": "sec-1", "appId": "app-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	DetachSecret(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}
