package rbac_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

func TestRoleConstants(t *testing.T) {
	cases := []struct {
		role rbac.Role
		want string
	}{
		{rbac.RoleOwner, "owner"},
		{rbac.RoleAdmin, "admin"},
		{rbac.RoleMember, "member"},
	}
	for _, tc := range cases {
		if got := string(tc.role); got != tc.want {
			t.Fatalf("role constant = %q, want %q", got, tc.want)
		}
	}
}

func TestRequireAllowsMatchingRole(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)

	cases := []struct {
		name    string
		role    rbac.Role
		allowed []rbac.Role
	}{
		{"owner in owner list", rbac.RoleOwner, []rbac.Role{rbac.RoleOwner}},
		{"admin in owner-admin list", rbac.RoleAdmin, []rbac.Role{rbac.RoleOwner, rbac.RoleAdmin}},
		{"member in member list", rbac.RoleMember, []rbac.Role{rbac.RoleMember}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			org := testdb.SeedOrgWithRole(t, tc.role)
			if err := rbac.Require(testdb.Get(t), org.OrgID, org.UserID, tc.allowed...); err != nil {
				t.Fatalf("Require allowed role: %v", err)
			}
		})
	}
}

func TestRequireRejectsInsufficientRole(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)

	cases := []struct {
		name    string
		role    rbac.Role
		allowed []rbac.Role
	}{
		{"member against owner", rbac.RoleMember, []rbac.Role{rbac.RoleOwner}},
		{"member against owner-admin", rbac.RoleMember, []rbac.Role{rbac.RoleOwner, rbac.RoleAdmin}},
		{"admin against owner", rbac.RoleAdmin, []rbac.Role{rbac.RoleOwner}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			org := testdb.SeedOrgWithRole(t, tc.role)
			err := rbac.Require(testdb.Get(t), org.OrgID, org.UserID, tc.allowed...)
			if err == nil || err.Error() != "forbidden" {
				t.Fatalf("Require insufficient role err = %v, want forbidden", err)
			}
		})
	}
}

func TestRequireEmptyAllowedOnlyChecksMembership(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrgWithRole(t, rbac.RoleMember)

	// Any role passes when no allowed list is given, as long as the user is a
	// member of the organization.
	if err := rbac.Require(testdb.Get(t), org.OrgID, org.UserID); err != nil {
		t.Fatalf("Require membership-only: %v", err)
	}
}

func TestRequireMissingMembershipPropagatesNoRows(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	outsider := testdb.SeedOrg(t) // separate org, no membership in org

	err := rbac.Require(testdb.Get(t), org.OrgID, outsider.UserID, rbac.RoleOwner)
	if err == nil {
		t.Fatal("Require non-member err = nil, want error")
	}
	if err != pgx.ErrNoRows {
		t.Fatalf("Require non-member err = %v, want pgx.ErrNoRows", err)
	}
}

func TestRequireErrorPropagationOnBadInput(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)

	cases := []struct {
		name string
		org  string
		user string
	}{
		{"malformed organization id", "not-a-uuid", org.UserID},
		{"malformed user id", org.OrgID, "not-a-uuid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := rbac.Require(testdb.Get(t), tc.org, tc.user)
			if err == nil {
				t.Fatal("Require malformed input err = nil, want error")
			}
		})
	}
}
