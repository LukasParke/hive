package store

import (
	"context"
)

func (s *Store) CreateDeployment(ctx context.Context, d *Deployment) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO deployment (app_id, status, commit_sha, image_digest, logs) VALUES ($1, $2, $3, $4, $5) RETURNING id, started_at`,
		d.AppID, d.Status, d.CommitSHA, d.ImageDigest, d.Logs,
	).Scan(&d.ID, &d.StartedAt)
}

func (s *Store) UpdateDeployment(ctx context.Context, id string, status string, logs string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deployment SET status = $1, logs = $2, finished_at = NOW() WHERE id = $3`,
		status, logs, id,
	)
	return err
}

func (s *Store) ListDeployments(ctx context.Context, appID string) ([]Deployment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, status, commit_sha, image_digest, logs, started_at, finished_at
		 FROM deployment WHERE app_id = $1 ORDER BY started_at DESC LIMIT 50`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deployments []Deployment
	for rows.Next() {
		var d Deployment
		if err := rows.Scan(&d.ID, &d.AppID, &d.Status, &d.CommitSHA, &d.ImageDigest, &d.Logs, &d.StartedAt, &d.FinishedAt); err != nil {
			return nil, err
		}
		deployments = append(deployments, d)
	}
	return deployments, nil
}

func (s *Store) GetDeployment(ctx context.Context, id string) (*Deployment, error) {
	d := &Deployment{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, status, commit_sha, image_digest, logs, started_at, finished_at FROM deployment WHERE id = $1`, id,
	).Scan(&d.ID, &d.AppID, &d.Status, &d.CommitSHA, &d.ImageDigest, &d.Logs, &d.StartedAt, &d.FinishedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) DeleteDeployment(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM deployment WHERE id = $1`, id)
	return err
}

func (s *Store) UpdateDeploymentStatus(ctx context.Context, id, status, logs string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deployment SET status = $1, logs = CASE WHEN $2 = '' THEN logs ELSE $2 END, finished_at = NOW() WHERE id = $3`,
		status, logs, id,
	)
	return err
}

func (s *Store) AppendDeploymentLogs(ctx context.Context, id, logs string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deployment SET logs = logs || $1 WHERE id = $2`,
		logs, id,
	)
	return err
}
