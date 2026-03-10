package store

import "context"

func (s *Store) CreateServiceLink(ctx context.Context, sl *ServiceLink) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO service_link (source_app_id, target_app_id, target_database_id, env_prefix)
		 VALUES ($1, NULLIF($2,''), NULLIF($3,''), $4) RETURNING id, created_at`,
		sl.SourceAppID, sl.TargetAppID, sl.TargetDatabaseID, sl.EnvPrefix,
	).Scan(&sl.ID, &sl.CreatedAt)
}

func (s *Store) ListServiceLinks(ctx context.Context, appID string) ([]ServiceLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source_app_id, COALESCE(target_app_id,''), COALESCE(target_database_id,''), env_prefix, created_at
		 FROM service_link WHERE source_app_id = $1 ORDER BY created_at DESC`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var links []ServiceLink
	for rows.Next() {
		var sl ServiceLink
		if err := rows.Scan(&sl.ID, &sl.SourceAppID, &sl.TargetAppID, &sl.TargetDatabaseID, &sl.EnvPrefix, &sl.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, sl)
	}
	return links, nil
}

func (s *Store) DeleteServiceLink(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM service_link WHERE id = $1`, id)
	return err
}
