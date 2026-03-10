package store

import (
	"context"
)

func (s *Store) CreateSecret(ctx context.Context, sec *Secret) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO secret (project_id, name, docker_secret_id, description) VALUES ($1, $2, $3, $4) RETURNING id, created_at, updated_at`,
		sec.ProjectID, sec.Name, sec.DockerSecretID, sec.Description,
	).Scan(&sec.ID, &sec.CreatedAt, &sec.UpdatedAt)
}

func (s *Store) ListSecrets(ctx context.Context, projectID string) ([]Secret, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, docker_secret_id, description, created_at, updated_at FROM secret WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var secrets []Secret
	for rows.Next() {
		var sec Secret
		if err := rows.Scan(&sec.ID, &sec.ProjectID, &sec.Name, &sec.DockerSecretID, &sec.Description, &sec.CreatedAt, &sec.UpdatedAt); err != nil {
			return nil, err
		}
		secrets = append(secrets, sec)
	}
	return secrets, nil
}

func (s *Store) GetSecret(ctx context.Context, id string) (*Secret, error) {
	sec := &Secret{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, docker_secret_id, description, created_at, updated_at FROM secret WHERE id = $1`, id,
	).Scan(&sec.ID, &sec.ProjectID, &sec.Name, &sec.DockerSecretID, &sec.Description, &sec.CreatedAt, &sec.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return sec, nil
}

func (s *Store) DeleteSecret(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM secret WHERE id = $1`, id)
	return err
}

func (s *Store) AttachSecret(ctx context.Context, as *AppSecret) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_secret (app_id, secret_id, target, uid, gid, mode) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (app_id, secret_id) DO UPDATE SET target=$3, uid=$4, gid=$5, mode=$6`,
		as.AppID, as.SecretID, as.Target, as.UID, as.GID, as.Mode,
	)
	return err
}

func (s *Store) DetachSecret(ctx context.Context, appID, secretID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_secret WHERE app_id = $1 AND secret_id = $2`, appID, secretID)
	return err
}

func (s *Store) ListAppSecrets(ctx context.Context, appID string) ([]AppSecret, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT app_id, secret_id, target, uid, gid, mode FROM app_secret WHERE app_id = $1`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []AppSecret
	for rows.Next() {
		var as AppSecret
		if err := rows.Scan(&as.AppID, &as.SecretID, &as.Target, &as.UID, &as.GID, &as.Mode); err != nil {
			return nil, err
		}
		result = append(result, as)
	}
	return result, nil
}
