-- Resource quotas
CREATE TABLE IF NOT EXISTS resource_quota (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    cpu_limit DOUBLE PRECISION NOT NULL DEFAULT 0,
    memory_limit BIGINT NOT NULL DEFAULT 0,
    storage_limit BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id)
);

-- Docker configs
CREATE TABLE IF NOT EXISTS docker_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    org_id TEXT NOT NULL DEFAULT 'default',
    name TEXT NOT NULL,
    docker_config_id TEXT NOT NULL DEFAULT '',
    data TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS app_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    config_id UUID NOT NULL REFERENCES docker_config(id) ON DELETE CASCADE,
    target_path TEXT NOT NULL DEFAULT '/',
    uid TEXT NOT NULL DEFAULT '0',
    gid TEXT NOT NULL DEFAULT '0',
    mode INT NOT NULL DEFAULT 292,
    UNIQUE(app_id, config_id)
);

-- Scheduled jobs
CREATE TABLE IF NOT EXISTS scheduled_job (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    org_id TEXT NOT NULL DEFAULT 'default',
    name TEXT NOT NULL,
    image TEXT NOT NULL,
    command TEXT NOT NULL DEFAULT '',
    schedule TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    env JSONB NOT NULL DEFAULT '{}',
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS job_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES scheduled_job(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    exit_code INT,
    logs TEXT NOT NULL DEFAULT '',
    container_id TEXT NOT NULL DEFAULT ''
);

-- Vulnerability scanning
CREATE TABLE IF NOT EXISTS vulnerability_scan (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id UUID NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    image TEXT NOT NULL,
    scan_status TEXT NOT NULL DEFAULT 'pending',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    critical_count INT NOT NULL DEFAULT 0,
    high_count INT NOT NULL DEFAULT 0,
    medium_count INT NOT NULL DEFAULT 0,
    low_count INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS vulnerability (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scan_id UUID NOT NULL REFERENCES vulnerability_scan(id) ON DELETE CASCADE,
    vuln_id TEXT NOT NULL,
    pkg_name TEXT NOT NULL,
    installed_version TEXT NOT NULL DEFAULT '',
    fixed_version TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'unknown',
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT ''
);

-- Node config (power management)
CREATE TABLE IF NOT EXISTS node_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id TEXT NOT NULL UNIQUE,
    hostname TEXT NOT NULL DEFAULT '',
    mac_address TEXT NOT NULL DEFAULT '',
    bmc_address TEXT NOT NULL DEFAULT '',
    bmc_username TEXT NOT NULL DEFAULT '',
    bmc_password_encrypted TEXT NOT NULL DEFAULT '',
    wol_enabled BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- UPS monitoring
CREATE TABLE IF NOT EXISTS ups_device (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT NOT NULL DEFAULT 'default',
    name TEXT NOT NULL,
    nut_host TEXT NOT NULL,
    nut_port INT NOT NULL DEFAULT 3493,
    ups_name TEXT NOT NULL DEFAULT 'ups',
    poll_interval_seconds INT NOT NULL DEFAULT 30,
    shutdown_threshold INT NOT NULL DEFAULT 10,
    shutdown_nodes JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ups_status_snapshot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES ups_device(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'unknown',
    battery_charge DOUBLE PRECISION NOT NULL DEFAULT 0,
    battery_runtime INT NOT NULL DEFAULT 0,
    input_voltage DOUBLE PRECISION NOT NULL DEFAULT 0,
    output_voltage DOUBLE PRECISION NOT NULL DEFAULT 0,
    load_percent DOUBLE PRECISION NOT NULL DEFAULT 0,
    temperature DOUBLE PRECISION NOT NULL DEFAULT 0,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- API tokens
CREATE TABLE IF NOT EXISTS api_token (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT NOT NULL DEFAULT 'default',
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    scopes JSONB NOT NULL DEFAULT '["read"]',
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Webhooks
CREATE TABLE IF NOT EXISTS webhook_endpoint (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT NOT NULL DEFAULT 'default',
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    secret TEXT NOT NULL DEFAULT '',
    events JSONB NOT NULL DEFAULT '[]',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id UUID NOT NULL REFERENCES webhook_endpoint(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    response_status INT NOT NULL DEFAULT 0,
    response_body TEXT NOT NULL DEFAULT '',
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- VPN
CREATE TABLE IF NOT EXISTS vpn_server (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT NOT NULL DEFAULT 'default',
    name TEXT NOT NULL,
    node_id TEXT NOT NULL DEFAULT '',
    listen_port INT NOT NULL DEFAULT 51820,
    address_range TEXT NOT NULL DEFAULT '10.99.0.0/24',
    dns TEXT NOT NULL DEFAULT '1.1.1.1',
    private_key_encrypted TEXT NOT NULL DEFAULT '',
    public_key TEXT NOT NULL DEFAULT '',
    endpoint TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS vpn_peer (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id UUID NOT NULL REFERENCES vpn_server(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    public_key TEXT NOT NULL DEFAULT '',
    preshared_key_encrypted TEXT NOT NULL DEFAULT '',
    allowed_ips TEXT NOT NULL DEFAULT '0.0.0.0/0',
    assigned_ip TEXT NOT NULL DEFAULT '',
    last_handshake TIMESTAMPTZ,
    transfer_rx BIGINT NOT NULL DEFAULT 0,
    transfer_tx BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Dashboard layout
CREATE TABLE IF NOT EXISTS dashboard_layout (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id TEXT NOT NULL,
    org_id TEXT NOT NULL DEFAULT 'default',
    layout JSONB NOT NULL DEFAULT '{"widgets":[]}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, org_id)
);

-- Multi-cluster
CREATE TABLE IF NOT EXISTS cluster (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id TEXT NOT NULL DEFAULT 'default',
    name TEXT NOT NULL,
    api_endpoint TEXT NOT NULL DEFAULT '',
    auth_token_encrypted TEXT NOT NULL DEFAULT '',
    tls_ca TEXT NOT NULL DEFAULT '',
    is_local BOOLEAN NOT NULL DEFAULT false,
    status TEXT NOT NULL DEFAULT 'disconnected',
    node_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Template ratings
CREATE TABLE IF NOT EXISTS template_rating (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_name TEXT NOT NULL,
    user_id TEXT NOT NULL,
    rating INT NOT NULL CHECK (rating >= 1 AND rating <= 5),
    review_text TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(template_name, user_id)
);

CREATE TABLE IF NOT EXISTS template_install_count (
    template_name TEXT PRIMARY KEY,
    install_count INT NOT NULL DEFAULT 0
);

-- Add spec snapshot to deployments
ALTER TABLE deployment ADD COLUMN IF NOT EXISTS service_spec_snapshot JSONB;

-- Add auto_scan to apps
ALTER TABLE app ADD COLUMN IF NOT EXISTS auto_scan BOOLEAN NOT NULL DEFAULT false;

-- Add DDNS columns to dns_provider
ALTER TABLE dns_provider ADD COLUMN IF NOT EXISTS ddns_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE dns_provider ADD COLUMN IF NOT EXISTS ddns_interval_minutes INT NOT NULL DEFAULT 5;
ALTER TABLE dns_provider ADD COLUMN IF NOT EXISTS ddns_last_ip TEXT NOT NULL DEFAULT '';
ALTER TABLE dns_provider ADD COLUMN IF NOT EXISTS ddns_last_update TIMESTAMPTZ;
