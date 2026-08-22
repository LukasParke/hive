package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// testRouter returns a fresh chi mux.
func testRouter(t *testing.T) *chi.Mux {
	t.Helper()
	return chi.NewRouter()
}

// withClaims attaches claims via the shared apicxt helper.
func withClaims(c context.Context, claims *auth.Claims) context.Context {
	return apicxt.WithClaims(c, claims)
}
func claimsMiddleware(claims *auth.Claims) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := withClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newRouter(t *testing.T, claims *auth.Claims) http.Handler {
	t.Helper()
	h := NewHandler(testdb.Get(t))
	r := testRouter(t)
	r.Use(claimsMiddleware(claims))
	r.Get("/api/v1/profile", h.GetProfile)
	r.Put("/api/v1/profile", h.UpdateProfile)
	r.Post("/api/v1/profile/change-password", h.ChangePassword)
	return r
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
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

// TestGetProfile covers unauthorized, invalid user id, unknown user, and the
// success path.
func TestGetProfile(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "profile@test.local"
	userID, err := authSvc.Register(t.Context(), email, "sup3rsecret!", "Prof Ile")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// No claims -> 401 envelope.
	noAuth := testRouter(t)
	noAuth.Get("/api/v1/profile", NewHandler(testdb.Get(t)).GetProfile)
	rec := do(t, noAuth, http.MethodGet, "/api/v1/profile", "")
	assertEnvelope(t, rec, http.StatusUnauthorized, "unauthorized")

	r := newRouter(t, &auth.Claims{UserID: userID, Email: email})
	rec = do(t, r, http.MethodGet, "/api/v1/profile", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get profile status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	if body["email"] != email {
		t.Fatalf("profile email = %v, want %s (body %s)", body["email"], email, rec.Body.String())
	}

	// Malformed user id in the claims -> 400 bad_request.
	badID := newRouter(t, &auth.Claims{UserID: "not-a-uuid", Email: email})
	rec = do(t, badID, http.MethodGet, "/api/v1/profile", "")
	assertEnvelope(t, rec, http.StatusBadRequest, "bad_request")

	// Well-formed id that is not in the users table -> 404 not_found.
	ghost := newRouter(t, &auth.Claims{UserID: "00000000-0000-0000-0000-0000000000ff", Email: email})
	rec = do(t, ghost, http.MethodGet, "/api/v1/profile", "")
	assertEnvelope(t, rec, http.StatusNotFound, "not_found")
}

// TestUpdateProfile covers validation errors and a successful display-name
// update visible through GetProfile.
func TestUpdateProfile(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "update@test.local"
	userID, err := authSvc.Register(t.Context(), email, "sup3rsecret!", "Before")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	r := newRouter(t, &auth.Claims{UserID: userID, Email: email})

	rec := do(t, r, http.MethodPut, "/api/v1/profile", "{nope")
	assertEnvelope(t, rec, http.StatusBadRequest, "bad_request")

	rec = do(t, r, http.MethodPut, "/api/v1/profile", `{"displayName":"After"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update profile status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	if body["display_name"] != "After" {
		t.Fatalf("display_name = %v, want After (body %s)", body["display_name"], rec.Body.String())
	}
}

// TestChangePassword covers payload validation, unknown user, wrong current
// password, and the success path including session invalidation.
func TestChangePassword(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "changepw@test.local"
	userID, err := authSvc.Register(t.Context(), email, "oldpassword1", "Change")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, _, err := authSvc.Login(t.Context(), email, "oldpassword1"); err != nil {
		t.Fatalf("seed login: %v", err)
	}
	r := newRouter(t, &auth.Claims{UserID: userID, Email: email})

	for _, tc := range []struct {
		name string
		body string
	}{
		{"bad json", "{oops"},
		{"missing current", `{"newPassword":"newpassword1"}`},
		{"missing new", `{"currentPassword":"oldpassword1"}`},
	} {
		rec := do(t, r, http.MethodPost, "/api/v1/profile/change-password", tc.body)
		assertEnvelope(t, rec, http.StatusBadRequest, "bad_request")
	}

	// Unknown user behind valid-format claims -> 401.
	ghost := newRouter(t, &auth.Claims{UserID: "00000000-0000-0000-0000-0000000000fe", Email: email})
	rec := do(t, ghost, http.MethodPost, "/api/v1/profile/change-password", `{"currentPassword":"x","newPassword":"newpassword1"}`)
	assertEnvelope(t, rec, http.StatusUnauthorized, "unauthorized")

	// Wrong current password -> 401.
	rec = do(t, r, http.MethodPost, "/api/v1/profile/change-password", `{"currentPassword":"wrongpass12","newPassword":"newpassword1"}`)
	assertEnvelope(t, rec, http.StatusUnauthorized, "unauthorized")
	if _, _, err := authSvc.Login(t.Context(), email, "oldpassword1"); err != nil {
		t.Fatalf("old password should still work after failed change: %v", err)
	}

	// Success: password rotates and all sessions are invalidated.
	rec = do(t, r, http.MethodPost, "/api/v1/profile/change-password", `{"currentPassword":"oldpassword1","newPassword":"newpassword1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var n int
	if err := pool.QueryRow(t.Context(), `select count(*) from sessions where user_id = $1::uuid`, userID).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Fatalf("session rows after change = %d, want 0", n)
	}
	if _, _, err := authSvc.Login(t.Context(), email, "newpassword1"); err != nil {
		t.Fatalf("login with changed password: %v", err)
	}
	if _, _, err := authSvc.Login(t.Context(), email, "oldpassword1"); err == nil {
		t.Fatal("login with old password succeeded after change")
	}
}

// assertEnvelope asserts status plus error code on an error envelope body.
func assertEnvelope(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v (%s)", err, rec.Body.String())
	}
	if body["error"] != code {
		t.Fatalf("error = %q, want %q (message %q)", body["error"], code, body["message"])
	}
}

// execFaultTrigger installs a trigger aborting the given action on a table.
func execFaultTrigger(t *testing.T, table, name, action, whenClause string) {
	t.Helper()
	pool := testdb.Get(t)
	if _, err := pool.Exec(t.Context(), `create or replace function cov_raise_exception() returns trigger as $f$ begin raise exception 'injected fault'; end $f$ language plpgsql`); err != nil {
		t.Fatalf("create trigger function: %v", err)
	}
	stmt := "create trigger " + name + " " + action + " on " + table + " for each row " + whenClause + " execute function cov_raise_exception()"
	if _, err := pool.Exec(t.Context(), stmt); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "drop trigger if exists "+name+" on "+table)
	})
}

