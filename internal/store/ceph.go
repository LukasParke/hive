package store

import (
	"context"
	"strings"
)

func pqStringArray(arr []string) string {
	if len(arr) == 0 {
		return "{}"
	}
	elements := make([]string, len(arr))
	for i, v := range arr {
		elements[i] = `"` + v + `"`
	}
	result := elements[0]
	for _, e := range elements[1:] {
		result += "," + e
	}
	return "{" + result + "}"
}

func parsePqStringArray(data []byte) []string {
	raw := string(data)
	raw = strings.TrimPrefix(raw, "{")
	raw = strings.TrimSuffix(raw, "}")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, `"`)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func (s *Store) CreateCephCluster(ctx context.Context, c *CephCluster) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO ceph_cluster (name, fsid, status, bootstrap_node_id, mon_hosts, public_network, cluster_network,
		 ceph_conf_encrypted, admin_keyring_encrypted, replication_size, storage_host_id)
		 VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11,''))
		 RETURNING id, created_at, updated_at`,
		c.Name, c.FSID, c.Status, c.BootstrapNodeID, pqStringArray(c.MonHosts), c.PublicNetwork, c.ClusterNetwork,
		c.CephConfEncrypted, c.AdminKeyringEncrypted, c.ReplicationSize, c.StorageHostID,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

func (s *Store) GetCephCluster(ctx context.Context, id string) (*CephCluster, error) {
	c := &CephCluster{}
	var monHosts []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(fsid,''), status, bootstrap_node_id, mon_hosts, public_network, cluster_network,
		 ceph_conf_encrypted, admin_keyring_encrypted, replication_size, COALESCE(storage_host_id,''), created_at, updated_at
		 FROM ceph_cluster WHERE id = $1`, id,
	).Scan(&c.ID, &c.Name, &c.FSID, &c.Status, &c.BootstrapNodeID, &monHosts, &c.PublicNetwork, &c.ClusterNetwork,
		&c.CephConfEncrypted, &c.AdminKeyringEncrypted, &c.ReplicationSize, &c.StorageHostID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.MonHosts = parsePqStringArray(monHosts)
	return c, nil
}

func (s *Store) GetCephClusterByFSID(ctx context.Context, fsid string) (*CephCluster, error) {
	c := &CephCluster{}
	var monHosts []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(fsid,''), status, bootstrap_node_id, mon_hosts, public_network, cluster_network,
		 ceph_conf_encrypted, admin_keyring_encrypted, replication_size, COALESCE(storage_host_id,''), created_at, updated_at
		 FROM ceph_cluster WHERE fsid = $1`, fsid,
	).Scan(&c.ID, &c.Name, &c.FSID, &c.Status, &c.BootstrapNodeID, &monHosts, &c.PublicNetwork, &c.ClusterNetwork,
		&c.CephConfEncrypted, &c.AdminKeyringEncrypted, &c.ReplicationSize, &c.StorageHostID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.MonHosts = parsePqStringArray(monHosts)
	return c, nil
}

func (s *Store) ListCephClusters(ctx context.Context) ([]CephCluster, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(fsid,''), status, bootstrap_node_id, mon_hosts, public_network, cluster_network,
		 replication_size, COALESCE(storage_host_id,''), created_at, updated_at
		 FROM ceph_cluster ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var clusters []CephCluster
	for rows.Next() {
		var c CephCluster
		var monHosts []byte
		if err := rows.Scan(&c.ID, &c.Name, &c.FSID, &c.Status, &c.BootstrapNodeID, &monHosts, &c.PublicNetwork, &c.ClusterNetwork,
			&c.ReplicationSize, &c.StorageHostID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.MonHosts = parsePqStringArray(monHosts)
		clusters = append(clusters, c)
	}
	return clusters, nil
}

func (s *Store) UpdateCephCluster(ctx context.Context, c *CephCluster) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ceph_cluster SET name=$1, fsid=NULLIF($2,''), status=$3, mon_hosts=$4, public_network=$5, cluster_network=$6,
		 ceph_conf_encrypted=$7, admin_keyring_encrypted=$8, replication_size=$9, storage_host_id=NULLIF($10,''), updated_at=now()
		 WHERE id=$11`,
		c.Name, c.FSID, c.Status, pqStringArray(c.MonHosts), c.PublicNetwork, c.ClusterNetwork,
		c.CephConfEncrypted, c.AdminKeyringEncrypted, c.ReplicationSize, c.StorageHostID, c.ID,
	)
	return err
}

func (s *Store) UpdateCephClusterStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ceph_cluster SET status=$1, updated_at=now() WHERE id=$2`, status, id)
	return err
}

func (s *Store) DeleteCephCluster(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ceph_cluster WHERE id = $1`, id)
	return err
}

func (s *Store) CreateCephOSD(ctx context.Context, o *CephOSD) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO ceph_osd (cluster_id, node_id, hostname, osd_id, device_path, device_size, device_type, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		o.ClusterID, o.NodeID, o.Hostname, o.OsdID, o.DevicePath, o.DeviceSize, o.DeviceType, o.Status,
	).Scan(&o.ID, &o.CreatedAt)
}

func (s *Store) ListCephOSDs(ctx context.Context, clusterID string) ([]CephOSD, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, cluster_id, node_id, hostname, osd_id, device_path, device_size, device_type, status, created_at
		 FROM ceph_osd WHERE cluster_id = $1 ORDER BY created_at`, clusterID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var osds []CephOSD
	for rows.Next() {
		var o CephOSD
		if err := rows.Scan(&o.ID, &o.ClusterID, &o.NodeID, &o.Hostname, &o.OsdID, &o.DevicePath, &o.DeviceSize, &o.DeviceType, &o.Status, &o.CreatedAt); err != nil {
			return nil, err
		}
		osds = append(osds, o)
	}
	return osds, nil
}

func (s *Store) UpdateCephOSDStatus(ctx context.Context, id, status string, osdID *int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ceph_osd SET status=$1, osd_id=$2 WHERE id=$3`, status, osdID, id)
	return err
}

func (s *Store) DeleteCephOSD(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ceph_osd WHERE id = $1`, id)
	return err
}

func (s *Store) CreateCephPool(ctx context.Context, p *CephPool) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO ceph_pool (cluster_id, name, pool_id, pg_num, size, type, application)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		p.ClusterID, p.Name, p.PoolID, p.PGNum, p.Size, p.Type, p.Application,
	).Scan(&p.ID, &p.CreatedAt)
}

func (s *Store) ListCephPools(ctx context.Context, clusterID string) ([]CephPool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, cluster_id, name, pool_id, pg_num, size, type, application, created_at
		 FROM ceph_pool WHERE cluster_id = $1 ORDER BY created_at`, clusterID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var pools []CephPool
	for rows.Next() {
		var p CephPool
		if err := rows.Scan(&p.ID, &p.ClusterID, &p.Name, &p.PoolID, &p.PGNum, &p.Size, &p.Type, &p.Application, &p.CreatedAt); err != nil {
			return nil, err
		}
		pools = append(pools, p)
	}
	return pools, nil
}

func (s *Store) DeleteCephPool(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ceph_pool WHERE id = $1`, id)
	return err
}
