package rbac

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Role is a member's role within an organization.
type Role string

// Organization member roles, ordered from most to least privileged.
const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// Require verifies that userID holds one of the allowed roles in the given
// organization; an empty allowed list only checks membership.
func Require(pool *pgxpool.Pool, organizationID, userID string, allowed ...Role) error {
	var current string
	err := pool.QueryRow(context.Background(), `
		select role::text
		from organization_members
		where organization_id = $1::uuid and user_id = $2::uuid
	`, organizationID, userID).Scan(&current)
	if err != nil {
		return err
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, r := range allowed {
		if current == string(r) {
			return nil
		}
	}
	return errors.New("forbidden")
}