// TestUpdateProfileAuthAndValidation covers the claims and uuid branches that
// the success-path test cannot reach.
func TestUpdateProfileAuthAndValidation(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(testdb.Get(t))

	// No claims -> 401 envelope.
	noAuth := testRouter(t)
	noAuth.Put("/api/v1/profile", h.UpdateProfile)
	rec := do(t, noAuth, http.MethodPut, "/api/v1/profile", `{"displayName":"X"}`)
	assertEnvelope(t, rec, http.StatusUnauthorized, "unauthorized")

	// Malformed user id in claims -> 400 bad_request.
	badID := newRouter(t, &auth.Claims{UserID: "not-a-uuid", Email: "x@test.local"})
	rec = do(t, badID, http.MethodPut, "/api/v1/profile", `{"displayName":"X"}`)
	assertEnvelope(t, rec, http.StatusBadRequest, "bad_request")
}

// TestChangePasswordAuthAndHashing covers the claims branch and bcrypt
// rejecting oversized passwords.
func TestChangePasswordAuthAndHashing(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "hashing@test.local"
	userID, err := authSvc.Register(t.Context(), email, "oldpassword1", "Hashing")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	noAuth := testRouter(t)
	noAuth.Post("/api/v1/profile/change-password", NewHandler(testdb.Get(t)).ChangePassword)
	rec := do(t, noAuth, http.MethodPost, "/api/v1/profile/change-password",
		`{"currentPassword":"a","newPassword":"b"}`)
	assertEnvelope(t, rec, http.StatusUnauthorized, "unauthorized")

	r := newRouter(t, &auth.Claims{UserID: userID, Email: email})
	longPassword := strings.Repeat("y", 80)
	rec = do(t, r, http.MethodPost, "/api/v1/profile/change-password",
		fmt.Sprintf(`{"currentPassword":"oldpassword1","newPassword":%q}`, longPassword))
	assertEnvelope(t, rec, http.StatusInternalServerError, "internal_error")
}

// TestUpdateProfileUpdateFailure injects a display-name update fault.
func TestUpdateProfileUpdateFailure(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "updfail@test.local"
	userID, err := authSvc.Register(t.Context(), email, "sup3rsecret!", "Before")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	execFaultTrigger(t, "users", "cov_no_display_update", "before update",
		"when (old.display_name is distinct from new.display_name)")

	r := newRouter(t, &auth.Claims{UserID: userID, Email: email})
	rec := do(t, r, http.MethodPut, "/api/v1/profile", `{"displayName":"After"}`)
	assertEnvelope(t, rec, http.StatusBadRequest, "bad_request")
}

// TestUpdateProfileRefetchFailure deletes the row after a successful update so
// the follow-up profile fetch fails with internal_error.
func TestUpdateProfileRefetchFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "refetchfail@test.local"
	userID, err := authSvc.Register(t.Context(), email, "sup3rsecret!", "Before")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		create or replace function cov_self_destruct() returns trigger as $f$
		begin delete from users where id = new.id; return null; end
		$f$ language plpgsql
	`); err != nil {
		t.Fatalf("create self-destruct fn: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		create trigger cov_refetch_fail after update of display_name on users
		for each row when (new.id = '`+userID+`') execute function cov_self_destruct()
	`); err != nil {
		t.Fatalf("create self-destruct trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "drop trigger if exists cov_refetch_fail on users")
	})

	r := newRouter(t, &auth.Claims{UserID: userID, Email: email})
	rec := do(t, r, http.MethodPut, "/api/v1/profile", `{"displayName":"After"}`)
	assertEnvelope(t, rec, http.StatusInternalServerError, "internal_error")
}

// TestChangePasswordUpdateFailure injects a password-update fault.
func TestChangePasswordUpdateFailure(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "pwupdfail@test.local"
	userID, err := authSvc.Register(t.Context(), email, "oldpassword1", "PW Update Fail")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, _, err := authSvc.Login(t.Context(), email, "oldpassword1"); err != nil {
		t.Fatalf("seed login: %v", err)
	}
	execFaultTrigger(t, "users", "cov_no_pw_update", "before update",
		"when (old.password_hash is distinct from new.password_hash)")

	r := newRouter(t, &auth.Claims{UserID: userID, Email: email})
	rec := do(t, r, http.MethodPost, "/api/v1/profile/change-password",
		`{"currentPassword":"oldpassword1","newPassword":"newpassword1"}`)
	assertEnvelope(t, rec, http.StatusBadRequest, "bad_request")
}
