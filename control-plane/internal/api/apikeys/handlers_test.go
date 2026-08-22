package apikeys

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/luke/hive/control-plane/internal/api/common"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// claimsMiddleware injects the given claims into every request, standing in
// for the real auth middleware so arbitrary user ids can be simulated.
func claimsMiddleware(claims *auth.Claims) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(apicxt.WithClaims(r.Context(), claims)))
		})
	}
}

func newRouter(t *testing.T, h *Handler, claims *auth.Claims) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	if claims != nil {
		r.Use(claimsMiddleware(claims))
	}
	r.Post("/api/v1/organizations/{id}/api-keys", h.CreateAPIKey)
	r.Get("/api/v1/organizations/{id}/api-keys", h.ListAPIKeys)
	r.Delete("/api/v1/organizations/{id}/api-keys/{keyId}", h.DeleteAPIKey)
	r.Post("/api/v1/organizations/{id}/api-keys/{keyId}/regenerate", h.RegenerateAPIKey)
	return r
}

func do(t *testing.T, h http.Handler, method, path, body string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
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

// keyRow returns (tokenHash, userID, name) for an api key id.
func keyRow(t *testing.T, keyID string) (string, string, string) {
	t.Helper()
	pool := testdb.Get(t)
	var hash, userID, name string
	if err := pool.QueryRow(t.Context(), `
		select token_hash, user_id::text, name from api_keys where id = $1::uuid
	`, keyID).Scan(&hash, &userID, &name); err != nil {
		t.Fatalf("load api key %s: %v", keyID, err)
	}
	return hash, userID, name
}

// insertKey seeds an api key row and returns its id.
func insertKey(t *testing.T, userID, name, rawToken string) string {
	t.Helper()
	pool := testdb.Get(t)
	var id string
	if err := pool.QueryRow(t.Context(), `
		insert into api_keys(user_id, name, token_hash) values ($1::uuid, $2, $3) returning id::text
	`, userID, name, sha256Hex(rawToken)).Scan(&id); err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	return id
}

const ghostOrgID = "11111111-1111-1111-1111-111111111111"

// TestCreateAPIKey covers 401 without claims, RBAC denial for members,
// payload validation, and the success path including hash storage assertions.
func TestCreateAPIKey(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	member := org.AddMember(t, rbac.RoleMember)
	h := NewHandler(pool)

	// No claims -> 401.
	noAuth := newRouter(t, h, nil)
	rec := do(t, noAuth, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/api-keys", `{"name":"ci"}`, org.Headers)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-claims create status = %d, want 401", rec.Code)
	}

	// Member lacks owner/admin -> 403.
	memberRouter := newRouter(t, h, &auth.Claims{UserID: member.UserID, Email: member.Email})
	rec = do(t, memberRouter, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/api-keys", `{"name":"ci"}`, org.Headers)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member create status = %d, want 403", rec.Code)
	}

	ownerRouter := newRouter(t, h, &auth.Claims{UserID: org.UserID, Email: org.Email})

	// Missing name -> 400.
	rec = do(t, ownerRouter, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/api-keys", `{}`, org.Headers)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty-name create status = %d, want 400", rec.Code)
	}

	// Only the SHA-256 hash of the raw token is ever stored.
	rawToken := "raw-secret-token-for-hash-check"
	seededID := insertKey(t, org.UserID, "preexisting", rawToken)
	if storedHash, _, _ := keyRow(t, seededID); storedHash == rawToken {
		t.Fatal("api_keys must not store the raw token")
	}
	if storedHash, _, _ := keyRow(t, seededID); storedHash != sha256Hex(rawToken) {
		t.Fatalf("stored hash = %s, want sha256 of raw token", storedHash)
	}

	rec = do(t, ownerRouter, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/api-keys", `{"name":"ci"}`, org.Headers)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("create body missing token: %s", rec.Body.String())
	}

	var count int
	if err := pool.QueryRow(t.Context(), `
		select count(*) from api_keys where user_id = $1::uuid and name = 'ci' and token_hash = $2
	`, org.UserID, sha256Hex(token)).Scan(&count); err != nil {
		t.Fatalf("verify stored key: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows matching created key hash = %d, want 1", count)
	}
}

