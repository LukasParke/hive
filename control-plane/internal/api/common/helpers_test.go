package common

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// TestWriteJSONSetsContentTypeAndBody checks the JSON envelope basics.
func TestWriteJSONSetsContentTypeAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusTeapot, map[string]string{"ok": "yes"})

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v (%s)", err, rec.Body.String())
	}
	if body["ok"] != "yes" {
		t.Fatalf("body = %v, want ok=yes", body)
	}
}

// TestWriteErrorEnvelope verifies the error envelope shape.
func TestWriteErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusForbidden, "forbidden", "nope")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if body["error"] != "forbidden" || body["message"] != "nope" {
		t.Fatalf("envelope = %v, want error=forbidden message=nope", body)
	}
}

// TestRandomToken checks length, hex encoding, and uniqueness.
func TestRandomToken(t *testing.T) {
	tok, err := RandomToken(16)
	if err != nil {
		t.Fatalf("RandomToken: %v", err)
	}
	if len(tok) != 32 {
		t.Fatalf("token length = %d, want 32", len(tok))
	}
	if strings.ToLower(tok) != tok || strings.TrimSpace(tok) == "" {
		t.Fatalf("token %q is not lowercase hex", tok)
	}
	other, err := RandomToken(16)
	if err != nil {
		t.Fatalf("RandomToken second: %v", err)
	}
	if tok == other {
		t.Fatal("two random tokens are identical")
	}
}

// TestSHA256Hex uses the well-known empty-string and "hello" digests.
func TestSHA256Hex(t *testing.T) {
	const hello = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got := SHA256Hex("hello"); got != hello {
		t.Fatalf("SHA256Hex(hello) = %s, want %s", got, hello)
	}
	const empty = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := SHA256Hex(""); got != empty {
		t.Fatalf("SHA256Hex(\"\") = %s, want %s", got, empty)
	}
}

// TestToUUIDValidAndInvalid covers both parse outcomes.
func TestToUUIDValidAndInvalid(t *testing.T) {
	u, err := ToUUID("00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("ToUUID valid: %v", err)
	}
	if u.Bytes[15] != 0x01 {
		t.Fatalf("uuid bytes mismatch: %v", u.Bytes)
	}
	if _, err := ToUUID("not-a-uuid"); err == nil {
		t.Fatal("ToUUID(garbage) err = nil, want error")
	}
}

// TestResolveOrgIDHeaderWins asserts the header overrides any memberships.
func TestResolveOrgIDHeaderWins(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Organization-Id", "11111111-1111-1111-1111-111111111111")
	rec := httptest.NewRecorder()

	got, ok := ResolveOrgID(rec, r, pool, org.UserID)
	if !ok {
		t.Fatal("ResolveOrgID = false, want true")
	}
	if got != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("orgID = %q, want header value", got)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("response body written: %s", rec.Body.String())
	}
}

// TestResolveOrgIDSoleMembership falls back to the only membership.
func TestResolveOrgIDSoleMembership(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	got, ok := ResolveOrgID(rec, r, pool, org.UserID)
	if !ok || got != org.OrgID {
		t.Fatalf("ResolveOrgID = (%q, %v), want (%q, true)", got, ok, org.OrgID)
	}
}

// TestResolveOrgIDNoOrganizations yields a bad_request envelope.
func TestResolveOrgIDNoOrganizations(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	userID, err := testdb.Auth(t).Register(context.Background(), "lonely@test.local", "sup3rsecret!", "Lonely")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	if _, ok := ResolveOrgID(rec, r, pool, userID); ok {
		t.Fatal("ResolveOrgID = true, want false for org-less user")
	}
	assertErrorEnvelope(t, rec, http.StatusBadRequest, "bad_request")
}

