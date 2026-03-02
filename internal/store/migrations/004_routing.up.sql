-- Group 1: Proxy routing + certificates
CREATE TABLE IF NOT EXISTS proxy_route (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    domain TEXT NOT NULL,
    target_service TEXT NOT NULL,
    target_port INTEGER NOT NULL DEFAULT 80,
    ssl_mode TEXT NOT NULL DEFAULT 'letsencrypt',
    custom_cert_id TEXT,
    middleware_config JSONB NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS custom_certificate (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    domain TEXT NOT NULL,
    cert_pem TEXT NOT NULL,
    key_pem_encrypted BYTEA NOT NULL,
    is_wildcard BOOLEAN NOT NULL DEFAULT false,
    provider TEXT NOT NULL DEFAULT 'manual',
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Group 2: Backup enhancements
ALTER TABLE backup_config ADD COLUMN IF NOT EXISTS backup_type TEXT NOT NULL DEFAULT 'database';
ALTER TABLE backup_config ADD COLUMN IF NOT EXISTS volume_id TEXT;

-- Group 4: App label support
ALTER TABLE app ADD COLUMN IF NOT EXISTS homepage_labels JSONB NOT NULL DEFAULT '{}';
ALTER TABLE app ADD COLUMN IF NOT EXISTS extra_labels JSONB NOT NULL DEFAULT '{}';

-- Group 5: Stacks + placement
CREATE TABLE IF NOT EXISTS stack (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    compose_content TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE app ADD COLUMN IF NOT EXISTS placement_constraints JSONB NOT NULL DEFAULT '[]';
ALTER TABLE app ADD COLUMN IF NOT EXISTS placement_preferences JSONB NOT NULL DEFAULT '[]';

-- Group 7/8: Rolling update fields
ALTER TABLE app ADD COLUMN IF NOT EXISTS update_strategy TEXT NOT NULL DEFAULT 'rolling';
ALTER TABLE app ADD COLUMN IF NOT EXISTS update_parallelism INTEGER NOT NULL DEFAULT 1;
ALTER TABLE app ADD COLUMN IF NOT EXISTS update_delay TEXT NOT NULL DEFAULT '5s';
ALTER TABLE app ADD COLUMN IF NOT EXISTS update_failure_action TEXT NOT NULL DEFAULT 'rollback';
ALTER TABLE app ADD COLUMN IF NOT EXISTS update_order TEXT NOT NULL DEFAULT 'stop-first';

-- Notification channel table update (add name)
ALTER TABLE notification_channel ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';

-- Resource alert thresholds
CREATE TABLE IF NOT EXISTS alert_threshold (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    org_id TEXT NOT NULL,
    metric TEXT NOT NULL,
    operator TEXT NOT NULL DEFAULT '>',
    value DOUBLE PRECISION NOT NULL,
    cooldown_minutes INTEGER NOT NULL DEFAULT 5,
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_fired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Group 9: Storage hosts
CREATE TABLE IF NOT EXISTS storage_host (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    name TEXT NOT NULL UNIQUE,
    node_id TEXT,
    address TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'nas',
    default_export_path TEXT NOT NULL DEFAULT '',
    default_mount_type TEXT NOT NULL DEFAULT 'nfs',
    mount_options_default TEXT NOT NULL DEFAULT '',
    credentials_encrypted BYTEA,
    capabilities JSONB NOT NULL DEFAULT '{}',
    node_label TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Volume enhancements for storage host integration
ALTER TABLE volume ADD COLUMN IF NOT EXISTS storage_host_id TEXT REFERENCES storage_host(id);
ALTER TABLE volume ADD COLUMN IF NOT EXISTS local_path TEXT NOT NULL DEFAULT '';
ALTER TABLE volume ADD COLUMN IF NOT EXISTS ceph_pool TEXT NOT NULL DEFAULT '';
ALTER TABLE volume ADD COLUMN IF NOT EXISTS ceph_image TEXT NOT NULL DEFAULT '';
ALTER TABLE volume ADD COLUMN IF NOT EXISTS ceph_fs_name TEXT NOT NULL DEFAULT '';

-- Group 10: Metrics snapshots
CREATE TABLE IF NOT EXISTS node_metrics_snapshot (
    id BIGSERIAL PRIMARY KEY,
    node_id TEXT NOT NULL,
    metrics JSONB NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_node_metrics_node_time
    ON node_metrics_snapshot (node_id, collected_at DESC);
