package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddlewareNoCookie(t *testing.T) {
	handler := Auth("http://auth.test")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuthMiddlewareInvalidSession(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer authServer.Close()

	handler := Auth(authServer.URL)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "invalid-token"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAuthMiddlewareValidSession(t *testing.T) {
	sessionData := SessionData{
		User: SessionUser{ID: "user-1", Email: "test@test.com", Name: "Test"},
	}
	sessionData.Session.ID = "session-1"
	sessionData.Session.OrgID = "org-1"

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie := r.Header.Get("Cookie")
		assert.Contains(t, cookie, "better-auth.session_token=valid-token")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessionData)
	}))
	defer authServer.Close()

	var capturedUser *SessionUser
	var capturedOrgID string
	handler := Auth(authServer.URL)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = GetUser(r.Context())
		capturedOrgID = GetOrgID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedUser)
	assert.Equal(t, "user-1", capturedUser.ID)
	assert.Equal(t, "test@test.com", capturedUser.Email)
	assert.Equal(t, "org-1", capturedOrgID)
}

func TestAuthMiddlewareEmptyUserID(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SessionData{})
	}))
	defer authServer.Close()

	handler := Auth(authServer.URL)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "bad-token"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
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
