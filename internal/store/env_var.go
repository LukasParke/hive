package store

import "context"

func (s *Store) CreateAppEnvVar(ctx context.Context, ev *AppEnvVar) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO app_env_var (app_id, key, value_encrypted, is_secret, source) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at, updated_at`,
		ev.AppID, ev.Key, ev.ValueEncrypted, ev.IsSecret, ev.Source,
	).Scan(&ev.ID, &ev.CreatedAt, &ev.UpdatedAt)
}

func (s *Store) ListAppEnvVars(ctx context.Context, appID string) ([]AppEnvVar, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, key, value_encrypted, is_secret, source, created_at, updated_at FROM app_env_var WHERE app_id = $1 ORDER BY key`,
		appID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var vars []AppEnvVar
	for rows.Next() {
		var ev AppEnvVar
		if err := rows.Scan(&ev.ID, &ev.AppID, &ev.Key, &ev.ValueEncrypted, &ev.IsSecret, &ev.Source, &ev.CreatedAt, &ev.UpdatedAt); err != nil {
			return nil, err
		}
		vars = append(vars, ev)
	}
	return vars, nil
}

func (s *Store) GetAppEnvVar(ctx context.Context, id string) (*AppEnvVar, error) {
	ev := &AppEnvVar{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, key, value_encrypted, is_secret, source, created_at, updated_at FROM app_env_var WHERE id = $1`, id,
	).Scan(&ev.ID, &ev.AppID, &ev.Key, &ev.ValueEncrypted, &ev.IsSecret, &ev.Source, &ev.CreatedAt, &ev.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func (s *Store) GetAppEnvVarByKey(ctx context.Context, appID, key string) (*AppEnvVar, error) {
	ev := &AppEnvVar{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, key, value_encrypted, is_secret, source, created_at, updated_at FROM app_env_var WHERE app_id = $1 AND key = $2`,
		appID, key,
	).Scan(&ev.ID, &ev.AppID, &ev.Key, &ev.ValueEncrypted, &ev.IsSecret, &ev.Source, &ev.CreatedAt, &ev.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func (s *Store) UpdateAppEnvVar(ctx context.Context, ev *AppEnvVar) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app_env_var SET value_encrypted = $1, is_secret = $2, updated_at = NOW() WHERE id = $3`,
		ev.ValueEncrypted, ev.IsSecret, ev.ID,
	)
	return err
}

func (s *Store) DeleteAppEnvVar(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_env_var WHERE id = $1`, id)
	return err
}

func (s *Store) DeleteAppEnvVarByKey(ctx context.Context, appID, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_env_var WHERE app_id = $1 AND key = $2`, appID, key)
	return err
}

func (s *Store) BulkUpsertAppEnvVars(ctx context.Context, appID string, vars []AppEnvVar) error {
	for _, ev := range vars {
		ev.AppID = appID
		ev.Source = "user"
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO app_env_var (app_id, key, value_encrypted, is_secret, source) VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (app_id, key) DO UPDATE SET value_encrypted = EXCLUDED.value_encrypted, is_secret = EXCLUDED.is_secret, updated_at = NOW()`,
			ev.AppID, ev.Key, ev.ValueEncrypted, ev.IsSecret, ev.Source,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
