package testdb

import "testing"

func TestGetPoolAndSeedOrg(t *testing.T) {
	p := Get(t)
	TruncateAll(t)
	f := SeedOrg(t)
	var role string
	if err := p.QueryRow(testContext, `
		select role::text from organization_members
		where organization_id = $1::uuid and user_id = $2::uuid
	`, f.OrgID, f.UserID).Scan(&role); err != nil {
		t.Fatalf("membership row missing: %v", err)
	}
	if role != "owner" {
		t.Fatalf("role = %q, want owner", role)
	}
	if f.Headers.Get("Authorization") == "" || f.Headers.Get("X-Organization-Id") != f.OrgID {
		t.Fatalf("headers not populated: %v", f.Headers)
	}
	claims, err := sharedAuth.ParseAccessToken(f.Token)
	if err != nil {
		t.Fatalf("token should verify with test secret: %v", err)
	}
	if claims.UserID != f.UserID {
		t.Fatalf("claims user = %q, want %q", claims.UserID, f.UserID)
	}
}

func TestTruncateCascade(t *testing.T) {
	TruncateAll(t)
	org := SeedOrg(t)
	app := SeedApplication(t, org.ProjectID, "", "https://github.com/example/repo.git", nil)
	Truncate(t, "organizations") // cascades through projects/applications
	if n := QueryCount(t, `select count(*) from applications where id = $1::uuid`, app); n != 0 {
		t.Fatalf("application survived cascade truncate")
	}
}
