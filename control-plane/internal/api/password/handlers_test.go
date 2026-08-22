package password

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newHandler returns a handler over the shared test DB.
func newHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(testdb.Get(t))
}

func postJSON(t *testing.T, h *Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	switch {
	case strings.HasSuffix(path, "/send-reset-password"):
		h.SendResetPassword(rec, req)
	case strings.HasSuffix(path, "/reset-password"):
		h.ResetPassword(rec, req)
	default:
		t.Fatalf("unknown path %q", path)
	}
	return rec
}

// TestSendResetPasswordValidationAndLifecycle covers payload validation, the
// privacy-preserving unknown-email path, and token persistence for a real user.
func TestSendResetPasswordValidationAndLifecycle(t *testing.T) {
	newHandler(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	userID, err := authSvc.Register(t.Context(), "resetme@test.local", "oldpassword1", "Reset Me")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	h := newHandler(t)

	// Malformed JSON.
	rec := postJSON(t, h, "/api/v1/auth/send-reset-password", "{broken")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d, want 400", rec.Code)
	}
	assertEnvelope(t, rec, http.StatusBadRequest, "bad_request")

	// Empty email after trim.
	rec = postJSON(t, h, "/api/v1/auth/send-reset-password", `{"email":"   "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty email status = %d, want 400", rec.Code)
	}
	assertEnvelope(t, rec, http.StatusBadRequest, "bad_request")

	// Unknown email: same public response, but no token row.
	rec = postJSON(t, h, "/api/v1/auth/send-reset-password", `{"email":"ghost@test.local"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown email status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if _, hasToken := body["token"]; hasToken {
		t.Fatalf("unknown email must not return a token: %s", rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from password_reset_tokens`); n != 0 {
		t.Fatalf("token rows for unknown email = %d, want 0", n)
	}

	// Existing user: response carries the raw token and the DB stores only
	// its SHA-256 hash with a future expiry.
	rec = postJSON(t, h, "/api/v1/auth/send-reset-password", `{"email":"RESETME@test.local"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("known email status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body = map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("token missing for known user: %s", rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from password_reset_tokens where user_id = $1::uuid and token_hash = $2 and expires_at > now()`, userID, common.SHA256Hex(token)); n != 1 {
		t.Fatalf("hashed token rows = %d, want 1", n)
	}
}

// TestResetPasswordValidation covers malformed payloads and weak passwords.
func TestResetPasswordValidation(t *testing.T) {
	h := newHandler(t)
	testdb.TruncateAll(t)

	cases := []struct {
		name string
		body string
		code string
	}{
		{"bad json", "{oops", "bad_request"},
		{"missing password", `{"token":"sometoken"}`, "bad_request"},
		{"short password", `{"token":"sometoken","newPassword":"short"}`, "bad_request"},
	}
	for _, tc := range cases {
		rec := postJSON(t, h, "/api/v1/auth/reset-password", tc.body)
		assertEnvelope(t, rec, http.StatusBadRequest, tc.code)
	}
}

// TestResetPasswordTokenLifecycle walks invalid, expired, and valid tokens,
// asserting password change plus cleanup of tokens and sessions in the DB.
func TestResetPasswordTokenLifecycle(t *testing.T) {
	pool := newHandler(t).Pool
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "lifecycle@test.local"
	userID, err := authSvc.Register(t.Context(), email, "oldpassword1", "Lifecycle")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// A live session that the reset must invalidate.
	if _, _, err := authSvc.Login(t.Context(), email, "oldpassword1"); err != nil {
		t.Fatalf("seed login: %v", err)
	}
	h := newHandler(t)

	// Unknown token -> invalid_token envelope.
	rec := postJSON(t, h, "/api/v1/auth/reset-password", `{"token":"deadbeef","newPassword":"newpassword1"}`)
	assertEnvelope(t, rec, http.StatusBadRequest, "invalid_token")

	// Expired token -> still rejected even though the row exists.
	expired := "expired-raw-token"
	if _, err := pool.Exec(t.Context(), `
		insert into password_reset_tokens(user_id, token_hash, expires_at)
		values ($1::uuid, $2, now() - interval '1 minute')
	`, userID, common.SHA256Hex(expired)); err != nil {
		t.Fatalf("seed expired token: %v", err)
	}
	rec = postJSON(t, h, "/api/v1/auth/reset-password", fmt.Sprintf(`{"token":%q,"newPassword":"newpassword1"}`, expired))
	assertEnvelope(t, rec, http.StatusBadRequest, "invalid_token")
	if got := testdb.QueryCount(t, `select count(*) from sessions where user_id = $1::uuid`, userID); got != 1 {
		t.Fatalf("failed reset touched sessions: rows = %d, want 1", got)
	}

	// Valid token: password changes, token row and all sessions are deleted.
	valid := "valid-raw-token"
	if _, err := pool.Exec(t.Context(), `
		insert into password_reset_tokens(user_id, token_hash, expires_at)
		values ($1::uuid, $2, now() + interval '1 hour')
	`, userID, common.SHA256Hex(valid)); err != nil {
		t.Fatalf("seed valid token: %v", err)
	}
	rec = postJSON(t, h, "/api/v1/auth/reset-password", fmt.Sprintf(`{"token":%q,"newPassword":"newpassword1"}`, valid))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid reset status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	assertEnvelopeStatus(t, rec, http.StatusOK)

	if got := testdb.QueryCount(t, `select count(*) from password_reset_tokens where user_id = $1::uuid and expires_at > now()`, userID); got != 0 {
		t.Fatalf("reset token rows after use = %d, want 0", got)
	}
	if got := testdb.QueryCount(t, `select count(*) from sessions where user_id = $1::uuid`, userID); got != 0 {
		t.Fatalf("session rows after reset = %d, want 0 (sessions invalidated)", got)
	}
	// The new password authenticates; the old one does not.
	if _, _, err := authSvc.Login(t.Context(), email, "oldpassword1"); err == nil {
		t.Fatal("login with old password succeeded after reset")
	}
	if _, _, err := authSvc.Login(t.Context(), email, "newpassword1"); err != nil {
		t.Fatalf("login with reset password: %v", err)
	}
}

// assertEnvelope asserts status and error code on an error envelope.
func assertEnvelope(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	assertEnvelopeStatus(t, rec, status)
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v (%s)", err, rec.Body.String())
	}
	if body["error"] != code {
		t.Fatalf("error = %q, want %q (body %s)", body["error"], code, rec.Body.String())
	}
}

func assertEnvelopeStatus(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
}

// raiseTrigger installs a trigger that aborts the statement with an error,
// simulating database faults; it is dropped when the test finishes.
func raiseTrigger(t *testing.T, table, name, action, whenClause string) {
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

// seedResetToken inserts a live reset token and returns its raw value.
func seedResetToken(t *testing.T, userID string) string {
	t.Helper()
	raw := fmt.Sprintf("raw-%s", strings.ReplaceAll(strings.ToLower(userID)[:8], "-", ""))
	pool := testdb.Get(t)
	if _, err := pool.Exec(t.Context(), `
		insert into password_reset_tokens(user_id, token_hash, expires_at)
		values ($1::uuid, $2, now() + interval '1 hour')
	`, userID, common.SHA256Hex(raw)); err != nil {
		t.Fatalf("seed reset token: %v", err)
	}
	return raw
}

// TestResetPasswordHashingFailure covers bcrypt rejecting oversized passwords.
func TestResetPasswordHashingFailure(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	userID, err := authSvc.Register(t.Context(), "hashfail@test.local", "oldpassword1", "Hash Fail")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	token := seedResetToken(t, userID)

	longPassword := strings.Repeat("x", 80)
	rec := postJSON(t, newHandler(t), "/api/v1/auth/reset-password",
		fmt.Sprintf(`{"token":%q,"newPassword":%q}`, token, longPassword))
	assertEnvelope(t, rec, http.StatusInternalServerError, "internal_error")

	// Nothing changed.
	if _, _, err := authSvc.Login(t.Context(), "hashfail@test.local", "oldpassword1"); err != nil {
		t.Fatalf("old password should still work: %v", err)
	}
}

// TestResetPasswordUpdateFailure injects a users-table update fault.
func TestResetPasswordUpdateFailure(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	userID, err := authSvc.Register(t.Context(), "updfail@test.local", "oldpassword1", "Update Fail")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	token := seedResetToken(t, userID)
	raiseTrigger(t, "users", "cov_no_password_update", "before update",
		"when (old.id = '"+userID+"')")

	rec := postJSON(t, newHandler(t), "/api/v1/auth/reset-password",
		fmt.Sprintf(`{"token":%q,"newPassword":"newpassword1"}`, token))
	assertEnvelope(t, rec, http.StatusInternalServerError, "internal_error")
}

// TestRandomTokenFailure injects a randomness-source failure to prove the
// defensive error branch of randomToken surfaces instead of issuing a token.
func TestRandomTokenFailure(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, fmt.Errorf("entropy exhausted") }
	defer func() { randRead = orig }()

	if _, err := randomToken(32); err == nil {
		t.Fatal("randomToken must fail when the randomness source fails")
	}
}
