package store

import (
	"context"
	"encoding/json"
	"time"
)

type DashboardLayout struct {
	ID        string          `json:"id"`
	UserID    string          `json:"user_id"`
	OrgID     string          `json:"org_id"`
	Layout    json.RawMessage `json:"layout"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func (s *Store) GetDashboardLayout(ctx context.Context, userID, orgID string) (*DashboardLayout, error) {
	d := &DashboardLayout{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, org_id, layout, created_at, updated_at
		 FROM dashboard_layout WHERE user_id = $1 AND org_id = $2`, userID, orgID,
	).Scan(&d.ID, &d.UserID, &d.OrgID, &d.Layout, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

func (s *Store) UpsertDashboardLayout(ctx context.Context, userID, orgID string, layout json.RawMessage) (*DashboardLayout, error) {
	d := &DashboardLayout{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO dashboard_layout (user_id, org_id, layout) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, org_id) DO UPDATE SET layout=$3, updated_at=NOW()
		 RETURNING id, user_id, org_id, layout, created_at, updated_at`,
		userID, orgID, layout,
	).Scan(&d.ID, &d.UserID, &d.OrgID, &d.Layout, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}
