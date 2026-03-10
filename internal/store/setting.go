package store

import "context"

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM setting WHERE key = $1`, key).Scan(&val)
	if err != nil {
		return "", err
	}
	return val, nil
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO setting (key, value, updated_at) VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()`,
		key, value,
	)
	return err
}

func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM setting WHERE key = $1`, key)
	return err
}
