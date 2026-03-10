package store

import (
	"context"
)

func (s *Store) CreateUpdatePolicy(ctx context.Context, p *UpdatePolicy) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO update_policy (org_id, target_type, target_id, auto_update, auto_restart,
			maintenance_window_start, maintenance_window_end, maintenance_window_days,
			security_only, pre_update_backup, notify_on_update)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (org_id, target_type, target_id) DO UPDATE SET
			auto_update = EXCLUDED.auto_update,
			auto_restart = EXCLUDED.auto_restart,
			maintenance_window_start = EXCLUDED.maintenance_window_start,
			maintenance_window_end = EXCLUDED.maintenance_window_end,
			maintenance_window_days = EXCLUDED.maintenance_window_days,
			security_only = EXCLUDED.security_only,
			pre_update_backup = EXCLUDED.pre_update_backup,
			notify_on_update = EXCLUDED.notify_on_update,
			updated_at = NOW()
		RETURNING id, created_at, updated_at`,
		p.OrgID, p.TargetType, p.TargetID, p.AutoUpdate, p.AutoRestart,
		p.MaintenanceWindowStart, p.MaintenanceWindowEnd, p.MaintenanceWindowDays,
		p.SecurityOnly, p.PreUpdateBackup, p.NotifyOnUpdate,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (s *Store) GetUpdatePolicy(ctx context.Context, id string) (*UpdatePolicy, error) {
	p := &UpdatePolicy{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, target_type, target_id, auto_update, auto_restart,
			maintenance_window_start, maintenance_window_end, maintenance_window_days,
			security_only, pre_update_backup, notify_on_update, created_at, updated_at
		FROM update_policy WHERE id = $1`, id,
	).Scan(&p.ID, &p.OrgID, &p.TargetType, &p.TargetID, &p.AutoUpdate, &p.AutoRestart,
		&p.MaintenanceWindowStart, &p.MaintenanceWindowEnd, &p.MaintenanceWindowDays,
		&p.SecurityOnly, &p.PreUpdateBackup, &p.NotifyOnUpdate, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) GetUpdatePolicyByTarget(ctx context.Context, orgID, targetType, targetID string) (*UpdatePolicy, error) {
	p := &UpdatePolicy{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, target_type, target_id, auto_update, auto_restart,
			maintenance_window_start, maintenance_window_end, maintenance_window_days,
			security_only, pre_update_backup, notify_on_update, created_at, updated_at
		FROM update_policy WHERE org_id = $1 AND target_type = $2 AND target_id = $3`,
		orgID, targetType, targetID,
	).Scan(&p.ID, &p.OrgID, &p.TargetType, &p.TargetID, &p.AutoUpdate, &p.AutoRestart,
		&p.MaintenanceWindowStart, &p.MaintenanceWindowEnd, &p.MaintenanceWindowDays,
		&p.SecurityOnly, &p.PreUpdateBackup, &p.NotifyOnUpdate, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) ListUpdatePolicies(ctx context.Context, orgID string) ([]UpdatePolicy, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, target_type, target_id, auto_update, auto_restart,
			maintenance_window_start, maintenance_window_end, maintenance_window_days,
			security_only, pre_update_backup, notify_on_update, created_at, updated_at
		FROM update_policy WHERE org_id = $1 ORDER BY target_type, target_id`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var policies []UpdatePolicy
	for rows.Next() {
		var p UpdatePolicy
		if err := rows.Scan(&p.ID, &p.OrgID, &p.TargetType, &p.TargetID, &p.AutoUpdate, &p.AutoRestart,
			&p.MaintenanceWindowStart, &p.MaintenanceWindowEnd, &p.MaintenanceWindowDays,
			&p.SecurityOnly, &p.PreUpdateBackup, &p.NotifyOnUpdate, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}
	return policies, nil
}

func (s *Store) ListAutoUpdatePolicies(ctx context.Context) ([]UpdatePolicy, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, target_type, target_id, auto_update, auto_restart,
			maintenance_window_start, maintenance_window_end, maintenance_window_days,
			security_only, pre_update_backup, notify_on_update, created_at, updated_at
		FROM update_policy WHERE auto_update = true ORDER BY target_type, target_id`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var policies []UpdatePolicy
	for rows.Next() {
		var p UpdatePolicy
		if err := rows.Scan(&p.ID, &p.OrgID, &p.TargetType, &p.TargetID, &p.AutoUpdate, &p.AutoRestart,
			&p.MaintenanceWindowStart, &p.MaintenanceWindowEnd, &p.MaintenanceWindowDays,
			&p.SecurityOnly, &p.PreUpdateBackup, &p.NotifyOnUpdate, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}
	return policies, nil
}

func (s *Store) UpdateUpdatePolicy(ctx context.Context, p *UpdatePolicy) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE update_policy SET auto_update = $1, auto_restart = $2,
			maintenance_window_start = $3, maintenance_window_end = $4, maintenance_window_days = $5,
			security_only = $6, pre_update_backup = $7, notify_on_update = $8, updated_at = NOW()
		WHERE id = $9`,
		p.AutoUpdate, p.AutoRestart,
		p.MaintenanceWindowStart, p.MaintenanceWindowEnd, p.MaintenanceWindowDays,
		p.SecurityOnly, p.PreUpdateBackup, p.NotifyOnUpdate, p.ID,
	)
	return err
}

func (s *Store) DeleteUpdatePolicy(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM update_policy WHERE id = $1`, id)
	return err
}
