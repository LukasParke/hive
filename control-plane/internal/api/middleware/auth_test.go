package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// keyHash hashes a raw API key the way the middleware does.
func keyHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// insertAPIKey seeds an API key for userID and returns the raw secret.
func insertAPIKey(t *testing.T, userID string) string {
	t.Helper()
	raw := "ik_" + strings.ReplaceAll(strings.ToLower(userID)[:8], "-", "") + "-secret"
	if _, err := testdb.Get(t).Exec(context.Background(), `
		insert into api_keys(user_id, name, token_hash) values ($1::uuid, 'test-key', $2)
	`, userID, keyHash(raw)); err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	return raw
}

// authRouter builds the middleware chain with a downstream handler that
// echoes the authenticated claims as JSON.
func authRouter(t *testing.T) (http.Handler, *[]auth.Claims) {
	t.Helper()
	seen := &[]auth.Claims{}
	r := chi.NewRouter()
	r.Use(WithAuth(testdb.Auth(t), testdb.Get(t)))
	r.Get("/whoami", func(w http.ResponseWriter, req *http.Request) {
		claims, ok := ClaimsFromContext(req.Context())
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		*seen = append(*seen, *claims)
		w.WriteHeader(http.StatusOK)
	})
	return r, seen
}

// TestWithAuthBearerTokens covers missing, malformed, and valid bearer tokens.
func TestWithAuthBearerTokens(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "bearer@test.local"
	if _, err := authSvc.Register(context.Background(), email, "sup3rsecret!", "Bearer"); err != nil {
		t.Fatalf("register: %v", err)
	}
	token, _, err := authSvc.Login(context.Background(), email, "sup3rsecret!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	r, seen := authRouter(t)

	do := func(authz string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
		if authz != "" {
			req.Header.Set("Authorization", authz)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// No credentials at all.
	rec := do("")
	assertUnauthorized(t, rec, "missing bearer token or api key")

	// Garbage token.
	rec = do("Bearer not-a-jwt")
	assertUnauthorized(t, rec, "invalid token")

	// Wrong scheme falls through to the API-key branch with no header -> 401.
	rec = do("Basic dXNlcjpwYXNz")
	assertUnauthorized(t, rec, "missing bearer token or api key")

	// Valid token authenticates.
	before := len(*seen)
	rec = do("Bearer " + token)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid bearer status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if len(*seen) != before+1 {
		t.Fatal("downstream handler did not observe claims")
	}
	last := (*seen)[len(*seen)-1]
	if last.Email != email {
		t.Fatalf("claims email = %q, want %q", last.Email, email)
	}
	if last.UserID == "" {
		t.Fatal("claims UserID empty")
	}
}

// TestWithAuthAPIKeys covers unknown keys, valid keys (with last_used_at
// bookkeeping), and deactivated users.
func TestWithAuthAPIKeys(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	authSvc := testdb.Auth(t)
	email := "apikey@test.local"
	userID, err := authSvc.Register(context.Background(), email, "sup3rsecret!", "Key User")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	r, seen := authRouter(t)

	doWithKey := func(raw string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
		req.Header.Set("X-API-Key", raw)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// Unknown key -> 401.
	if rec := doWithKey("ik_totally-unknown"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown api key status = %d, want 401", rec.Code)
	} else if !strings.Contains(rec.Body.String(), "invalid api key") {
		t.Fatalf("401 body = %s, want invalid api key", rec.Body.String())
	}

	// Valid key -> 200 with claims derived from the DB row.
	raw := insertAPIKey(t, userID)
	if n := testdb.QueryCount(t, `select count(*) from api_keys where user_id = $1::uuid and last_used_at is not null`, userID); n != 0 {
		t.Fatal("last_used_at set before any use")
	}
	rec := doWithKey(raw)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid api key status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	last := (*seen)[len(*seen)-1]
	if last.UserID != userID || last.Email != email {
		t.Fatalf("claims = %+v, want user %s/%s", last, userID, email)
	}
	if n := testdb.QueryCount(t, `select count(*) from api_keys where user_id = $1::uuid and last_used_at is not null`, userID); n != 1 {
		t.Fatalf("last_used_at rows after use = %d, want 1", n)
	}

	// Same key for a deactivated user -> 401.
	if _, err := pool.Exec(context.Background(), `update users set is_active = false where id = $1::uuid`, userID); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}
	rec = doWithKey(raw)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("inactive-user api key status = %d, want 401", rec.Code)
	}
	assertUnauthorized(t, rec, "invalid api key")
}

// TestClaimsFromContextDelegates pins the deprecated wrapper to apicxt.
func TestClaimsFromContextDelegates(t *testing.T) {
	if _, ok := ClaimsFromContext(context.Background()); ok {
		t.Fatal("bare context reported claims")
	}
	want := &auth.Claims{UserID: "u9", Email: "u9@test.local"}
	got, ok := ClaimsFromContext(withTestClaims(context.Background(), want))
	if !ok || got != want {
		t.Fatalf("ClaimsFromContext = (%v, %v), want injected claims", got, ok)
	}
}

func assertUnauthorized(t *testing.T, rec *httptest.ResponseRecorder, message string) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v (%s)", err, rec.Body.String())
	}
	if body["error"] != "unauthorized" || body["message"] != message {
		t.Fatalf("envelope = %v, want unauthorized/%s", body, message)
	}
}