// TestListAPIKeys covers RBAC gating, the active-org mismatch check, and list
// contents scoped to the organization.
func TestListAPIKeys(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrgWithRole(t, rbac.RoleOwner)

	h := NewHandler(pool)
	router := newRouter(t, h, &auth.Claims{UserID: orgA.UserID, Email: orgA.Email})
	headers := http.Header{}
	headers.Set("X-Organization-Id", orgA.OrgID)

	// Keys from two orgs exist; listing org A must show only its members' keys.
	insertKey(t, orgA.UserID, "a-key", "raw-a")
	insertKey(t, orgB.UserID, "b-key", "raw-b")

	rec := do(t, router, http.MethodGet, "/api/v1/organizations/"+orgA.OrgID+"/api-keys", "", headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("org A items = %d, want 1 (%s)", len(items), rec.Body.String())
	}
	first, _ := items[0].(map[string]any)
	if first["name"] != "a-key" {
		t.Fatalf("item = %v, want a-key", first)
	}
	if first["userId"] != orgA.UserID {
		t.Fatalf("item userId = %v, want %s", first["userId"], orgA.UserID)
	}

	// Caller is an admin in the header org but the URL names a different org
	// -> forbidden mismatch.
	if _, err := pool.Exec(t.Context(), `
		insert into organization_members(organization_id, user_id, role) values ($1::uuid, $2::uuid, 'admin')
	`, orgB.OrgID, orgA.UserID); err != nil {
		t.Fatalf("cross-org membership: %v", err)
	}
	headers.Set("X-Organization-Id", orgB.OrgID)
	rec = do(t, router, http.MethodGet, "/api/v1/organizations/"+orgA.OrgID+"/api-keys", "", headers)
	assertEnvelopeCode(t, rec, http.StatusForbidden)
}

