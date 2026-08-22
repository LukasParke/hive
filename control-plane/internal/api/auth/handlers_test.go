package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newRouter mounts every auth handler exactly like server.go does.
func newRouter(t *testing.T, h *Handler) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/v1/auth/register", h.RegisterUser)
	r.Post("/api/v1/auth/login", h.Login)
	r.Post("/api/v1/auth/refresh", h.Refresh)
	r.Post("/api/v1/auth/logout", h.Logout)
	return r
}

func postJSON(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body not json: %v (%s)", err, rec.Body.String())
	}
	return m
}

// sessionCount returns the number of live sessions for a user.
func sessionCount(t *testing.T, userID string) int {
	t.Helper()
	return testdb.QueryCount(t, `select count(*) from sessions where user_id = $1::uuid`, userID)
}

// TestRegisterUserValidationAndSuccess covers payload validation plus the
// happy path and the duplicate-email error branch.
func TestRegisterUserValidationAndSuccess(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(testdb.Auth(t))
	r := newRouter(t, h)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"bad json", "{nope", http.StatusBadRequest},
		{"missing email", `{"password":"sup3rsecret!"}`, http.StatusBadRequest},
		{"missing password", `{"email":"a@test.local"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/register", tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s: status = %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}

	rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/register", `{"email":"reg@test.local","password":"sup3rsecret!","displayName":"Reg"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("register body missing id: %s", rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from users where email = 'reg@test.local'`); n != 1 {
		t.Fatalf("users rows = %d, want 1", n)
	}

	// Duplicate email surfaces as 400 from the service layer.
	rec = postJSON(t, r, http.MethodPost, "/api/v1/auth/register", `{"email":"reg@test.local","password":"anotherpass1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate register status = %d, want 400", rec.Code)
	}
}

// TestLoginFlows covers validation, wrong password, unknown email, and the
// success path that persists a session row.
func TestLoginFlows(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	mustRegister(t, authSvc, "login@test.local")
	h := NewHandler(authSvc)
	r := newRouter(t, h)

	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"bad json", "{", http.StatusBadRequest},
		{"missing fields", `{"email":"login@test.local"}`, http.StatusBadRequest},
		{"wrong password", `{"email":"login@test.local","password":"wrongpass123"}`, http.StatusUnauthorized},
		{"unknown email", `{"email":"ghost@test.local","password":"sup3rsecret!"}`, http.StatusUnauthorized},
	} {
		rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/login", tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s: status = %d, want %d (%s)", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}

	userID := testdb.QueryCount(t, `select count(*) from users where email = 'login@test.local'`)
	if userID != 1 {
		t.Fatalf("seed user missing")
	}
	rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/login", `{"email":"login@test.local","password":"sup3rsecret!"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	access, _ := body["accessToken"].(string)
	refresh, _ := body["refreshToken"].(string)
	if access == "" || refresh == "" {
		t.Fatalf("login body missing tokens: %s", rec.Body.String())
	}
	if _, err := authSvc.ParseAccessToken(access); err != nil {
		t.Fatalf("issued access token does not parse: %v", err)
	}
	if got := testdb.QueryCount(t, `select count(*) from sessions s join users u on u.id = s.user_id where u.email = 'login@test.local'`); got != 1 {
		t.Fatalf("session rows after login = %d, want 1", got)
	}
}

func mustRegister(t *testing.T, svc *auth.Service, email string) string {
	t.Helper()
	id, err := svc.Register(t.Context(), email, "sup3rsecret!", email)
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return id
}

// TestRefreshRotatesAndInvalidatesOldToken exercises rotation end to end.
func TestRefreshRotatesAndInvalidatesOldToken(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "refresh@test.local"
	mustRegister(t, authSvc, email)
	_, refresh, err := authSvc.Login(t.Context(), email, "sup3rsecret!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	h := NewHandler(authSvc)
	r := newRouter(t, h)

	if rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/refresh", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty refresh status = %d, want 400", rec.Code)
	}
	if rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"bogus-token"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bogus refresh status = %d, want 401", rec.Code)
	}

	rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"`+refresh+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	nextRefresh, _ := body["refreshToken"].(string)
	if nextRefresh == "" || nextRefresh == refresh {
		t.Fatalf("refresh did not rotate token: %s", rec.Body.String())
	}
	if access, _ := body["accessToken"].(string); access == "" {
		t.Fatal("refresh response missing accessToken")
	}

	// The old refresh token is dead after rotation.
	rec = postJSON(t, r, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"`+refresh+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed old refresh status = %d, want 401", rec.Code)
	}
}

