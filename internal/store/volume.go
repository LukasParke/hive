package store

import (
	"context"
)

func (s *Store) CreateVolume(ctx context.Context, vol *Volume) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO volume (project_id, name, driver, driver_opts, labels, mount_type, remote_host, remote_path, mount_options, scope, status, storage_host_id, local_path, ceph_pool, ceph_image, ceph_fs_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULLIF($12,''), $13, $14, $15, $16) RETURNING id, created_at`,
		vol.ProjectID, vol.Name, vol.Driver, vol.DriverOpts, vol.Labels, vol.MountType, vol.RemoteHost, vol.RemotePath, vol.MountOptions, vol.Scope, vol.Status,
		vol.StorageHostID, vol.LocalPath, vol.CephPool, vol.CephImage, vol.CephFSName,
	).Scan(&vol.ID, &vol.CreatedAt)
}

func (s *Store) ListVolumes(ctx context.Context, projectID string) ([]Volume, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, driver, driver_opts, labels, mount_type, remote_host, remote_path, mount_options, scope, status,
		 COALESCE(storage_host_id,''), local_path, ceph_pool, ceph_image, ceph_fs_name, created_at
		 FROM volume WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var vols []Volume
	for rows.Next() {
		var v Volume
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Driver, &v.DriverOpts, &v.Labels, &v.MountType, &v.RemoteHost, &v.RemotePath, &v.MountOptions, &v.Scope, &v.Status,
			&v.StorageHostID, &v.LocalPath, &v.CephPool, &v.CephImage, &v.CephFSName, &v.CreatedAt); err != nil {
			return nil, err
		}
		vols = append(vols, v)
	}
	return vols, nil
}

func (s *Store) GetVolume(ctx context.Context, id string) (*Volume, error) {
	v := &Volume{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, driver, driver_opts, labels, mount_type, remote_host, remote_path, mount_options, scope, status,
		 COALESCE(storage_host_id,''), local_path, ceph_pool, ceph_image, ceph_fs_name, created_at
		 FROM volume WHERE id = $1`, id,
	).Scan(&v.ID, &v.ProjectID, &v.Name, &v.Driver, &v.DriverOpts, &v.Labels, &v.MountType, &v.RemoteHost, &v.RemotePath, &v.MountOptions, &v.Scope, &v.Status,
		&v.StorageHostID, &v.LocalPath, &v.CephPool, &v.CephImage, &v.CephFSName, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Store) UpdateVolumeStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE volume SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (s *Store) DeleteVolume(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM volume WHERE id = $1`, id)
	return err
}

func (s *Store) AttachVolume(ctx context.Context, av *AppVolume) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_volume (app_id, volume_id, container_path, read_only) VALUES ($1, $2, $3, $4) ON CONFLICT (app_id, volume_id) DO UPDATE SET container_path=$3, read_only=$4`,
		av.AppID, av.VolumeID, av.ContainerPath, av.ReadOnly,
	)
	return err
}

func (s *Store) DetachVolume(ctx context.Context, appID, volumeID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_volume WHERE app_id = $1 AND volume_id = $2`, appID, volumeID)
	return err
}

func (s *Store) ListAppVolumes(ctx context.Context, appID string) ([]AppVolume, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT app_id, volume_id, container_path, read_only FROM app_volume WHERE app_id = $1`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []AppVolume
	for rows.Next() {
		var av AppVolume
		if err := rows.Scan(&av.AppID, &av.VolumeID, &av.ContainerPath, &av.ReadOnly); err != nil {
			return nil, err
		}
		result = append(result, av)
	}
	return result, nil
}