// TestDeleteAPIKey covers the mismatch gate, bad uuid input, and deletion.
func TestDeleteAPIKey(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	h := NewHandler(pool)
	router := newRouter(t, h, &auth.Claims{UserID: org.UserID, Email: org.Email})

	keyID := insertKey(t, org.UserID, "doomed", "raw-doomed")

	// Non-member org in URL/header -> RBAC denial.
	foreignHeaders := http.Header{}
	foreignHeaders.Set("X-Organization-Id", ghostOrgID)
	rec := do(t, router, http.MethodDelete, "/api/v1/organizations/"+ghostOrgID+"/api-keys/"+keyID, "", foreignHeaders)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member delete status = %d, want 403", rec.Code)
	}

	headers := http.Header{}
	headers.Set("X-Organization-Id", org.OrgID)

	// Malformed key id fails the delete statement -> 400.
	rec = do(t, router, http.MethodDelete, "/api/v1/organizations/"+org.OrgID+"/api-keys/not-a-uuid", "", headers)
	assertEnvelopeCode(t, rec, http.StatusBadRequest)

	// Valid delete removes exactly that key.
	rec = do(t, router, http.MethodDelete, "/api/v1/organizations/"+org.OrgID+"/api-keys/"+keyID, "", headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from api_keys where id = $1::uuid`, keyID); n != 0 {
		t.Fatalf("key rows after delete = %d, want 0", n)
	}
}

// TestRegenerateAPIKey covers not-found, success rotation, and hash change.
func TestRegenerateAPIKey(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	h := NewHandler(pool)
	router := newRouter(t, h, &auth.Claims{UserID: org.UserID, Email: org.Email})
	headers := http.Header{}
	headers.Set("X-Organization-Id", org.OrgID)

	// Unknown key -> 404 not_found envelope.
	rec := do(t, router, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/api-keys/00000000-0000-0000-0000-00000000000a/regenerate", "", headers)
	assertEnvelopeCode(t, rec, http.StatusNotFound)

	oldRaw := "regenerate-me-raw"
	keyID := insertKey(t, org.UserID, "rotate-me", oldRaw)
	oldHash, oldUserID, oldName := keyRow(t, keyID)

	rec = do(t, router, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/api-keys/"+keyID+"/regenerate", "", headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	newToken, _ := body["token"].(string)
	if newToken == "" || newToken == oldRaw {
		t.Fatalf("regenerate returned unexpected token: %s", rec.Body.String())
	}

	// The old row was deleted and a replacement exists with a fresh id, same
	// identity, but a different hash.
	if n := testdb.QueryCount(t, `select count(*) from api_keys where token_hash = $1`, oldHash); n != 0 {
		t.Fatalf("old hash still present in %d rows, want 0", n)
	}
	if n := testdb.QueryCount(t, `select count(*) from api_keys where id = $1::uuid`, keyID); n != 0 {
		t.Fatalf("old key id still present in %d rows, want 0", n)
	}
	var newKeyID string
	if err := pool.QueryRow(t.Context(), `
		select id::text from api_keys where token_hash = $1
	`, sha256Hex(newToken)).Scan(&newKeyID); err != nil {
		t.Fatalf("replacement key missing: %v", err)
	}
	if newKeyID == keyID {
		t.Fatal("replacement reused the deleted key id")
	}
	newHash, gotUser, gotName := keyRow(t, newKeyID)
	if newHash != sha256Hex(newToken) || gotUser != oldUserID || gotName != oldName {
		t.Fatalf("replacement row = (%s, %s, %s), want new hash for same user/name", newHash, gotUser, gotName)
	}
}

// TestSha256HexMatchesCommonHelper pins the hashing used for storage checks.
func TestSha256HexMatchesCommonHelper(t *testing.T) {
	if sha256Hex("hive") != common.SHA256Hex("hive") {
		t.Fatal("package-local sha256Hex diverges from common.SHA256Hex")
	}
}

func assertEnvelopeCode(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v (%s)", err, rec.Body.String())
	}
	if body["error"] == "" {
		t.Fatalf("expected error envelope, got %s", rec.Body.String())
	}
}

// raiseTrigger installs a trigger aborting the given action on a table.
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

// failRandomBytes swaps the token entropy source for one that always fails.
func failRandomBytes(t *testing.T) {
	t.Helper()
	prev := randomBytes
	randomBytes = func(b []byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randomBytes = prev })
}

// TestListAndRegenerateRequireClaims covers the RequireOrgAccess early-return
// branches in handlers reached without claims.
func TestListAndRegenerateRequireClaims(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	h := NewHandler(testdb.Get(t))
	router := newRouter(t, h, nil) // no claims middleware

	rec := do(t, router, http.MethodGet, "/api/v1/organizations/"+org.OrgID+"/api-keys", "", org.Headers)
	assertEnvelopeCode(t, rec, http.StatusUnauthorized)

	rec = do(t, router, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/api-keys/00000000-0000-0000-0000-00000000000b/regenerate", "", org.Headers)
	assertEnvelopeCode(t, rec, http.StatusUnauthorized)
}

// TestDeleteAndRegenerateOrgMismatch covers the active-organization mismatch
// gates: header org differs from the org named in the URL.
func TestDeleteAndRegenerateOrgMismatch(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	h := NewHandler(testdb.Get(t))
	router := newRouter(t, h, &auth.Claims{UserID: org.UserID, Email: org.Email})
	headers := http.Header{}
	headers.Set("X-Organization-Id", org.OrgID)

	keyID := insertKey(t, org.UserID, "mismatched", "raw-mismatch")

	rec := do(t, router, http.MethodDelete, "/api/v1/organizations/"+ghostOrgID+"/api-keys/"+keyID, "", headers)
	assertEnvelopeCode(t, rec, http.StatusForbidden)

	rec = do(t, router, http.MethodPost, "/api/v1/organizations/"+ghostOrgID+"/api-keys/"+keyID+"/regenerate", "", headers)
	assertEnvelopeCode(t, rec, http.StatusForbidden)
}

// TestCreateAPIKeyTokenGenerationFailure injects an entropy failure.
func TestCreateAPIKeyTokenGenerationFailure(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	h := NewHandler(testdb.Get(t))
	failRandomBytes(t)
	router := newRouter(t, h, &auth.Claims{UserID: org.UserID, Email: org.Email})

	rec := do(t, router, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/api-keys", `{"name":"ci"}`, org.Headers)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("entropy-failure create status = %d, want 500 (%s)", rec.Code, rec.Body.String())
	}
}

// TestCreateAPIKeyInsertFailure injects an api_keys insert fault.
func TestCreateAPIKeyInsertFailure(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	h := NewHandler(testdb.Get(t))
	raiseTrigger(t, "api_keys", "cov_no_key_insert", "before insert",
		"when (new.name = 'boom')")
	router := newRouter(t, h, &auth.Claims{UserID: org.UserID, Email: org.Email})

	rec := do(t, router, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/api-keys", `{"name":"boom"}`, org.Headers)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("insert-failure create status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}

// TestRegenerateAPIKeyFailures injects faults into each regenerate stage.
func TestRegenerateAPIKeyFailures(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	h := NewHandler(testdb.Get(t))
	router := newRouter(t, h, &auth.Claims{UserID: org.UserID, Email: org.Email})
	headers := http.Header{}
	headers.Set("X-Organization-Id", org.OrgID)

	regenURL := func(keyID string) string {
		return "/api/v1/organizations/" + org.OrgID + "/api-keys/" + keyID + "/regenerate"
	}

	// Delete-old fails.
	deleteFailID := insertKey(t, org.UserID, "boomdel", "raw-boomdel")
	raiseTrigger(t, "api_keys", "cov_no_key_delete", "before delete",
		"when (old.name = 'boomdel')")
	rec := do(t, router, http.MethodPost, regenURL(deleteFailID), "", headers)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "failed to delete old api key") {
		t.Fatalf("delete-failure regenerate = %d %s, want 400 failed to delete old api key", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from api_keys where id = $1::uuid`, deleteFailID); n != 1 {
		t.Fatalf("old key deleted despite fault: rows = %d, want 1", n)
	}

	// Insert-new fails after successful delete.
	insertFailID := insertKey(t, org.UserID, "boom", "raw-boom")
	raiseTrigger(t, "api_keys", "cov_no_key_reinsert", "before insert",
		"when (new.name = 'boom')")
	rec = do(t, router, http.MethodPost, regenURL(insertFailID), "", headers)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "failed to create new api key") {
		t.Fatalf("insert-failure regenerate = %d %s, want 400 failed to create new api key", rec.Code, rec.Body.String())
	}

	// Token generation fails.
	okID := insertKey(t, org.UserID, "entropy", "raw-entropy")
	failRandomBytes(t)
	rec = do(t, router, http.MethodPost, regenURL(okID), "", headers)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "failed to generate token") {
		t.Fatalf("entropy-failure regenerate = %d %s, want 500 failed to generate token", rec.Code, rec.Body.String())
	}
}
