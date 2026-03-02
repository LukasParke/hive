CREATE TABLE IF NOT EXISTS ceph_cluster (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    name TEXT NOT NULL UNIQUE,
    fsid TEXT UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending',
    bootstrap_node_id TEXT NOT NULL,
    mon_hosts TEXT[] NOT NULL DEFAULT '{}',
    public_network TEXT NOT NULL DEFAULT '',
    cluster_network TEXT NOT NULL DEFAULT '',
    ceph_conf_encrypted BYTEA,
    admin_keyring_encrypted BYTEA,
    replication_size INT NOT NULL DEFAULT 3,
    storage_host_id TEXT REFERENCES storage_host(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ceph_osd (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    cluster_id TEXT NOT NULL REFERENCES ceph_cluster(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL,
    hostname TEXT NOT NULL,
    osd_id INT,
    device_path TEXT NOT NULL,
    device_size BIGINT NOT NULL DEFAULT 0,
    device_type TEXT NOT NULL DEFAULT 'hdd',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ceph_pool (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    cluster_id TEXT NOT NULL REFERENCES ceph_cluster(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    pool_id INT,
    pg_num INT NOT NULL DEFAULT 32,
    size INT NOT NULL DEFAULT 3,
    type TEXT NOT NULL DEFAULT 'replicated',
    application TEXT NOT NULL DEFAULT 'rbd',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ceph_osd_cluster ON ceph_osd(cluster_id);
CREATE INDEX IF NOT EXISTS idx_ceph_pool_cluster ON ceph_pool(cluster_id);
