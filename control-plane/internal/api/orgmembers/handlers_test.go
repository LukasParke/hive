package orgmembers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	pgx "github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/auth"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newOrgMembersRouter wires a real chi router with the same auth middleware
// used in production so JWTs and org headers are exercised end-to-end.
func newOrgMembersRouter(t *testing.T) (http.Handler, *Handler) {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool)
	r := chi.NewRouter()
	r.Get("/api/v1/invitations/{token}", h.GetInvitationByToken) // public route
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Post("/api/v1/invitations/{token}/accept", h.AcceptInvitationByToken)
		gr.Get("/api/v1/organizations/{id}/members", h.ListOrganizationMembers)
		gr.Put("/api/v1/organizations/{id}/members/{userId}", h.UpdateOrganizationMemberRole)
		gr.Get("/api/v1/organizations/{id}/invitations", h.ListOrganizationInvitations)
		gr.Post("/api/v1/organizations/{id}/invitations", h.CreateOrganizationInvitation)
		gr.Delete("/api/v1/organizations/{id}/invitations/{inviteId}", h.DeleteOrganizationInvitation)
		gr.Post("/api/v1/organizations/{id}/invitations/{inviteId}/resend", h.ResendOrganizationInvitation)
		gr.Post("/api/v1/organizations/{id}/invitations/{inviteId}/revoke", h.RevokeOrganizationInvitation)
	})
	return r, h
}

func doJSON(router http.Handler, method, path string, headers http.Header, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// simpleProtocolPool returns a pool whose statements fail at parse time when
// columns are renamed, letting us exercise failure branches deterministically.
func simpleProtocolPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testdb.Get(t)
	cfg, err := pgxpool.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	handlerPool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open simple protocol pool: %v", err)
	}
	t.Cleanup(handlerPool.Close)
	return handlerPool
}

func simpleProtocolRouter(t *testing.T) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(simpleProtocolPool(t))
	r := chi.NewRouter()
	r.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
	r.Get("/api/v1/organizations/{id}/members", h.ListOrganizationMembers)
	r.Put("/api/v1/organizations/{id}/members/{userId}", h.UpdateOrganizationMemberRole)
	r.Get("/api/v1/organizations/{id}/invitations", h.ListOrganizationInvitations)
	r.Post("/api/v1/organizations/{id}/invitations", h.CreateOrganizationInvitation)
	r.Delete("/api/v1/organizations/{id}/invitations/{inviteId}", h.DeleteOrganizationInvitation)
	r.Post("/api/v1/organizations/{id}/invitations/{inviteId}/resend", h.ResendOrganizationInvitation)
	r.Post("/api/v1/organizations/{id}/invitations/{inviteId}/revoke", h.RevokeOrganizationInvitation)
	return r
}

// renameColumn renames a column so matching statements fail at parse time; it
// restores itself via t.Cleanup.
func renameColumn(t *testing.T, table, from, to string) {
	t.Helper()
	ctx := context.Background()
	p := testdb.Get(t)
	if _, err := p.Exec(ctx, fmt.Sprintf("alter table %s rename column %s to %s", table, from, to)); err != nil {
		t.Fatalf("rename %s.%s: %v", table, from, err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, fmt.Sprintf("alter table %s rename column %s to %s", table, to, from)); err != nil {
			t.Fatalf("restore %s.%s: %v", table, to, err)
		}
	})
}

// seedInvitation inserts an invitation with a known raw token and returns its id.
func seedInvitation(t *testing.T, orgID, email, role, status string, expiresIn time.Duration) (string, string) {
	t.Helper()
	p := testdb.Get(t)
	rawToken := "raw-" + email + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	var id string
	err := p.QueryRow(context.Background(), `
		insert into organization_invitations(organization_id, email, role, token_hash, status, expires_at)
		values ($1::uuid, $2, $3::member_role, $4, $5, now() + $6::interval)
		returning id::text
	`, orgID, email, role, sha256Hex(rawToken), status, fmt.Sprintf("%.0f seconds", expiresIn.Seconds())).Scan(&id)
	if err != nil {
		t.Fatalf("seed invitation: %v", err)
	}
	return id, rawToken
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return resp
}

