-- Feature 1: DNS + SSL Management
CREATE TABLE IF NOT EXISTS dns_provider (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    org_id TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL, -- 'cloudflare', 'route53', 'digitalocean', 'manual'
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

ALTER TABLE custom_certificate ADD COLUMN IF NOT EXISTS auto_renew BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE custom_certificate ADD COLUMN IF NOT EXISTS dns_provider_id TEXT REFERENCES dns_provider(id);
ALTER TABLE custom_certificate ADD COLUMN IF NOT EXISTS last_renewed_at TIMESTAMPTZ;
ALTER TABLE custom_certificate ADD COLUMN IF NOT EXISTS renewal_error TEXT NOT NULL DEFAULT '';

-- Feature 2: CI/CD Pipeline
ALTER TABLE app ADD COLUMN IF NOT EXISTS build_cache_enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE app ADD COLUMN IF NOT EXISTS auto_deploy_branch TEXT NOT NULL DEFAULT 'main';
ALTER TABLE app ADD COLUMN IF NOT EXISTS preview_environments BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE git_source ADD COLUMN IF NOT EXISTS webhook_secret_encrypted BYTEA;
ALTER TABLE git_source ADD COLUMN IF NOT EXISTS provider_name TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS preview_deployment (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    app_id TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    branch TEXT NOT NULL,
    pr_number INTEGER,
    domain TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    service_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Feature 3: Service Networking
CREATE TABLE IF NOT EXISTS service_link (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    source_app_id TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    target_app_id TEXT REFERENCES app(id) ON DELETE SET NULL,
    target_database_id TEXT REFERENCES managed_database(id) ON DELETE SET NULL,
    env_prefix TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_service_link_target CHECK (target_app_id IS NOT NULL OR target_database_id IS NOT NULL)
);

ALTER TABLE proxy_route ADD COLUMN IF NOT EXISTS protocol TEXT NOT NULL DEFAULT 'http';
ALTER TABLE proxy_route ADD COLUMN IF NOT EXISTS upstream_port INTEGER;

-- Feature 4: RBAC + Audit
CREATE TABLE IF NOT EXISTS org_role (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    org_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'viewer',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_org_role_user ON org_role(user_id);
CREATE INDEX IF NOT EXISTS idx_org_role_org ON org_role(org_id);

-- Feature 5: Automated Maintenance
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
