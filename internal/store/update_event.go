package store

import (
	"context"
)

func (s *Store) CreateUpdateEvent(ctx context.Context, e *UpdateEvent) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO update_event (event_type, target_type, target_id, target_name,
			previous_version, new_version, status, details, triggered_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id, started_at`,
		e.EventType, e.TargetType, e.TargetID, e.TargetName,
		e.PreviousVersion, e.NewVersion, e.Status, e.Details, e.TriggeredBy,
	).Scan(&e.ID, &e.StartedAt)
}

func (s *Store) UpdateUpdateEvent(ctx context.Context, id, status, details string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE update_event SET status = $1, details = $2, finished_at = NOW() WHERE id = $3`,
		status, details, id,
	)
	return err
}

func (s *Store) GetUpdateEvent(ctx context.Context, id string) (*UpdateEvent, error) {
	e := &UpdateEvent{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, event_type, target_type, target_id, target_name, previous_version,
			new_version, status, details, triggered_by, started_at, finished_at
		FROM update_event WHERE id = $1`, id,
	).Scan(&e.ID, &e.EventType, &e.TargetType, &e.TargetID, &e.TargetName, &e.PreviousVersion,
		&e.NewVersion, &e.Status, &e.Details, &e.TriggeredBy, &e.StartedAt, &e.FinishedAt)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (s *Store) ListUpdateEvents(ctx context.Context, limit int) ([]UpdateEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, target_type, target_id, target_name, previous_version,
			new_version, status, details, triggered_by, started_at, finished_at
		FROM update_event ORDER BY started_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var events []UpdateEvent
	for rows.Next() {
		var e UpdateEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.TargetType, &e.TargetID, &e.TargetName, &e.PreviousVersion,
			&e.NewVersion, &e.Status, &e.Details, &e.TriggeredBy, &e.StartedAt, &e.FinishedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

func (s *Store) ListUpdateEventsByTarget(ctx context.Context, targetType, targetID string, limit int) ([]UpdateEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, target_type, target_id, target_name, previous_version,
			new_version, status, details, triggered_by, started_at, finished_at
		FROM update_event WHERE target_type = $1 AND target_id = $2
		ORDER BY started_at DESC LIMIT $3`, targetType, targetID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var events []UpdateEvent
	for rows.Next() {
		var e UpdateEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.TargetType, &e.TargetID, &e.TargetName, &e.PreviousVersion,
			&e.NewVersion, &e.Status, &e.Details, &e.TriggeredBy, &e.StartedAt, &e.FinishedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

func (s *Store) ListUpdateEventsByType(ctx context.Context, eventType string, limit int) ([]UpdateEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, target_type, target_id, target_name, previous_version,
			new_version, status, details, triggered_by, started_at, finished_at
		FROM update_event WHERE event_type = $1
		ORDER BY started_at DESC LIMIT $2`, eventType, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var events []UpdateEvent
	for rows.Next() {
		var e UpdateEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.TargetType, &e.TargetID, &e.TargetName, &e.PreviousVersion,
			&e.NewVersion, &e.Status, &e.Details, &e.TriggeredBy, &e.StartedAt, &e.FinishedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}
