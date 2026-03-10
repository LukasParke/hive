package store

import "context"

func (s *Store) CreateOrgRole(ctx context.Context, or *OrgRole) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO org_role (org_id, user_id, role) VALUES ($1, $2, $3) RETURNING id, created_at`,
		or.OrgID, or.UserID, or.Role,
	).Scan(&or.ID, &or.CreatedAt)
}

func (s *Store) GetOrgRole(ctx context.Context, orgID, userID string) (*OrgRole, error) {
	or := &OrgRole{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, user_id, role, created_at FROM org_role WHERE org_id = $1 AND user_id = $2`,
		orgID, userID,
	).Scan(&or.ID, &or.OrgID, &or.UserID, &or.Role, &or.CreatedAt)
	if err != nil {
		return nil, err
	}
	return or, nil
}

func (s *Store) ListOrgRoles(ctx context.Context, orgID string) ([]OrgRole, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, user_id, role, created_at FROM org_role WHERE org_id = $1 ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var roles []OrgRole
	for rows.Next() {
		var or OrgRole
		if err := rows.Scan(&or.ID, &or.OrgID, &or.UserID, &or.Role, &or.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, or)
	}
	return roles, nil
}

func (s *Store) UpdateOrgRole(ctx context.Context, orgID, userID, role string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE org_role SET role = $1 WHERE org_id = $2 AND user_id = $3`,
		role, orgID, userID,
	)
	return err
}

func (s *Store) DeleteOrgRole(ctx context.Context, orgID, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM org_role WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	return err
}
