package store

import "context"

func (s *Store) CreatePreviewDeployment(ctx context.Context, pd *PreviewDeployment) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO preview_deployment (app_id, branch, pr_number, domain, status, service_name) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at, updated_at`,
		pd.AppID, pd.Branch, pd.PRNumber, pd.Domain, pd.Status, pd.ServiceName,
	).Scan(&pd.ID, &pd.CreatedAt, &pd.UpdatedAt)
}

func (s *Store) ListPreviewDeployments(ctx context.Context, appID string) ([]PreviewDeployment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, branch, pr_number, domain, status, service_name, created_at, updated_at
		 FROM preview_deployment WHERE app_id = $1 ORDER BY created_at DESC`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var previews []PreviewDeployment
	for rows.Next() {
		var pd PreviewDeployment
		if err := rows.Scan(&pd.ID, &pd.AppID, &pd.Branch, &pd.PRNumber, &pd.Domain, &pd.Status, &pd.ServiceName, &pd.CreatedAt, &pd.UpdatedAt); err != nil {
			return nil, err
		}
		previews = append(previews, pd)
	}
	return previews, nil
}

func (s *Store) GetPreviewDeployment(ctx context.Context, id string) (*PreviewDeployment, error) {
	pd := &PreviewDeployment{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, branch, pr_number, domain, status, service_name, created_at, updated_at
		 FROM preview_deployment WHERE id = $1`, id,
	).Scan(&pd.ID, &pd.AppID, &pd.Branch, &pd.PRNumber, &pd.Domain, &pd.Status, &pd.ServiceName, &pd.CreatedAt, &pd.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return pd, nil
}

func (s *Store) UpdatePreviewDeploymentStatus(ctx context.Context, id string, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE preview_deployment SET status = $1, updated_at = NOW() WHERE id = $2`, status, id)
	return err
}

func (s *Store) DeletePreviewDeployment(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM preview_deployment WHERE id = $1`, id)
	return err
}
