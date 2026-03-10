-- Baseline migration: creates all app-domain tables if they don't already exist.
-- These tables were originally managed by Prisma; Go now owns them via golang-migrate.
-- Auth tables (user, session, account, verification, org_role) remain under Prisma.

CREATE TABLE IF NOT EXISTS project (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    name TEXT NOT NULL,
    org_id TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_project_org ON project(org_id);

CREATE TABLE IF NOT EXISTS app (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    deploy_type TEXT NOT NULL DEFAULT 'image',
    image TEXT NOT NULL DEFAULT '',
    git_repo TEXT NOT NULL DEFAULT '',
    git_branch TEXT NOT NULL DEFAULT 'main',
    dockerfile_path TEXT NOT NULL DEFAULT 'Dockerfile',
    domain TEXT NOT NULL DEFAULT '',
    port INT NOT NULL DEFAULT 3000,
    replicas INT NOT NULL DEFAULT 1,
    env_encrypted BYTEA,
    status TEXT NOT NULL DEFAULT 'pending',
    cpu_limit DOUBLE PRECISION NOT NULL DEFAULT 0,
    memory_limit BIGINT NOT NULL DEFAULT 0,
    health_check_path TEXT NOT NULL DEFAULT '',
    health_check_interval INT NOT NULL DEFAULT 30,
    homepage_labels JSONB NOT NULL DEFAULT '{}',
    extra_labels JSONB NOT NULL DEFAULT '{}',
    placement_constraints JSONB NOT NULL DEFAULT '[]',
    placement_preferences JSONB NOT NULL DEFAULT '[]',
    update_strategy TEXT NOT NULL DEFAULT 'rolling',
    update_parallelism INT NOT NULL DEFAULT 1,
    update_delay TEXT NOT NULL DEFAULT '5s',
    update_failure_action TEXT NOT NULL DEFAULT 'rollback',
    update_order TEXT NOT NULL DEFAULT 'stop-first',
    build_cache_enabled BOOLEAN NOT NULL DEFAULT true,
    auto_deploy_branch TEXT NOT NULL DEFAULT 'main',
    preview_environments BOOLEAN NOT NULL DEFAULT false,
    template_name TEXT NOT NULL DEFAULT '',
    template_version TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_app_project ON app(project_id);
CREATE INDEX IF NOT EXISTS idx_app_git_repo ON app(deploy_type, git_repo);

CREATE TABLE IF NOT EXISTS deployment (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    app_id TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'building',
    commit_sha TEXT NOT NULL DEFAULT '',
    image_digest TEXT NOT NULL DEFAULT '',
    logs TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_deployment_app ON deployment(app_id);

CREATE TABLE IF NOT EXISTS managed_database (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    db_type TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT 'latest',
    status TEXT NOT NULL DEFAULT 'pending',
    connection_encrypted BYTEA,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_managed_database_project ON managed_database(project_id);

CREATE TABLE IF NOT EXISTS domain (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    app_id TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    domain TEXT NOT NULL UNIQUE,
    ssl_status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS git_source (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    token_encrypted BYTEA,
    webhook_secret_encrypted BYTEA,
    provider_name TEXT NOT NULL DEFAULT '',
    webhook_ids JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_git_source_org ON git_source(org_id);

CREATE TABLE IF NOT EXISTS backup_config (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    resource_id TEXT NOT NULL,
    schedule TEXT NOT NULL DEFAULT '0 3 * * *',
    s3_bucket TEXT NOT NULL DEFAULT '',
    s3_prefix TEXT NOT NULL DEFAULT '',
    backup_type TEXT NOT NULL DEFAULT 'database',
    volume_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_backup_config_resource ON backup_config(resource_id);

CREATE TABLE IF NOT EXISTS backup_run (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    config_id TEXT NOT NULL REFERENCES backup_config(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'running',
    size BIGINT NOT NULL DEFAULT 0,
    target_path TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS audit_log (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    action TEXT NOT NULL,
    resource TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    details TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_log_org_time ON audit_log(org_id, created_at DESC);

CREATE TABLE IF NOT EXISTS notification_channel (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notification_event (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    channel_id TEXT NOT NULL REFERENCES notification_channel(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'sent',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notification_event_channel ON notification_event(channel_id, created_at DESC);

CREATE TABLE IF NOT EXISTS secret (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    docker_secret_id TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_secret_project ON secret(project_id);

CREATE TABLE IF NOT EXISTS volume (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    driver TEXT NOT NULL DEFAULT 'local',
    driver_opts JSONB NOT NULL DEFAULT '{}',
    labels JSONB NOT NULL DEFAULT '{}',
    mount_type TEXT NOT NULL DEFAULT 'volume',
    remote_host TEXT NOT NULL DEFAULT '',
    remote_path TEXT NOT NULL DEFAULT '',
    mount_options TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL DEFAULT 'local',
    status TEXT NOT NULL DEFAULT 'pending',
    storage_host_id TEXT,
    local_path TEXT NOT NULL DEFAULT '',
    ceph_pool TEXT NOT NULL DEFAULT '',
    ceph_image TEXT NOT NULL DEFAULT '',
    ceph_fs_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_volume_project ON volume(project_id);

CREATE TABLE IF NOT EXISTS app_secret (
    app_id TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    secret_id TEXT NOT NULL REFERENCES secret(id) ON DELETE CASCADE,
    target TEXT NOT NULL DEFAULT '',
    uid TEXT NOT NULL DEFAULT '0',
    gid TEXT NOT NULL DEFAULT '0',
    mode INT NOT NULL DEFAULT 292,
    PRIMARY KEY (app_id, secret_id)
);

CREATE TABLE IF NOT EXISTS app_volume (
    app_id TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    volume_id TEXT NOT NULL REFERENCES volume(id) ON DELETE CASCADE,
    container_path TEXT NOT NULL,
    read_only BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (app_id, volume_id)
);

CREATE TABLE IF NOT EXISTS proxy_route (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    domain TEXT NOT NULL,
    target_service TEXT NOT NULL,
    target_port INT NOT NULL DEFAULT 80,
    protocol TEXT NOT NULL DEFAULT 'http',
    upstream_port INT,
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
    auto_renew BOOLEAN NOT NULL DEFAULT false,
    dns_provider_id TEXT,
    last_renewed_at TIMESTAMPTZ,
    renewal_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS stack (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    project_id TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    compose_content TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS alert_threshold (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    org_id TEXT NOT NULL,
    metric TEXT NOT NULL,
    operator TEXT NOT NULL DEFAULT '>',
    value DOUBLE PRECISION NOT NULL,
    cooldown_minutes INT NOT NULL DEFAULT 5,
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_fired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

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

CREATE TABLE IF NOT EXISTS node_metrics_snapshot (
    id BIGSERIAL PRIMARY KEY,
    node_id TEXT NOT NULL,
    metrics JSONB NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_node_metrics_node_time ON node_metrics_snapshot(node_id, collected_at DESC);

CREATE TABLE IF NOT EXISTS dns_provider (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    org_id TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config_encrypted BYTEA NOT NULL,
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS dns_record (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    provider_id TEXT NOT NULL REFERENCES dns_provider(id) ON DELETE CASCADE,
    app_id TEXT REFERENCES app(id) ON DELETE SET NULL,
    domain TEXT NOT NULL,
    record_type TEXT NOT NULL DEFAULT 'A',
    value TEXT NOT NULL,
    proxied BOOLEAN NOT NULL DEFAULT false,
    managed BOOLEAN NOT NULL DEFAULT true,
    external_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS preview_deployment (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    app_id TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    branch TEXT NOT NULL,
    pr_number INT,
    domain TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    service_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS service_link (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    source_app_id TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    target_app_id TEXT REFERENCES app(id) ON DELETE SET NULL,
    target_database_id TEXT REFERENCES managed_database(id) ON DELETE SET NULL,
    env_prefix TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS maintenance_task (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    org_id TEXT NOT NULL,
    type TEXT NOT NULL,
    schedule TEXT NOT NULL DEFAULT '0 3 * * 0',
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_run_at TIMESTAMPTZ,
    last_status TEXT NOT NULL DEFAULT '',
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS maintenance_run (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    task_id TEXT NOT NULL REFERENCES maintenance_task(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'running',
    details TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS app_env_var (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    app_id TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value_encrypted BYTEA NOT NULL,
    is_secret BOOLEAN NOT NULL DEFAULT false,
    source TEXT NOT NULL DEFAULT 'user',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(app_id, key)
);

CREATE TABLE IF NOT EXISTS log_entry (
    id BIGSERIAL PRIMARY KEY,
    app_id TEXT NOT NULL,
    service_name TEXT NOT NULL DEFAULT '',
    node_id TEXT NOT NULL DEFAULT '',
    stream TEXT NOT NULL DEFAULT 'stdout',
    message TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'info',
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_log_entry_app_ts ON log_entry(app_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_log_entry_level ON log_entry(level);

CREATE TABLE IF NOT EXISTS log_forward_config (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'webhook',
    config_encrypted BYTEA,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS template_source (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id TEXT NOT NULL,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'git',
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS custom_template (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id TEXT NOT NULL,
    source_id TEXT REFERENCES template_source(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT 'custom',
    icon TEXT NOT NULL DEFAULT '',
    image TEXT NOT NULL DEFAULT '',
    version TEXT NOT NULL DEFAULT '1.0.0',
    ports TEXT NOT NULL DEFAULT '[]',
    env TEXT NOT NULL DEFAULT '{}',
    volumes TEXT NOT NULL DEFAULT '[]',
    domain TEXT NOT NULL DEFAULT '',
    replicas INT NOT NULL DEFAULT 1,
    is_stack BOOLEAN NOT NULL DEFAULT false,
    compose_content TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

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
    storage_host_id TEXT,
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
CREATE INDEX IF NOT EXISTS idx_ceph_osd_cluster ON ceph_osd(cluster_id);

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
CREATE INDEX IF NOT EXISTS idx_ceph_pool_cluster ON ceph_pool(cluster_id);
