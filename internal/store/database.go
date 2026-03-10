package store

import (
	"context"
)

func (s *Store) CreateManagedDatabase(ctx context.Context, d *ManagedDatabase) error {
	if d.StorageMode == "" {
		d.StorageMode = "local"
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO managed_database (project_id, name, db_type, version, connection_encrypted, storage_mode, storage_host_id, node_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		d.ProjectID, d.Name, d.DBType, d.Version, d.ConnectionEncrypted, d.StorageMode, d.StorageHostID, d.NodeID,
	).Scan(&d.ID, &d.CreatedAt)
}

func (s *Store) GetManagedDatabase(ctx context.Context, id string) (*ManagedDatabase, error) {
	d := &ManagedDatabase{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, db_type, version, status, connection_encrypted,
		        storage_mode, storage_host_id, node_id, created_at
		 FROM managed_database WHERE id = $1`, id,
	).Scan(&d.ID, &d.ProjectID, &d.Name, &d.DBType, &d.Version, &d.Status, &d.ConnectionEncrypted,
		&d.StorageMode, &d.StorageHostID, &d.NodeID, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) UpdateManagedDatabaseStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE managed_database SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (s *Store) UpdateManagedDatabaseConnection(ctx context.Context, id string, connEncrypted []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE managed_database SET connection_encrypted = $1, status = 'running' WHERE id = $2`, connEncrypted, id)
	return err
}

func (s *Store) DeleteManagedDatabase(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM managed_database WHERE id = $1`, id)
	return err
}

func (s *Store) ListManagedDatabases(ctx context.Context, projectID string) ([]ManagedDatabase, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, db_type, version, status, connection_encrypted,
		        storage_mode, storage_host_id, node_id, created_at
		 FROM managed_database WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var dbs []ManagedDatabase
	for rows.Next() {
		var d ManagedDatabase
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Name, &d.DBType, &d.Version, &d.Status, &d.ConnectionEncrypted,
			&d.StorageMode, &d.StorageHostID, &d.NodeID, &d.CreatedAt); err != nil {
			return nil, err
		}
		dbs = append(dbs, d)
	}
	return dbs, nil
}
