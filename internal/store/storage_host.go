package store

import (
	"context"
)

func (s *Store) CreateStorageHost(ctx context.Context, sh *StorageHost) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO storage_host (name, node_id, address, type, default_export_path, default_mount_type, mount_options_default, credentials_encrypted, capabilities, node_label, status)
		 VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id, created_at, updated_at`,
		sh.Name, sh.NodeID, sh.Address, sh.Type, sh.DefaultExportPath, sh.DefaultMountType, sh.MountOptionsDefault,
		sh.CredentialsEncrypted, sh.Capabilities, sh.NodeLabel, sh.Status,
	).Scan(&sh.ID, &sh.CreatedAt, &sh.UpdatedAt)
}

func (s *Store) ListStorageHosts(ctx context.Context) ([]StorageHost, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(node_id,''), address, type, default_export_path, default_mount_type, mount_options_default,
		 credentials_encrypted, capabilities, node_label, status, created_at, updated_at
		 FROM storage_host ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var hosts []StorageHost
	for rows.Next() {
		var h StorageHost
		if err := rows.Scan(&h.ID, &h.Name, &h.NodeID, &h.Address, &h.Type, &h.DefaultExportPath, &h.DefaultMountType, &h.MountOptionsDefault,
			&h.CredentialsEncrypted, &h.Capabilities, &h.NodeLabel, &h.Status, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func (s *Store) GetStorageHost(ctx context.Context, id string) (*StorageHost, error) {
	h := &StorageHost{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(node_id,''), address, type, default_export_path, default_mount_type, mount_options_default,
		 credentials_encrypted, capabilities, node_label, status, created_at, updated_at
		 FROM storage_host WHERE id = $1`, id,
	).Scan(&h.ID, &h.Name, &h.NodeID, &h.Address, &h.Type, &h.DefaultExportPath, &h.DefaultMountType, &h.MountOptionsDefault,
		&h.CredentialsEncrypted, &h.Capabilities, &h.NodeLabel, &h.Status, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return h, nil
}

func (s *Store) UpdateStorageHost(ctx context.Context, sh *StorageHost) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE storage_host SET name=$1, node_id=NULLIF($2,''), address=$3, type=$4, default_export_path=$5, default_mount_type=$6,
		 mount_options_default=$7, credentials_encrypted=$8, capabilities=$9, node_label=$10, status=$11, updated_at=now()
		 WHERE id=$12`,
		sh.Name, sh.NodeID, sh.Address, sh.Type, sh.DefaultExportPath, sh.DefaultMountType, sh.MountOptionsDefault,
		sh.CredentialsEncrypted, sh.Capabilities, sh.NodeLabel, sh.Status, sh.ID,
	)
	return err
}

func (s *Store) DeleteStorageHost(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM storage_host WHERE id = $1`, id)
	return err
}