// TestLogoutDeletesSession covers validation, unknown tokens, and deletion.
func TestLogoutDeletesSession(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	userID := mustRegister(t, authSvc, "logout@test.local")
	_, refresh, err := authSvc.Login(t.Context(), "logout@test.local", "sup3rsecret!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	h := NewHandler(authSvc)
	r := newRouter(t, h)

	if rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/logout", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty logout status = %d, want 400", rec.Code)
	}
	// Unknown token still deletes zero rows without error -> 200.
	if rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/logout", `{"refreshToken":"nope"}`); rec.Code != http.StatusOK {
		t.Fatalf("unknown-token logout status = %d, want 200", rec.Code)
	}
	if got := sessionCount(t, userID); got != 1 {
		t.Fatalf("session rows after no-op logout = %d, want 1", got)
	}

	rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/logout", `{"refreshToken":"`+refresh+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if got := sessionCount(t, userID); got != 0 {
		t.Fatalf("session rows after logout = %d, want 0", got)
	}
	// Refreshing with the logged-out token now fails.
	if rec := postJSON(t, r, http.MethodPost, "/api/v1/auth/refresh", `{"refreshToken":"`+refresh+`"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout refresh status = %d, want 401", rec.Code)
	}
}

// TestMeRequiresClaims asserts 401 without claims and identity echo with them,
// routed through the real WithAuth middleware like server.go.
func TestMeRequiresClaims(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "me@test.local"
	mustRegister(t, authSvc, email)
	token, _, err := authSvc.Login(t.Context(), email, "sup3rsecret!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(authSvc, pool))
		gr.Get("/api/v1/auth/me", NewHandler(authSvc).Me)
	})

	rec := postJSON(t, r, http.MethodGet, "/api/v1/auth/me", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /me status = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/me status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	if body["email"] != email {
		t.Fatalf("/me email = %v, want %s", body["email"], email)
	}
	if id, _ := body["id"].(string); id == "" {
		t.Fatalf("/me id missing: %s", rec.Body.String())
	}
}

// TestMeWithInjectedClaims covers the claims-present branch via direct context
// injection.
func TestMeWithInjectedClaims(t *testing.T) {
	h := NewHandler(nil)
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(apicxt.WithClaims(req.Context(), &auth.Claims{UserID: "u-42", Email: "u-42@test.local"})))
		})
	}).Get("/me", h.Me)

	rec := postJSON(t, r, http.MethodGet, "/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decodeMap(t, rec)
	if body["id"] != "u-42" || body["email"] != "u-42@test.local" {
		t.Fatalf("body = %v, want u-42 claims", body)
	}
}

// TestMeWithoutClaims hits Me directly with no auth middleware.
func TestMeWithoutClaims(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/api/v1/auth/me", NewHandler(nil).Me)
	rec := postJSON(t, r, http.MethodGet, "/api/v1/auth/me", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("claims-less /me status = %d, want 401", rec.Code)
	}
}

// raiseTrigger installs a trigger that aborts the statement with an error,
// simulating database faults; it is dropped when the test finishes.
func raiseTrigger(t *testing.T, table, name, whenClause string) {
	t.Helper()
	pool := testdb.Get(t)
	if _, err := pool.Exec(t.Context(), `create or replace function cov_raise_exception() returns trigger as $f$ begin raise exception 'injected fault'; end $f$ language plpgsql`); err != nil {
		t.Fatalf("create trigger function: %v", err)
	}
	stmt := "create trigger " + name + " before delete on " + table + " for each row " + whenClause + " execute function cov_raise_exception()"
	if _, err := pool.Exec(t.Context(), stmt); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "drop trigger if exists "+name+" on "+table)
	})
}

// TestLogoutServiceFailure maps session-deletion failures onto 400.
func TestLogoutServiceFailure(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	mustRegister(t, authSvc, "logoutfail@test.local")
	_, refresh, err := authSvc.Login(t.Context(), "logoutfail@test.local", "sup3rsecret!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	raiseTrigger(t, "sessions", "cov_no_session_delete", "")

	rec := postJSON(t, newRouter(t, NewHandler(authSvc)), http.MethodPost, "/api/v1/auth/logout", `{"refreshToken":"`+refresh+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("failing logout status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}
