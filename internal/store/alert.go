package store

import (
	"context"
)

func (s *Store) CreateAlertThreshold(ctx context.Context, at *AlertThreshold) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO alert_threshold (org_id, metric, operator, value, cooldown_minutes, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		at.OrgID, at.Metric, at.Operator, at.Value, at.CooldownMinutes, at.Enabled,
	).Scan(&at.ID, &at.CreatedAt)
}

func (s *Store) ListAlertThresholds(ctx context.Context, orgID string) ([]AlertThreshold, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, metric, operator, value, cooldown_minutes, enabled, last_fired_at, created_at
		 FROM alert_threshold WHERE org_id = $1 ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var thresholds []AlertThreshold
	for rows.Next() {
		var at AlertThreshold
		if err := rows.Scan(&at.ID, &at.OrgID, &at.Metric, &at.Operator, &at.Value, &at.CooldownMinutes, &at.Enabled, &at.LastFiredAt, &at.CreatedAt); err != nil {
			return nil, err
		}
		thresholds = append(thresholds, at)
	}
	return thresholds, nil
}

func (s *Store) GetAlertThreshold(ctx context.Context, id string) (*AlertThreshold, error) {
	at := &AlertThreshold{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, metric, operator, value, cooldown_minutes, enabled, last_fired_at, created_at
		 FROM alert_threshold WHERE id = $1`, id,
	).Scan(&at.ID, &at.OrgID, &at.Metric, &at.Operator, &at.Value, &at.CooldownMinutes, &at.Enabled, &at.LastFiredAt, &at.CreatedAt)
	if err != nil {
		return nil, err
	}
	return at, nil
}

func (s *Store) UpdateAlertThreshold(ctx context.Context, at *AlertThreshold) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE alert_threshold SET metric = $1, operator = $2, value = $3, cooldown_minutes = $4, enabled = $5 WHERE id = $6`,
		at.Metric, at.Operator, at.Value, at.CooldownMinutes, at.Enabled, at.ID,
	)
	return err
}

func (s *Store) DeleteAlertThreshold(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM alert_threshold WHERE id = $1`, id)
	return err
}

func (s *Store) ListAllAlertThresholds(ctx context.Context) ([]AlertThreshold, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, metric, operator, value, cooldown_minutes, enabled, last_fired_at, created_at
		 FROM alert_threshold WHERE enabled = true ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var thresholds []AlertThreshold
	for rows.Next() {
		var at AlertThreshold
		if err := rows.Scan(&at.ID, &at.OrgID, &at.Metric, &at.Operator, &at.Value, &at.CooldownMinutes, &at.Enabled, &at.LastFiredAt, &at.CreatedAt); err != nil {
			return nil, err
		}
		thresholds = append(thresholds, at)
	}
	return thresholds, nil
}

func (s *Store) UpdateAlertThresholdFired(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alert_threshold SET last_fired_at = NOW() WHERE id = $1`, id)
	return err
}