// TestResolveOrgIDAmbiguousMembership demands the header when the user
// belongs to more than one organization.
func TestResolveOrgIDAmbiguousMembership(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	suffix := strings.ReplaceAll(org.OrgID, "-", "")[:8]
	if _, err := pool.Exec(context.Background(), `
		insert into organizations(name, slug) values ($1, $2)
	`, "second-"+suffix, "second-"+suffix); err != nil {
		t.Fatalf("insert second org: %v", err)
	}
	var secondID string
	if err := pool.QueryRow(context.Background(), `select id::text from organizations where slug = $1`, "second-"+suffix).Scan(&secondID); err != nil {
		t.Fatalf("load second org: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into organization_members(organization_id, user_id, role) values ($1::uuid, $2::uuid, 'owner')
	`, secondID, org.UserID); err != nil {
		t.Fatalf("insert second membership: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	if _, ok := ResolveOrgID(rec, r, pool, org.UserID); ok {
		t.Fatal("ResolveOrgID = true, want false for ambiguous membership")
	}
	assertErrorEnvelope(t, rec, http.StatusBadRequest, "bad_request")
	if !strings.Contains(rec.Body.String(), "missing X-Organization-Id") {
		t.Fatalf("body = %s, want missing-header hint", rec.Body.String())
	}
}

// TestResolveOrgIDPoolFailure maps a broken pool onto the internal_error
// envelope.
func TestResolveOrgIDPoolFailure(t *testing.T) {
	dead, err := pgxpool.New(context.Background(), "postgres://hive:hive@127.0.0.1:1/hive")
	if err != nil {
		t.Fatalf("build dead pool: %v", err)
	}
	defer dead.Close()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	if _, ok := ResolveOrgID(rec, r, dead, "00000000-0000-0000-0000-000000000001"); ok {
		t.Fatal("ResolveOrgID = true, want false on dead pool")
	}
	assertErrorEnvelope(t, rec, http.StatusInternalServerError, "internal_error")
}

// TestRequireOrgAccess covers the unauthorized, forbidden, and happy paths.
func TestRequireOrgAccess(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	member := org.AddMember(t, rbac.RoleMember)

	withClaims := func(tok *testdb.OrgFixture) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		for k, vs := range tok.Headers {
			for _, v := range vs {
				r.Header.Add(k, v)
			}
		}
		claims := &auth.Claims{UserID: tok.UserID, Email: tok.Email}
		return r.WithContext(apicxt.WithClaims(r.Context(), claims))
	}

	// No claims at all -> 401 envelope.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	if _, ok := RequireOrgAccess(rec, r, pool, rbac.RoleOwner); ok {
		t.Fatal("RequireOrgAccess without claims = true, want false")
	}
	assertErrorEnvelope(t, rec, http.StatusUnauthorized, "unauthorized")

	// Owner passes an owner-only gate.
	rec = httptest.NewRecorder()
	got, ok := RequireOrgAccess(rec, withClaims(org), pool, rbac.RoleOwner)
	if !ok || got != org.OrgID {
		t.Fatalf("owner RequireOrgAccess = (%q, %v), want (%q, true)", got, ok, org.OrgID)
	}

	// Plain member fails the owner-only gate with 403.
	rec = httptest.NewRecorder()
	if _, ok := RequireOrgAccess(rec, withClaims(member), pool, rbac.RoleOwner); ok {
		t.Fatal("member passed owner-only gate")
	}
	assertErrorEnvelope(t, rec, http.StatusForbidden, "forbidden")

	// Member passes when membership alone suffices (empty role list).
	rec = httptest.NewRecorder()
	if _, ok := RequireOrgAccess(rec, withClaims(member), pool); !ok {
		t.Fatalf("member failed membership-only gate: %s", rec.Body.String())
	}
}

// assertErrorEnvelope decodes rec and asserts status plus error code.
func assertErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
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

// failingRows is a pgx.Rows stub whose Scan always fails, used to exercise
// the row-scanning error branch in ResolveOrgID.
type failingRows struct{ pgx.Rows }

func (failingRows) Next() bool            { return true }
func (failingRows) Scan(dst ...any) error { return errors.New("injected scan failure") }
func (failingRows) Close()                {}
func (failingRows) Err() error            { return nil }

// TestResolveOrgIDScanFailure injects a scanning failure and expects the
// internal_error envelope.
func TestResolveOrgIDScanFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)

	prev := queryOrgIDs
	queryOrgIDs = func(ctx context.Context, p *pgxpool.Pool, userID string) (pgx.Rows, error) {
		return failingRows{}, nil
	}
	t.Cleanup(func() { queryOrgIDs = prev })

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	if _, ok := ResolveOrgID(rec, r, pool, org.UserID); ok {
		t.Fatal("ResolveOrgID = true, want false on scan failure")
	}
	assertErrorEnvelope(t, rec, http.StatusInternalServerError, "internal_error")
}

// TestRequireOrgAccessResolutionFailure covers the early return when
// ResolveOrgID fails behind valid claims.
func TestRequireOrgAccessResolutionFailure(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	userID, err := testdb.Auth(t).Register(context.Background(), "noorg@test.local", "sup3rsecret!", "No Org")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(apicxt.WithClaims(r.Context(), &auth.Claims{UserID: userID, Email: "noorg@test.local"}))
	rec := httptest.NewRecorder()

	if _, ok := RequireOrgAccess(rec, r, pool, rbac.RoleOwner); ok {
		t.Fatal("RequireOrgAccess = true, want false when resolution fails")
	}
	assertErrorEnvelope(t, rec, http.StatusBadRequest, "bad_request")
}
