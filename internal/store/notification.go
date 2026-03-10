package store

import (
	"context"
)

func (s *Store) CreateNotificationChannel(ctx context.Context, nc *NotificationChannel) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO notification_channel (org_id, name, type, config) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		nc.OrgID, nc.Name, nc.Type, nc.Config,
	).Scan(&nc.ID, &nc.CreatedAt)
}

func (s *Store) ListNotificationChannels(ctx context.Context, orgID string) ([]NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, type, config, created_at FROM notification_channel WHERE org_id = $1 ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var channels []NotificationChannel
	for rows.Next() {
		var ch NotificationChannel
		if err := rows.Scan(&ch.ID, &ch.OrgID, &ch.Name, &ch.Type, &ch.Config, &ch.CreatedAt); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, nil
}

func (s *Store) GetNotificationChannel(ctx context.Context, id string) (*NotificationChannel, error) {
	ch := &NotificationChannel{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, name, type, config, created_at FROM notification_channel WHERE id = $1`, id,
	).Scan(&ch.ID, &ch.OrgID, &ch.Name, &ch.Type, &ch.Config, &ch.CreatedAt)
	if err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *Store) DeleteNotificationChannel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_channel WHERE id = $1`, id)
	return err
}

func (s *Store) ListAllNotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, type, config, created_at FROM notification_channel ORDER BY created_at DESC LIMIT 100`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var channels []NotificationChannel
	for rows.Next() {
		var ch NotificationChannel
		if err := rows.Scan(&ch.ID, &ch.OrgID, &ch.Name, &ch.Type, &ch.Config, &ch.CreatedAt); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, nil
}

func (s *Store) CreateNotificationEvent(ctx context.Context, ne *NotificationEvent) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO notification_event (channel_id, event_type, title, message, status) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		ne.ChannelID, ne.EventType, ne.Title, ne.Message, ne.Status,
	).Scan(&ne.ID, &ne.CreatedAt)
}

func (s *Store) ListNotificationEvents(ctx context.Context, orgID string, limit int) ([]NotificationEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT ne.id, ne.channel_id, ne.event_type, ne.title, ne.message, ne.status, ne.created_at
		 FROM notification_event ne JOIN notification_channel nc ON ne.channel_id = nc.id
		 WHERE nc.org_id = $1 ORDER BY ne.created_at DESC LIMIT $2`,
		orgID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var events []NotificationEvent
	for rows.Next() {
		var ne NotificationEvent
		if err := rows.Scan(&ne.ID, &ne.ChannelID, &ne.EventType, &ne.Title, &ne.Message, &ne.Status, &ne.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, ne)
	}
	return events, nil
}
