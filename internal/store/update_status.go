package store

import (
	"context"
	"encoding/json"

	"github.com/lib/pq"
)

func (s *Store) UpsertNodeUpdateStatus(ctx context.Context, n *NodeUpdateStatus) error {
	if n.PendingPackages == nil {
		n.PendingPackages = json.RawMessage("[]")
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO node_update_status (node_id, hostname, os_info, kernel_version, package_manager,
			pending_count, security_count, reboot_required, pending_packages, last_checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (node_id) DO UPDATE SET
			hostname = EXCLUDED.hostname,
			os_info = EXCLUDED.os_info,
			kernel_version = EXCLUDED.kernel_version,
			package_manager = EXCLUDED.package_manager,
			pending_count = EXCLUDED.pending_count,
			security_count = EXCLUDED.security_count,
			reboot_required = EXCLUDED.reboot_required,
			pending_packages = EXCLUDED.pending_packages,
			last_checked_at = NOW()
		RETURNING id, last_checked_at`,
		n.NodeID, n.Hostname, n.OSInfo, n.KernelVersion, n.PackageManager,
		n.PendingCount, n.SecurityCount, n.RebootRequired, n.PendingPackages,
	).Scan(&n.ID, &n.LastCheckedAt)
}

func (s *Store) GetNodeUpdateStatus(ctx context.Context, nodeID string) (*NodeUpdateStatus, error) {
	n := &NodeUpdateStatus{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, node_id, hostname, os_info, kernel_version, package_manager,
			pending_count, security_count, reboot_required, pending_packages, last_checked_at
		FROM node_update_status WHERE node_id = $1`, nodeID,
	).Scan(&n.ID, &n.NodeID, &n.Hostname, &n.OSInfo, &n.KernelVersion, &n.PackageManager,
		&n.PendingCount, &n.SecurityCount, &n.RebootRequired, &n.PendingPackages, &n.LastCheckedAt)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Store) ListNodeUpdateStatuses(ctx context.Context) ([]NodeUpdateStatus, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, node_id, hostname, os_info, kernel_version, package_manager,
			pending_count, security_count, reboot_required, pending_packages, last_checked_at
		FROM node_update_status ORDER BY hostname`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var statuses []NodeUpdateStatus
	for rows.Next() {
		var n NodeUpdateStatus
		if err := rows.Scan(&n.ID, &n.NodeID, &n.Hostname, &n.OSInfo, &n.KernelVersion, &n.PackageManager,
			&n.PendingCount, &n.SecurityCount, &n.RebootRequired, &n.PendingPackages, &n.LastCheckedAt); err != nil {
			return nil, err
		}
		statuses = append(statuses, n)
	}
	return statuses, nil
}

func (s *Store) DeleteNodeUpdateStatus(ctx context.Context, nodeID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM node_update_status WHERE node_id = $1`, nodeID)
	return err
}

func (s *Store) UpsertServiceUpdateStatus(ctx context.Context, sus *ServiceUpdateStatus) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO service_update_status (app_id, stack_id, service_name, current_image,
			current_digest, latest_digest, latest_version, update_available, last_checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (service_name) DO UPDATE SET
			app_id = EXCLUDED.app_id,
			stack_id = EXCLUDED.stack_id,
			current_image = EXCLUDED.current_image,
			current_digest = EXCLUDED.current_digest,
			latest_digest = EXCLUDED.latest_digest,
			latest_version = EXCLUDED.latest_version,
			update_available = EXCLUDED.update_available,
			last_checked_at = NOW()
		RETURNING id, last_checked_at`,
		sus.AppID, sus.StackID, sus.ServiceName, sus.CurrentImage,
		sus.CurrentDigest, sus.LatestDigest, sus.LatestVersion, sus.UpdateAvailable,
	).Scan(&sus.ID, &sus.LastCheckedAt)
}

func (s *Store) ListServiceUpdateStatuses(ctx context.Context) ([]ServiceUpdateStatus, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(app_id,''), COALESCE(stack_id,''), service_name, current_image,
			current_digest, latest_digest, latest_version, update_available, last_checked_at
		FROM service_update_status ORDER BY service_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var statuses []ServiceUpdateStatus
	for rows.Next() {
		var sus ServiceUpdateStatus
		if err := rows.Scan(&sus.ID, &sus.AppID, &sus.StackID, &sus.ServiceName, &sus.CurrentImage,
			&sus.CurrentDigest, &sus.LatestDigest, &sus.LatestVersion, &sus.UpdateAvailable, &sus.LastCheckedAt); err != nil {
			return nil, err
		}
		statuses = append(statuses, sus)
	}
	return statuses, nil
}

func (s *Store) ListServiceUpdateStatusesWithUpdates(ctx context.Context) ([]ServiceUpdateStatus, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(app_id,''), COALESCE(stack_id,''), service_name, current_image,
			current_digest, latest_digest, latest_version, update_available, last_checked_at
		FROM service_update_status WHERE update_available = true ORDER BY service_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var statuses []ServiceUpdateStatus
	for rows.Next() {
		var sus ServiceUpdateStatus
		if err := rows.Scan(&sus.ID, &sus.AppID, &sus.StackID, &sus.ServiceName, &sus.CurrentImage,
			&sus.CurrentDigest, &sus.LatestDigest, &sus.LatestVersion, &sus.UpdateAvailable, &sus.LastCheckedAt); err != nil {
			return nil, err
		}
		statuses = append(statuses, sus)
	}
	return statuses, nil
}

func (s *Store) GetServiceUpdateStatus(ctx context.Context, serviceName string) (*ServiceUpdateStatus, error) {
	sus := &ServiceUpdateStatus{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(app_id,''), COALESCE(stack_id,''), service_name, current_image,
			current_digest, latest_digest, latest_version, update_available, last_checked_at
		FROM service_update_status WHERE service_name = $1`, serviceName,
	).Scan(&sus.ID, &sus.AppID, &sus.StackID, &sus.ServiceName, &sus.CurrentImage,
		&sus.CurrentDigest, &sus.LatestDigest, &sus.LatestVersion, &sus.UpdateAvailable, &sus.LastCheckedAt)
	if err != nil {
		return nil, err
	}
	return sus, nil
}

func (s *Store) DeleteServiceUpdateStatus(ctx context.Context, serviceName string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM service_update_status WHERE service_name = $1`, serviceName)
	return err
}

func (s *Store) DeleteInfraServiceUpdateStatuses(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM service_update_status WHERE service_name = ANY($1) OR service_name LIKE 'hive-pg-%'`,
		pq.Array(names),
	)
	return err
}