func TestListOrganizationMembers(t *testing.T) {
	router, _ := newOrgMembersRouter(t)
	org := testdb.SeedOrg(t)

	t.Run("owner lists members", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/organizations/"+org.OrgID+"/members", org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		items, ok := resp["items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("items = %v, want exactly the owner", resp["items"])
		}
		item := items[0].(map[string]any)
		if item["userId"] != org.UserID || item["role"] != "owner" || item["email"] != org.Email {
			t.Fatalf("item = %v, want the owner fixture", item)
		}
	})

	t.Run("admin can list", func(t *testing.T) {
		admin := org.AddMember(t, rbac.RoleAdmin)
		rec := doJSON(router, http.MethodGet, "/api/v1/organizations/"+org.OrgID+"/members", admin.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("member forbidden", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		rec := doJSON(router, http.MethodGet, "/api/v1/organizations/"+org.OrgID+"/members", member.Headers, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("outsider forbidden", func(t *testing.T) {
		orgB := testdb.SeedOrg(t)
		intruder := http.Header{}
		intruder.Set("Authorization", "Bearer "+orgB.Token)
		intruder.Set("X-Organization-Id", org.OrgID)
		rec := doJSON(router, http.MethodGet, "/api/v1/organizations/"+org.OrgID+"/members", intruder, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
		}
	})

	t.Run("url org mismatch forbidden", func(t *testing.T) {
		orgB := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodGet, "/api/v1/organizations/"+orgB.OrgID+"/members", org.Headers, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403 mismatch", rec.Code, rec.Body.String())
		}
	})
}

func TestUpdateOrganizationMemberRole(t *testing.T) {
	router, _ := newOrgMembersRouter(t)
	org := testdb.SeedOrg(t)
	member := org.AddMember(t, rbac.RoleMember)
	path := "/api/v1/organizations/" + org.OrgID + "/members/" + member.UserID

	t.Run("owner changes role", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, path, org.Headers, `{"role":"admin"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		p := testdb.Get(t)
		var role string
		if err := p.QueryRow(context.Background(), `
			select role::text from organization_members where organization_id=$1::uuid and user_id=$2::uuid
		`, org.OrgID, member.UserID).Scan(&role); err != nil || role != "admin" {
			t.Fatalf("role = %q err=%v, want admin", role, err)
		}
	})

	t.Run("invalid role rejected", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, path, org.Headers, `{"role":"superboss"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("malformed json rejected", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut, path, org.Headers, "{not json")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("admin cannot change roles", func(t *testing.T) {
		admin := org.AddMember(t, rbac.RoleAdmin)
		rec := doJSON(router, http.MethodPut, path, admin.Headers, `{"role":"member"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("unknown user still answers ok", func(t *testing.T) {
		rec := doJSON(router, http.MethodPut,
			"/api/v1/organizations/"+org.OrgID+"/members/00000000-0000-0000-0000-000000000000",
			org.Headers, `{"role":"member"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
	})

	t.Run("url org mismatch forbidden", func(t *testing.T) {
		orgB := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodPut, "/api/v1/organizations/"+orgB.OrgID+"/members/"+member.UserID, org.Headers, `{"role":"member"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestCreateOrganizationInvitation(t *testing.T) {
	router, _ := newOrgMembersRouter(t)
	org := testdb.SeedOrg(t)
	path := "/api/v1/organizations/" + org.OrgID + "/invitations"

	t.Run("owner invites with default member role", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, path, org.Headers, `{"email":"New.Person@Example.com"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s, want 201", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		token, ok := resp["token"].(string)
		if !ok || token == "" || resp["status"] != "pending" {
			t.Fatalf("response = %v, want token+pending", resp)
		}
		p := testdb.Get(t)
		var email, role string
		if err := p.QueryRow(context.Background(), `
			select lower(email), role::text from organization_invitations where id::text = $1
		`, resp["id"]).Scan(&email, &role); err != nil {
			t.Fatalf("invitation row missing: %v", err)
		}
		if email != "new.person@example.com" || role != "member" {
			t.Fatalf("email=%q role=%q, want normalized email / member", email, role)
		}
		// The stored hash must match the returned raw token.
		n := testdb.QueryCount(t, `select count(*) from organization_invitations where id::text=$1 and token_hash=$2`, resp["id"], sha256Hex(token))
		if n != 1 {
			t.Fatalf("token_hash does not match returned token")
		}
	})

	t.Run("explicit admin role honored", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, path, org.Headers, `{"email":"adm@example.com","role":"ADMIN "}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s, want 201", rec.Code, rec.Body.String())
		}
		n := testdb.QueryCount(t, `select count(*) from organization_invitations where organization_id=$1::uuid and role='admin'`, org.OrgID)
		if n != 1 {
			t.Fatalf("admin invitations = %d, want 1", n)
		}
	})

	t.Run("missing email rejected", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, path, org.Headers, `{"email":"   "}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("malformed json rejected", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, path, org.Headers, "{not json")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("re-invite replaces pending invitation", func(t *testing.T) {
		first := doJSON(router, http.MethodPost, path, org.Headers, `{"email":"again@example.com"}`)
		if first.Code != http.StatusCreated {
			t.Fatalf("first status = %d body=%s, want 201", first.Code, first.Body.String())
		}
		firstResp := decodeBody(t, first)
		oldToken := firstResp["token"].(string)

		second := doJSON(router, http.MethodPost, path, org.Headers, `{"email":"again@example.com","role":"admin"}`)
		if second.Code != http.StatusCreated {
			t.Fatalf("second status = %d body=%s, want 201", second.Code, second.Body.String())
		}
		secondResp := decodeBody(t, second)
		newToken := secondResp["token"].(string)
		if oldToken == newToken {
			t.Fatal("re-invite must issue a fresh token")
		}
		if n := testdb.QueryCount(t, `
			select count(*) from organization_invitations
			where organization_id=$1::uuid and lower(email)='again@example.com' and status='pending'
		`, org.OrgID); n != 1 {
			t.Fatalf("pending invitations = %d, want exactly 1", n)
		}
		if got := doJSON(router, http.MethodGet, "/api/v1/invitations/"+oldToken, http.Header{}, ""); got.Code != http.StatusNotFound {
			t.Fatalf("old token status = %d, want 404", got.Code)
		}
		if got := doJSON(router, http.MethodGet, "/api/v1/invitations/"+newToken, http.Header{}, ""); got.Code != http.StatusOK {
			t.Fatalf("new token status = %d, want 200", got.Code)
		}
	})

	t.Run("admin can invite", func(t *testing.T) {
		admin := org.AddMember(t, rbac.RoleAdmin)
		rec := doJSON(router, http.MethodPost, path, admin.Headers, `{"email":"byadmin@example.com"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s, want 201", rec.Code, rec.Body.String())
		}
	})

	t.Run("member forbidden", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		rec := doJSON(router, http.MethodPost, path, member.Headers, `{"email":"x@example.com"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("url org mismatch forbidden", func(t *testing.T) {
		orgB := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations/"+orgB.OrgID+"/invitations", org.Headers, `{"email":"y@example.com"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestListOrganizationInvitations(t *testing.T) {
	router, _ := newOrgMembersRouter(t)
	org := testdb.SeedOrg(t)
	path := "/api/v1/organizations/" + org.OrgID + "/invitations"

	id, _ := seedInvitation(t, org.OrgID, "pending@example.com", "member", "pending", 7*24*time.Hour)
	seedInvitation(t, org.OrgID, "revoked@example.com", "member", "revoked", 7*24*time.Hour)

	t.Run("lists all statuses", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, path, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		items, ok := resp["items"].([]any)
		if !ok || len(items) != 2 {
			t.Fatalf("items = %v, want 2 invitations", resp["items"])
		}
		found := false
		for _, raw := range items {
			item := raw.(map[string]any)
			if item["id"] == id {
				found = true
				if item["email"] != "pending@example.com" || item["status"] != "pending" {
					t.Fatalf("item = %v, want pending invitation", item)
				}
			}
		}
		if !found {
			t.Fatalf("seeded invitation %s missing: %v", id, items)
		}
	})

	t.Run("member forbidden", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		if rec := doJSON(router, http.MethodGet, path, member.Headers, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("url org mismatch forbidden", func(t *testing.T) {
		orgB := testdb.SeedOrg(t)
		if rec := doJSON(router, http.MethodGet, "/api/v1/organizations/"+orgB.OrgID+"/invitations", org.Headers, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestResendOrganizationInvitation(t *testing.T) {
	router, _ := newOrgMembersRouter(t)
	org := testdb.SeedOrg(t)

	id, oldToken := seedInvitation(t, org.OrgID, "resend@example.com", "member", "pending", 7*24*time.Hour)
	path := "/api/v1/organizations/" + org.OrgID + "/invitations/" + id + "/resend"

	t.Run("resend issues fresh token", func(t *testing.T) {
		before := time.Now()
		rec := doJSON(router, http.MethodPost, path, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		newToken, ok := resp["token"].(string)
		if !ok || newToken == "" || newToken == oldToken {
			t.Fatalf("response = %v, want a fresh token", resp)
		}
		p := testdb.Get(t)
		var expiresAt time.Time
		var resentAt *time.Time
		if err := p.QueryRow(context.Background(), `
			select expires_at, resent_at from organization_invitations where id::text=$1
		`, id).Scan(&expiresAt, &resentAt); err != nil {
			t.Fatalf("read invitation: %v", err)
		}
		if resentAt == nil || resentAt.Before(before) {
			t.Fatalf("resent_at = %v, want set during resend", resentAt)
		}
		if expiresAt.Before(time.Now().Add(6 * 24 * time.Hour)) {
			t.Fatalf("expires_at = %v, want extended ~7 days", expiresAt)
		}
		if got := doJSON(router, http.MethodGet, "/api/v1/invitations/"+oldToken, http.Header{}, ""); got.Code != http.StatusNotFound {
			t.Fatalf("old token status = %d, want 404", got.Code)
		}
	})

	t.Run("non-pending invitation not found", func(t *testing.T) {
		doneID, _ := seedInvitation(t, org.OrgID, "done@example.com", "member", "accepted", 7*24*time.Hour)
		rec := doJSON(router, http.MethodPost,
			"/api/v1/organizations/"+org.OrgID+"/invitations/"+doneID+"/resend", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("unknown invitation not found", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost,
			"/api/v1/organizations/"+org.OrgID+"/invitations/00000000-0000-0000-0000-000000000000/resend", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("member forbidden", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		if rec := doJSON(router, http.MethodPost, path, member.Headers, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("url org mismatch forbidden", func(t *testing.T) {
		orgB := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations/"+orgB.OrgID+"/invitations/"+id+"/resend", org.Headers, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestRevokeOrganizationInvitation(t *testing.T) {
	router, _ := newOrgMembersRouter(t)
	org := testdb.SeedOrg(t)

	id, rawToken := seedInvitation(t, org.OrgID, "revoke@example.com", "member", "pending", 7*24*time.Hour)
	path := "/api/v1/organizations/" + org.OrgID + "/invitations/" + id + "/revoke"

	t.Run("revokes pending invitation", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, path, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		n := testdb.QueryCount(t, `select count(*) from organization_invitations where id::text=$1 and status='revoked'`, id)
		if n != 1 {
			t.Fatal("invitation status not revoked")
		}
		accepter := testdb.SeedOrgWithRole(t, rbac.RoleMember)
		if got := doJSON(router, http.MethodPost, "/api/v1/invitations/"+rawToken+"/accept", accepter.Headers, ""); got.Code != http.StatusBadRequest {
			t.Fatalf("accepting revoked invitation status = %d, want 400", got.Code)
		}
	})

	t.Run("member forbidden", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		if rec := doJSON(router, http.MethodPost, path, member.Headers, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("url org mismatch forbidden", func(t *testing.T) {
		orgB := testdb.SeedOrg(t)
		if rec := doJSON(router, http.MethodPost, "/api/v1/organizations/"+orgB.OrgID+"/invitations/"+id+"/revoke", org.Headers, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestDeleteOrganizationInvitation(t *testing.T) {
	router, _ := newOrgMembersRouter(t)
	org := testdb.SeedOrg(t)

	id, _ := seedInvitation(t, org.OrgID, "delete-me@example.com", "member", "pending", 7*24*time.Hour)
	path := "/api/v1/organizations/" + org.OrgID + "/invitations/" + id

	t.Run("deletes invitation", func(t *testing.T) {
		rec := doJSON(router, http.MethodDelete, path, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		if n := testdb.QueryCount(t, `select count(*) from organization_invitations where id::text=$1`, id); n != 0 {
			t.Fatal("invitation row not deleted")
		}
	})

	t.Run("member forbidden", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		if rec := doJSON(router, http.MethodDelete, path, member.Headers, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("url org mismatch forbidden", func(t *testing.T) {
		otherID, _ := seedInvitation(t, org.OrgID, "other@example.com", "member", "pending", 7*24*time.Hour)
		orgB := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodDelete, "/api/v1/organizations/"+orgB.OrgID+"/invitations/"+otherID, org.Headers, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestGetInvitationByToken(t *testing.T) {
	router, h := newOrgMembersRouter(t)
	org := testdb.SeedOrg(t)

	t.Run("returns pending invitation details", func(t *testing.T) {
		id, rawToken := seedInvitation(t, org.OrgID, "view@example.com", "admin", "pending", 7*24*time.Hour)
		rec := doJSON(router, http.MethodGet, "/api/v1/invitations/"+rawToken, http.Header{}, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		if resp["id"] != id || resp["organizationId"] != org.OrgID || resp["role"] != "admin" || resp["status"] != "pending" {
			t.Fatalf("response = %v, want invitation details", resp)
		}
	})

	t.Run("expired invitation not found", func(t *testing.T) {
		_, expiredToken := seedInvitation(t, org.OrgID, "expired@example.com", "member", "pending", -time.Hour)
		if rec := doJSON(router, http.MethodGet, "/api/v1/invitations/"+expiredToken, http.Header{}, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("unknown token not found", func(t *testing.T) {
		if rec := doJSON(router, http.MethodGet, "/api/v1/invitations/nope", http.Header{}, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	// Without routing context URLParam is empty, exercising the guard branch.
	rec := httptest.NewRecorder()
	h.GetInvitationByToken(rec, httptest.NewRequest(http.MethodGet, "/api/v1/invitations/", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty token status = %d, want 400", rec.Code)
	}
}

func TestAcceptInvitationByToken(t *testing.T) {
	router, h := newOrgMembersRouter(t)
	org := testdb.SeedOrg(t)

	t.Run("new user joins with invited role", func(t *testing.T) {
		id, rawToken := seedInvitation(t, org.OrgID, "joiner@example.com", "admin", "pending", 7*24*time.Hour)
		joiner := testdb.SeedOrg(t) // authenticated user of some other org

		rec := doJSON(router, http.MethodPost, "/api/v1/invitations/"+rawToken+"/accept", joiner.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		resp := decodeBody(t, rec)
		if resp["organizationId"] != org.OrgID || resp["status"] != "accepted" {
			t.Fatalf("response = %v, want accepted for %s", resp, org.OrgID)
		}
		p := testdb.Get(t)
		var role string
		if err := p.QueryRow(context.Background(), `
			select role::text from organization_members where organization_id=$1::uuid and user_id=$2::uuid
		`, org.OrgID, joiner.UserID).Scan(&role); err != nil || role != "admin" {
			t.Fatalf("membership role = %q err=%v, want admin", role, err)
		}
		n := testdb.QueryCount(t, `select count(*) from organization_invitations where id::text=$1 and status='accepted'`, id)
		if n != 1 {
			t.Fatal("invitation not marked accepted")
		}
	})

	t.Run("existing member upgrades via upsert", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		_, rawToken := seedInvitation(t, org.OrgID, "upgrade@example.com", "admin", "pending", 7*24*time.Hour)
		rec := doJSON(router, http.MethodPost, "/api/v1/invitations/"+rawToken+"/accept", member.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
		}
		n := testdb.QueryCount(t, `
			select count(*) from organization_members where organization_id=$1::uuid and user_id=$2::uuid and role='admin'
		`, org.OrgID, member.UserID)
		if n != 1 {
			t.Fatal("upsert did not upgrade existing membership to admin")
		}
	})

	t.Run("invalid token rejected", func(t *testing.T) {
		user := testdb.SeedOrg(t)
		if rec := doJSON(router, http.MethodPost, "/api/v1/invitations/bogus/accept", user.Headers, ""); rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("expired token rejected", func(t *testing.T) {
		_, expired := seedInvitation(t, org.OrgID, "late@example.com", "member", "pending", -time.Hour)
		user := testdb.SeedOrg(t)
		if rec := doJSON(router, http.MethodPost, "/api/v1/invitations/"+expired+"/accept", user.Headers, ""); rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	// Direct invocation covers branches the router cannot reach.
	t.Run("unauthenticated direct call", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.AcceptInvitationByToken(rec, httptest.NewRequest(http.MethodPost, "/api/v1/invitations/x/accept", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("empty token direct call", func(t *testing.T) {
		pool := testdb.Get(t)
		direct := NewHandler(pool)
		authed := httptest.NewRequest(http.MethodPost, "/api/v1/invitations//accept", nil)
		authed = authed.WithContext(apicxt.WithClaims(authed.Context(), &auth.Claims{UserID: org.UserID}))
		rec := httptest.NewRecorder()
		direct.AcceptInvitationByToken(rec, authed)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

// blockWrites installs a row trigger that fails every write of the given
// event ("update" or "delete") on the table, exercising exec-error branches
// without disturbing reads or RBAC lookups.
func blockWrites(t *testing.T, table, event string) {
	t.Helper()
	p := testdb.Get(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `
		create or replace function test_block_write_fn() returns trigger as $$
		begin raise exception 'blocked by test'; end
		$$ language plpgsql
	`); err != nil {
		t.Fatalf("create function: %v", err)
	}
	if _, err := p.Exec(ctx, fmt.Sprintf(
		"create trigger test_block_write_trg before %s on %s for each row execute function test_block_write_fn()",
		event, table)); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, fmt.Sprintf("drop trigger if exists test_block_write_trg on %s", table)); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
		if _, err := p.Exec(ctx, `drop function if exists test_block_write_fn()`); err != nil {
			t.Fatalf("drop function: %v", err)
		}
	})
}

func TestStatementFailuresReturnErrorResponses(t *testing.T) {
	router := simpleProtocolRouter(t)
	org := testdb.SeedOrg(t)
	member := org.AddMember(t, rbac.RoleMember)

	t.Run("list members query failure", func(t *testing.T) {
		renameColumn(t, "users", "display_name", "display_name_gone")
		rec := doJSON(router, http.MethodGet, "/api/v1/organizations/"+org.OrgID+"/members", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})

	t.Run("list invitations query failure", func(t *testing.T) {
		renameColumn(t, "organization_invitations", "expires_at", "expires_at_gone")
		rec := doJSON(router, http.MethodGet, "/api/v1/organizations/"+org.OrgID+"/invitations", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})

	t.Run("create invitation insert failure", func(t *testing.T) {
		renameColumn(t, "organization_invitations", "token_hash", "token_hash_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/invitations", org.Headers, `{"email":"f@example.com"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("role update exec failure", func(t *testing.T) {
		blockWrites(t, "organization_members", "update")
		rec := doJSON(router, http.MethodPut,
			"/api/v1/organizations/"+org.OrgID+"/members/"+member.UserID, org.Headers, `{"role":"admin"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete invitation exec failure", func(t *testing.T) {
		id, _ := seedInvitation(t, org.OrgID, "delfail@example.com", "member", "pending", 7*24*time.Hour)
		blockWrites(t, "organization_invitations", "delete")
		rec := doJSON(router, http.MethodDelete, "/api/v1/organizations/"+org.OrgID+"/invitations/"+id, org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("resend update exec failure", func(t *testing.T) {
		id, _ := seedInvitation(t, org.OrgID, "resendfail@example.com", "member", "pending", 7*24*time.Hour)
		blockWrites(t, "organization_invitations", "update")
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/invitations/"+id+"/resend", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("revoke update exec failure", func(t *testing.T) {
		id, _ := seedInvitation(t, org.OrgID, "revokefail@example.com", "member", "pending", 7*24*time.Hour)
		blockWrites(t, "organization_invitations", "update")
		rec := doJSON(router, http.MethodPost, "/api/v1/organizations/"+org.OrgID+"/invitations/"+id+"/revoke", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})
}
