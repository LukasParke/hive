package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/lholliger/hive/internal/auth"
)

func testAuthService(t *testing.T) (*auth.Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	log := zap.NewNop().Sugar()
	return auth.NewService(db, log), mock
}

func TestAuthMiddlewareNoCookie(t *testing.T) {
	svc, _ := testAuthService(t)
	handler := Auth(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuthMiddlewareInvalidSession(t *testing.T) {
	svc, mock := testAuthService(t)
	mock.ExpectQuery("SELECT").WithArgs("invalid-token").WillReturnRows(sqlmock.NewRows(nil))

	handler := Auth(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "invalid-token"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuthMiddlewareValidSession(t *testing.T) {
	svc, mock := testAuthService(t)

	expires := time.Now().Add(24 * time.Hour)
	created := time.Now().Add(-1 * time.Hour)

	rows := sqlmock.NewRows([]string{
		"s_id", "s_token", "s_user_id", "s_active_org", "s_expires_at", "s_created_at",
		"u_id", "u_email", "u_name", "u_created_at", "u_updated_at",
	}).AddRow(
		"session-1", "valid-token", "user-1", "org-1", expires, created,
		"user-1", "test@test.com", "Test", created, created,
	)
	mock.ExpectQuery("SELECT").WithArgs("valid-token").WillReturnRows(rows)

	var capturedUser *SessionUser
	var capturedOrgID string
	handler := Auth(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = GetUser(r.Context())
		capturedOrgID = GetOrgID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "valid-token"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedUser)
	assert.Equal(t, "user-1", capturedUser.ID)
	assert.Equal(t, "test@test.com", capturedUser.Email)
	assert.Equal(t, "org-1", capturedOrgID)
}

func TestGetUserFromEmptyContext(t *testing.T) {
	u := GetUser(context.Background())
	assert.Nil(t, u)
}

func TestGetSessionFromEmptyContext(t *testing.T) {
	s := GetSession(context.Background())
	assert.Nil(t, s)
}

func TestGetOrgIDFromEmptyContext(t *testing.T) {
	orgID := GetOrgID(context.Background())
	assert.Empty(t, orgID)
}
