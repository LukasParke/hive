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

var volumeColumns = []string{"id", "project_id", "name", "driver", "driver_opts", "labels", "mount_type", "remote_host", "remote_path", "mount_options", "scope", "status",
	"storage_host_id", "local_path", "ceph_pool", "ceph_image", "ceph_fs_name", "created_at"}

func TestListVolumesHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM volume WHERE project_id`).
		WithArgs("p-1").
		WillReturnRows(sqlmock.NewRows(volumeColumns).
			AddRow("vol-1", "p-1", "data", "local", []byte("{}"), []byte("{}"), "volume", "", "", "", "local", "active",
				"", "", "", "", "", now))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/volumes", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "p-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	ListVolumes(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "data")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListVolumesEmpty(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT .+ FROM volume WHERE project_id`).
		WithArgs("p-1").
		WillReturnRows(sqlmock.NewRows(volumeColumns))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/volumes", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"projectId": "p-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	ListVolumes(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "[]")
}

func TestGetVolumeHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)
	now := time.Now()

	mock.ExpectQuery(`SELECT .+ FROM volume WHERE id`).
		WithArgs("vol-1").
		WillReturnRows(sqlmock.NewRows(volumeColumns).
			AddRow("vol-1", "p-1", "data", "local", []byte("{}"), []byte("{}"), "nfs", "nas.local", "/share", "vers=4", "local", "active",
				"", "", "", "", "", now))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/volumes/vol-1", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"volumeId": "vol-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	GetVolume(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "nas.local")
}

func TestGetVolumeNotFound(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT .+ FROM volume WHERE id`).
		WithArgs("nonexistent").
		WillReturnRows(sqlmock.NewRows(volumeColumns))

	req := httptest.NewRequest("GET", "/api/v1/projects/p-1/volumes/nonexistent", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"volumeId": "nonexistent"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	GetVolume(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestAttachVolumeHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectExec(`INSERT INTO app_volume`).
		WithArgs("app-1", "vol-1", "/data", false).
		WillReturnResult(sqlmock.NewResult(0, 1))

	body := `{"container_path":"/data","read_only":false}`
	req := httptest.NewRequest("POST", "/api/v1/projects/p-1/volumes/vol-1/attach/app-1", strings.NewReader(body))
	req = testutil.RequestWithChiParams(req, map[string]string{"volumeId": "vol-1", "appId": "app-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	AttachVolume(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAttachVolumeMissingPath(t *testing.T) {
	s, _, err := testutil.NewMockStore()
	require.NoError(t, err)

	body := `{"read_only":false}`
	req := httptest.NewRequest("POST", "/api/v1/projects/p-1/volumes/vol-1/attach/app-1", strings.NewReader(body))
	req = testutil.RequestWithChiParams(req, map[string]string{"volumeId": "vol-1", "appId": "app-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	AttachVolume(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestDetachVolumeHandler(t *testing.T) {
	s, mock, err := testutil.NewMockStore()
	require.NoError(t, err)

	mock.ExpectExec(`DELETE FROM app_volume`).
		WithArgs("app-1", "vol-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest("DELETE", "/api/v1/projects/p-1/volumes/vol-1/detach/app-1", nil)
	req = testutil.RequestWithChiParams(req, map[string]string{"volumeId": "vol-1", "appId": "app-1"})
	req = testutil.RequestWithStore(req, s)

	rr := httptest.NewRecorder()
	DetachVolume(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}
