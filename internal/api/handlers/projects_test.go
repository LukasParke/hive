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

func TestCreateProjectHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`INSERT INTO project`).
		WithArgs("test-project", "org-1", "description").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("p-1", now, now))

	body := `{"name":"test-project","description":"description"}`
	req := httptest.NewRequest("POST", "/api/v1/projects", strings.NewReader(body))
	req = testutil.RequestWithStore(req, s)
	req = testutil.RequestWithSession(req, &testutil.TestUser, "org-1")

	rr := httptest.NewRecorder()
	CreateProject(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	assert.Equal(t, "test-project", resp["name"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateProjectMissingName(t *testing.T) {
	s, _, err := testutil.NewMockStore()
	require.NoError(t, err)

	body := `{"description":"no name"}`
	req := httptest.NewRequest("POST", "/api/v1/projects", strings.NewReader(body))
	req = testutil.RequestWithStore(req, s)
	req = testutil.RequestWithSession(req, &testutil.TestUser, "org-1")

	rr := httptest.NewRecorder()
	CreateProject(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestListProjectsHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM project WHERE org_id`).
		WithArgs("org-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "org_id", "description", "created_at", "updated_at"}).
			AddRow("p-1", "proj-a", "org-1", "", now, now))

	req := httptest.NewRequest("GET", "/api/v1/projects", nil)
	req = testutil.RequestWithStore(req, s)
	req = testutil.RequestWithSession(req, &testutil.TestUser, "org-1")

	rr := httptest.NewRecorder()
	ListProjects(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetProjectHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM project WHERE`).
		WithArgs("p-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "org_id", "description", "created_at", "updated_at"}).
			AddRow("p-1", "my-project", "org-1", "", now, now))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "p-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	GetProject(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetProjectNotFound(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT .+ FROM project WHERE`).
		WithArgs("nonexistent").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "org_id", "description", "created_at", "updated_at"}))

	req := httptest.NewRequest("GET", "/api/v1/projects/nonexistent", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "nonexistent"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	GetProject(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeleteProjectHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectExec(`DELETE FROM project`).
		WithArgs("p-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest("DELETE", "/api/v1/projects/p-1", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "p-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	DeleteProject(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}
