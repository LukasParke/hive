package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lholliger/hive/internal/testutil"
)

func TestListDatabasesHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM managed_database WHERE project_id`).
		WithArgs("p-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "db_type", "version", "status", "connection_encrypted", "created_at"}).
			AddRow("db-1", "p-1", "mydb", "postgres", "16", "running", nil, now))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/databases", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "p-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	ListDatabases(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "mydb")
	assert.NoError(t, mock.ExpectationsWereMet())
}
