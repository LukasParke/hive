package store

import (
	"context"
	"time"
)

func (s *Store) CreateBackupConfig(ctx context.Context, bc *BackupConfig) error {
	if bc.BackupType == "" {
		bc.BackupType = "database"
	}
	if bc.Destination == "" {
		if bc.S3Bucket != "" {
			bc.Destination = "s3"
		} else {
			bc.Destination = "local"
		}
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO backup_config (resource_id, schedule, s3_bucket, s3_prefix, backup_type, volume_id, destination, nas_host_id, nas_path, local_path, retention_days)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id, created_at`,
		bc.ResourceID, bc.Schedule, bc.S3Bucket, bc.S3Prefix, bc.BackupType, bc.VolumeID,
		bc.Destination, bc.NASHostID, bc.NASPath, bc.LocalPath, bc.RetentionDays,
	).Scan(&bc.ID, &bc.CreatedAt)
}

func (s *Store) GetBackupConfig(ctx context.Context, id string) (*BackupConfig, error) {
	bc := &BackupConfig{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, resource_id, schedule, s3_bucket, s3_prefix, backup_type, volume_id,
		        destination, nas_host_id, nas_path, local_path, retention_days, created_at
		 FROM backup_config WHERE id = $1`, id,
	).Scan(&bc.ID, &bc.ResourceID, &bc.Schedule, &bc.S3Bucket, &bc.S3Prefix, &bc.BackupType, &bc.VolumeID,
		&bc.Destination, &bc.NASHostID, &bc.NASPath, &bc.LocalPath, &bc.RetentionDays, &bc.CreatedAt)
	if err != nil {
		return nil, err
	}
	return bc, nil
}

func (s *Store) ListBackupConfigs(ctx context.Context) ([]BackupConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, resource_id, schedule, s3_bucket, s3_prefix, backup_type, volume_id,
		        destination, nas_host_id, nas_path, local_path, retention_days, created_at
		 FROM backup_config ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var configs []BackupConfig
	for rows.Next() {
		var bc BackupConfig
		if err := rows.Scan(&bc.ID, &bc.ResourceID, &bc.Schedule, &bc.S3Bucket, &bc.S3Prefix, &bc.BackupType, &bc.VolumeID,
			&bc.Destination, &bc.NASHostID, &bc.NASPath, &bc.LocalPath, &bc.RetentionDays, &bc.CreatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, bc)
	}
	return configs, nil
}

func (s *Store) ListBackupConfigsByOrg(ctx context.Context, orgID string) ([]BackupConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT bc.id, bc.resource_id, bc.schedule, bc.s3_bucket, bc.s3_prefix, bc.backup_type, bc.volume_id,
		        bc.destination, bc.nas_host_id, bc.nas_path, bc.local_path, bc.retention_days, bc.created_at
		 FROM backup_config bc
		 LEFT JOIN managed_database md ON md.id = bc.resource_id
		 LEFT JOIN volume v ON v.id = bc.volume_id
		 LEFT JOIN project p1 ON p1.id = md.project_id
		 LEFT JOIN project p2 ON p2.id = v.project_id
		 WHERE COALESCE(p1.org_id, p2.org_id) = $1
		 ORDER BY bc.created_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var configs []BackupConfig
	for rows.Next() {
		var bc BackupConfig
		if err := rows.Scan(&bc.ID, &bc.ResourceID, &bc.Schedule, &bc.S3Bucket, &bc.S3Prefix, &bc.BackupType, &bc.VolumeID,
			&bc.Destination, &bc.NASHostID, &bc.NASPath, &bc.LocalPath, &bc.RetentionDays, &bc.CreatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, bc)
	}
	return configs, nil
}

func (s *Store) UpdateBackupConfig(ctx context.Context, bc *BackupConfig) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE backup_config SET schedule = $1, s3_bucket = $2, s3_prefix = $3 WHERE id = $4`,
		bc.Schedule, bc.S3Bucket, bc.S3Prefix, bc.ID,
	)
	return err
}

func (s *Store) DeleteBackupConfig(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backup_config WHERE id = $1`, id)
	return err
}

func (s *Store) CreateBackupRun(ctx context.Context, br *BackupRun) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO backup_run (config_id, status) VALUES ($1, $2) RETURNING id, started_at`,
		br.ConfigID, br.Status,
	).Scan(&br.ID, &br.StartedAt)
}

func (s *Store) UpdateBackupRun(ctx context.Context, id, status string, size int64, targetPath string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE backup_run SET status = $1, size = $2, target_path = $3, finished_at = NOW() WHERE id = $4`,
		status, size, targetPath, id,
	)
	return err
}

func (s *Store) GetBackupRun(ctx context.Context, id string) (*BackupRun, error) {
	br := &BackupRun{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, config_id, status, size, target_path, started_at, finished_at FROM backup_run WHERE id = $1`, id,
	).Scan(&br.ID, &br.ConfigID, &br.Status, &br.Size, &br.TargetPath, &br.StartedAt, &br.FinishedAt)
	if err != nil {
		return nil, err
	}
	return br, nil
}

func (s *Store) ListBackupRuns(ctx context.Context, configID string) ([]BackupRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, config_id, status, size, target_path, started_at, finished_at FROM backup_run WHERE config_id = $1 ORDER BY started_at DESC LIMIT 50`,
		configID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var runs []BackupRun
	for rows.Next() {
		var br BackupRun
		if err := rows.Scan(&br.ID, &br.ConfigID, &br.Status, &br.Size, &br.TargetPath, &br.StartedAt, &br.FinishedAt); err != nil {
			return nil, err
		}
		runs = append(runs, br)
	}
	return runs, nil
}

// ReconcileStaleRuns marks backup_run and maintenance_run records that have been
// stuck in "running" state longer than the given threshold as "failed" with a
// reason indicating the run was abandoned (likely due to node/process failure).
func (s *Store) ReconcileStaleRuns(ctx context.Context, staleThreshold time.Duration) (int64, error) {
	cutoff := time.Now().Add(-staleThreshold)

	res1, err := s.db.ExecContext(ctx,
		`UPDATE backup_run SET status = 'failed', target_path = 'stale: abandoned after timeout', finished_at = NOW()
		 WHERE status = 'running' AND started_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	n1, _ := res1.RowsAffected()

	res2, err := s.db.ExecContext(ctx,
		`UPDATE maintenance_run SET status = 'failed', details = 'stale: abandoned after timeout', finished_at = NOW()
		 WHERE status = 'running' AND started_at < $1`, cutoff)
	if err != nil {
		return n1, err
	}
	n2, _ := res2.RowsAffected()

	return n1 + n2, nil
}
