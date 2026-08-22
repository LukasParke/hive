package nodeops

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/testdb"
	swarm "github.com/moby/moby/api/types/swarm"
)

// TestAuthorizeUsesOrgRBAC exercises the real RBAC gate (authorizeOverride
// nil): an organization owner passes and a non-member is rejected with 403.
func TestAuthorizeUsesOrgRBAC(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)

	fake := &fakeSwarm{nodes: map[string]swarm.Node{
		"w1": newNode("w1", "worker", "active", nil),
	}}
	h := NewHandler(pool, fake)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Post("/api/v1/nodes/{id}/drain", h.DrainNode)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/w1/drain", nil)
	for k, vs := range org.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("org owner drain status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := fake.nodes["w1"].Spec.Availability; got != "drain" {
		t.Fatalf("availability = %q, want drain", got)
	}

	// A user without membership in the organization is denied.
	ctx := context.Background()
	authSvc := testdb.Auth(t)
	email := fmt.Sprintf("outsider-%s@test.local", strings.ReplaceAll(org.OrgID, "-", "")[:12])
	if _, err := authSvc.Register(ctx, email, "sup3rsecret!", "Outsider"); err != nil {
		t.Fatalf("register outsider: %v", err)
	}
	token, _, err := authSvc.Login(ctx, email, "sup3rsecret!")
	if err != nil {
		t.Fatalf("login outsider: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/nodes/w1/drain", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Organization-Id", org.OrgID)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member drain status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if len(fake.removeCalls) != 0 {
		t.Fatal("denied request must not reach the swarm")
	}
}
