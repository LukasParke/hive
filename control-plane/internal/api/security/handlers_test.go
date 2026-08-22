package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// appliedApps records every application id handed to the apply seam.
var appliedMu sync.Mutex
var appliedApps []string

func resetApplied(t *testing.T) {
	t.Helper()
	appliedMu.Lock()
	appliedApps = nil
	appliedMu.Unlock()
	orig := applySecurityRules
	applySecurityRules = func(_ context.Context, _ *pgxpool.Pool, _ *swarmclient.Client, appID string) error {
		appliedMu.Lock()
		appliedApps = append(appliedApps, appID)
		appliedMu.Unlock()
		return nil
	}
	t.Cleanup(func() { applySecurityRules = orig })
}

func appliedCount() int {
	appliedMu.Lock()
	defer appliedMu.Unlock()
	return len(appliedApps)
}

// newSecurityRouter wires a real chi router with auth middleware and a live
// (unreachable) swarm client so the Swarm != nil branches execute.
func newSecurityRouter(t *testing.T) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	resetApplied(t)
	cli, err := swarmclient.New("tcp://127.0.0.1:1")
	if err != nil {
		t.Fatalf("construct swarm client: %v", err)
	}
	h := NewHandler(pool, cli)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/security-rules", h.ListSecurityRules)
		gr.Post("/api/v1/security-rules", h.CreateSecurityRule)
		gr.Get("/api/v1/security-rules/{id}", h.GetSecurityRule)
		gr.Put("/api/v1/security-rules/{id}", h.UpdateSecurityRule)
		gr.Delete("/api/v1/security-rules/{id}", h.DeleteSecurityRule)
	})
	return r
}

func doJSON(t *testing.T, router http.Handler, method, path string, headers http.Header, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func seedRule(t *testing.T, orgID, projectID, name, ruleType string) string {
	t.Helper()
	appID := testdb.SeedApplication(t, projectID, "app-"+name, "", nil)
	var id string
	if err := testdb.Get(t).QueryRow(context.Background(), `
		insert into security_rules(organization_id, application_id, name, type, config, priority, enabled)
		values ($1::uuid, $2::uuid, $3, $4, '{"header":"X-Frame-Options","value":"DENY"}'::jsonb, 10, true)
		returning id::text
	`, orgID, appID, name, ruleType).Scan(&id); err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	return id
}

func TestListSecurityRulesEmptyAndPopulated(t *testing.T) {
	router := newSecurityRouter(t)
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/security-rules", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":null`) {
		t.Fatalf("empty list status = %d body=%s", rec.Code, rec.Body.String())
	}

	id := seedRule(t, org.OrgID, org.ProjectID, "frame-guard", "header_security")

	rec = doJSON(t, router, http.MethodGet, "/api/v1/security-rules", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].ID != id || resp.Items[0].Name != "frame-guard" {
		t.Fatalf("items = %s", rec.Body.String())
	}
}

func TestListSecurityRulesIsolatesOrganizations(t *testing.T) {
	router := newSecurityRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)

	seedRule(t, orgA.OrgID, orgA.ProjectID, "a-only", "rate_limit")

	otherRec := doJSON(t, router, http.MethodGet, "/api/v1/security-rules", orgB.Headers, "")
	if otherRec.Code != http.StatusOK || !strings.Contains(otherRec.Body.String(), `"items":null`) {
		t.Fatalf("foreign org sees rows: %d %s", otherRec.Code, otherRec.Body.String())
	}
}

func TestGetSecurityRuleFoundAndMissing(t *testing.T) {
	router := newSecurityRouter(t)
	org := testdb.SeedOrg(t)
	id := seedRule(t, org.OrgID, org.ProjectID, "guard", "header_security")

	rec := doJSON(t, router, http.MethodGet, "/api/v1/security-rules/"+id, org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"guard"`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		name   string
		path   string
		status int
	}{
		{"invalid id", "junk-id", http.StatusBadRequest},
		{"unknown id", "00000000-0000-0000-0000-000000000000", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodGet, "/api/v1/security-rules/"+tc.path, org.Headers, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}

	other := testdb.SeedOrg(t)
	rec = doJSON(t, router, http.MethodGet, "/api/v1/security-rules/"+id, other.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org status = %d, want 404", rec.Code)
	}
}

func TestCreateSecurityRuleValidationPerType(t *testing.T) {
	router := newSecurityRouter(t)
	org := testdb.SeedOrg(t)

	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"malformed json", `{broken`, http.StatusBadRequest, "invalid payload"},
		{"country_block rejected", `{"type":"country_block","name":"geo"}`, http.StatusBadRequest, "not supported"},
		{"ip_blocklist rejected", `{"type":"ip_blocklist","name":"bl"}`, http.StatusBadRequest, "not supported"},
		{"unknown type rejected by db check", `{"type":"magic_filter","name":"m"}`, http.StatusBadRequest, "failed to create security rule"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPost, "/api/v1/security-rules", org.Headers, tc.body)
			if rec.Code != tc.wantStatus || !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	if n := testdb.QueryCount(t, `select count(*) from security_rules`); n != 0 {
		t.Fatalf("rejected rules persisted: %d rows", n)
	}
}

func TestCreateSecurityRuleHappyPathWithoutApplication(t *testing.T) {
	router := newSecurityRouter(t)
	org := testdb.SeedOrg(t)

	body := `{"type":"rate_limit","name":"throttle","config":{"average":100,"burst":200},"priority":5,"enabled":true}`
	rec := doJSON(t, router, http.MethodPost, "/api/v1/security-rules", org.Headers, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("decode id: %v %s", err, rec.Body.String())
	}
	var typ, config string
	var priority int32
	if err := testdb.Get(t).QueryRow(context.Background(), `
		select type, config::text, priority from security_rules where id=$1::uuid
	`, resp.ID).Scan(&typ, &config, &priority); err != nil {
		t.Fatalf("rule row missing: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if typ != "rate_limit" || priority != 5 || cfg["average"] != float64(100) || cfg["burst"] != float64(200) {
		t.Fatalf("row = (%s %d %s)", typ, priority, config)
	}
	if appliedCount() != 0 {
		t.Fatalf("apply calls = %d, want 0 without applicationId", appliedCount())
	}
}

func TestCreateSecurityRuleWithApplicationAppliesRules(t *testing.T) {
	router := newSecurityRouter(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "web", "", nil)

	body := `{"applicationId":"` + appID + `","type":"header_security","name":"hsts"}`
	rec := doJSON(t, router, http.MethodPost, "/api/v1/security-rules", org.Headers, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if appliedCount() != 1 || appliedApps[0] != appID {
		t.Fatalf("applied apps = %v, want [%s]", appliedApps, appID)
	}
}

func TestCreateSecurityRuleInvalidApplicationID(t *testing.T) {
	router := newSecurityRouter(t)
	org := testdb.SeedOrg(t)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/security-rules", org.Headers,
		`{"applicationId":"not-a-uuid","type":"rate_limit","name":"x"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid application id") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	// A syntactically valid but unknown application trips the FK constraint.
	rec = doJSON(t, router, http.MethodPost, "/api/v1/security-rules", org.Headers,
		`{"applicationId":"00000000-0000-0000-0000-000000000000","type":"rate_limit","name":"x"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "failed to create security rule") {
		t.Fatalf("fk violation status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateSecurityRuleHappyPathAndValidation(t *testing.T) {
	router := newSecurityRouter(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "web", "", nil)
	_ = appID
	id := seedRule(t, org.OrgID, org.ProjectID, "old-name", "header_security")

	body := `{"applicationId":"` + appID + `","type":"rate_limit","name":"new-name","priority":7,"enabled":false}`
	rec := doJSON(t, router, http.MethodPut, "/api/v1/security-rules/"+id, org.Headers, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var name, typ string
	var enabled bool
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select name, type, enabled from security_rules where id=$1::uuid`, id).Scan(&name, &typ, &enabled); err != nil {
		t.Fatalf("row missing: %v", err)
	}
	if name != "new-name" || typ != "rate_limit" || enabled {
		t.Fatalf("row = (%s %s %v)", name, typ, enabled)
	}
	if appliedCount() != 1 || appliedApps[0] != appID {
		t.Fatalf("applied apps = %v, want [%s]", appliedApps, appID)
	}

	// Unsupported type is rejected before touching the row.
	rec = doJSON(t, router, http.MethodPut, "/api/v1/security-rules/"+id, org.Headers,
		`{"type":"country_block","name":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad type status = %d, want 400", rec.Code)
	}
	// Malformed payload.
	rec = doJSON(t, router, http.MethodPut, "/api/v1/security-rules/"+id, org.Headers, `{broken`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d, want 400", rec.Code)
	}
	// Invalid application id.
	rec = doJSON(t, router, http.MethodPut, "/api/v1/security-rules/"+id, org.Headers,
		`{"applicationId":"nope","type":"rate_limit","name":"x"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid application id") {
		t.Fatalf("bad app id status = %d body=%s", rec.Code, rec.Body.String())
	}
	// Invalid rule id in URL.
	rec = doJSON(t, router, http.MethodPut, "/api/v1/security-rules/junk", org.Headers,
		`{"type":"rate_limit","name":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad url id status = %d, want 400", rec.Code)
	}
}

func TestDeleteSecurityRuleRemovesAndReapplies(t *testing.T) {
	router := newSecurityRouter(t)
	org := testdb.SeedOrg(t)
	id := seedRule(t, org.OrgID, org.ProjectID, "todelete", "rate_limit")
	var appID string
	if err := testdb.Get(t).QueryRow(context.Background(),
		`select application_id::text from security_rules where id=$1::uuid`, id).Scan(&appID); err != nil {
		t.Fatalf("rule app missing: %v", err)
	}

	rec := doJSON(t, router, http.MethodDelete, "/api/v1/security-rules/"+id, org.Headers, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s, want 204", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from security_rules where id=$1::uuid`, id); n != 0 {
		t.Fatal("rule row not deleted")
	}
	if appliedCount() != 1 || appliedApps[0] != appID {
		t.Fatalf("applied apps = %v, want re-apply for [%s]", appliedApps, appID)
	}

	// Deleting again still returns 204 (exec is idempotent).
	rec = doJSON(t, router, http.MethodDelete, "/api/v1/security-rules/"+id, org.Headers, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("repeat delete status = %d, want 204", rec.Code)
	}

	// Invalid rule id.
	rec = doJSON(t, router, http.MethodDelete, "/api/v1/security-rules/bad", org.Headers, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id status = %d, want 400", rec.Code)
	}
}

func TestSecurityEndpointsRequireAuthentication(t *testing.T) {
	router := newSecurityRouter(t)
	org := testdb.SeedOrg(t)
	id := seedRule(t, org.OrgID, org.ProjectID, "auth", "rate_limit")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/security-rules"},
		{http.MethodPost, "/api/v1/security-rules"},
		{http.MethodGet, "/api/v1/security-rules/" + id},
		{http.MethodPut, "/api/v1/security-rules/" + id},
		{http.MethodDelete, "/api/v1/security-rules/" + id},
	} {
		rec := doJSON(t, router, tc.method, tc.path, http.Header{}, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s unauthenticated status = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

// outsiderHeaders registers a user with no membership in orgID and returns
// authenticated headers, so handlers exercise their own RBAC rejection.
func outsiderHeaders(t *testing.T, orgID string) http.Header {
	t.Helper()
	svc := testdb.Auth(t)
	email := fmt.Sprintf("outsider-%s@test.local", strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
	if _, err := svc.Register(context.Background(), email, "sup3rsecret!", "Outsider"); err != nil {
		t.Fatalf("register outsider: %v", err)
	}
	token, _, err := svc.Login(context.Background(), email, "sup3rsecret!")
	if err != nil {
		t.Fatalf("login outsider: %v", err)
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set("X-Organization-Id", orgID)
	return h
}

// newSecurityRouterOnPool wires the routes against an arbitrary pool without
// resetting tables, so DDL-based failure injection keeps its seeded rows.
func newSecurityRouterOnPool(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()
	resetApplied(t)
	cli, err := swarmclient.New("tcp://127.0.0.1:1")
	if err != nil {
		t.Fatalf("construct swarm client: %v", err)
	}
	h := NewHandler(pool, cli)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/security-rules", h.ListSecurityRules)
		gr.Post("/api/v1/security-rules", h.CreateSecurityRule)
		gr.Get("/api/v1/security-rules/{id}", h.GetSecurityRule)
		gr.Put("/api/v1/security-rules/{id}", h.UpdateSecurityRule)
		gr.Delete("/api/v1/security-rules/{id}", h.DeleteSecurityRule)
	})
	return r
}

func TestSecurityEndpointsDenyNonMembers(t *testing.T) {
	router := newSecurityRouter(t)
	org := testdb.SeedOrg(t)
	id := seedRule(t, org.OrgID, org.ProjectID, "deny", "rate_limit")
	headers := outsiderHeaders(t, org.OrgID)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/security-rules"},
		{http.MethodPost, "/api/v1/security-rules"},
		{http.MethodGet, "/api/v1/security-rules/" + id},
		{http.MethodPut, "/api/v1/security-rules/" + id},
		{http.MethodDelete, "/api/v1/security-rules/" + id},
	} {
		rec := doJSON(t, router, tc.method, tc.path, headers, `{"type":"rate_limit","name":"x"}`)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "forbidden") {
			t.Fatalf("%s %s outsider status = %d body=%s, want 403", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// TestSecurityRulesDBFailures renames the backing table so every statement
// touching it fails at prepare time on a fresh (uncached) pool.
func TestSecurityRulesDBFailures(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	id := seedRule(t, org.OrgID, org.ProjectID, "broken", "rate_limit")
	body := `{"type":"rate_limit","name":"x"}`

	if _, err := pool.Exec(context.Background(), `alter table security_rules rename to security_rules_gone`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatalf("parse conn string: %v", err)
	}
	fresh, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer fresh.Close()
	router := newSecurityRouterOnPool(t, fresh)

	listRec := doJSON(t, router, http.MethodGet, "/api/v1/security-rules", org.Headers, "")
	createRec := doJSON(t, router, http.MethodPost, "/api/v1/security-rules", org.Headers, body)
	getRec := doJSON(t, router, http.MethodGet, "/api/v1/security-rules/"+id, org.Headers, "")
	updateRec := doJSON(t, router, http.MethodPut, "/api/v1/security-rules/"+id, org.Headers, body)
	deleteRec := doJSON(t, router, http.MethodDelete, "/api/v1/security-rules/"+id, org.Headers, "")

	if _, err := pool.Exec(context.Background(), `alter table security_rules_gone rename to security_rules`); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if listRec.Code != http.StatusInternalServerError || !strings.Contains(listRec.Body.String(), "failed to list security rules") {
		t.Fatalf("list status = %d body=%s, want 500", listRec.Code, listRec.Body.String())
	}
	if createRec.Code != http.StatusBadRequest || !strings.Contains(createRec.Body.String(), "failed to create security rule") {
		t.Fatalf("create status = %d body=%s, want 400", createRec.Code, createRec.Body.String())
	}
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("get status = %d, want 404", getRec.Code)
	}
	if updateRec.Code != http.StatusBadRequest || !strings.Contains(updateRec.Body.String(), "failed to update security rule") {
		t.Fatalf("update status = %d body=%s, want 400", updateRec.Code, updateRec.Body.String())
	}
	if deleteRec.Code != http.StatusBadRequest || !strings.Contains(deleteRec.Body.String(), "failed to delete security rule") {
		t.Fatalf("delete status = %d body=%s, want 400", deleteRec.Code, deleteRec.Body.String())
	}
}
