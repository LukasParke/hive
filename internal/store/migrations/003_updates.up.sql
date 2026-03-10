-- Node OS update status (cached from agent reports)
CREATE TABLE IF NOT EXISTS node_update_status (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    node_id TEXT NOT NULL,
    hostname TEXT NOT NULL,
    os_info TEXT NOT NULL DEFAULT '',
    kernel_version TEXT NOT NULL DEFAULT '',
    package_manager TEXT NOT NULL DEFAULT '',
    pending_count INT NOT NULL DEFAULT 0,
    security_count INT NOT NULL DEFAULT 0,
    reboot_required BOOLEAN NOT NULL DEFAULT false,
    pending_packages JSONB NOT NULL DEFAULT '[]',
    last_checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(node_id)
);

-- Service image update status
CREATE TABLE IF NOT EXISTS service_update_status (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    app_id TEXT,
    stack_id TEXT,
    service_name TEXT NOT NULL,
    current_image TEXT NOT NULL,
    current_digest TEXT NOT NULL DEFAULT '',
    latest_digest TEXT NOT NULL DEFAULT '',
    latest_version TEXT NOT NULL DEFAULT '',
    update_available BOOLEAN NOT NULL DEFAULT false,
    last_checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(service_name)
);

-- Update event log (audit trail for all updates performed)
CREATE TABLE IF NOT EXISTS update_event (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    event_type TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    target_name TEXT NOT NULL DEFAULT '',
    previous_version TEXT NOT NULL DEFAULT '',
    new_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    details TEXT NOT NULL DEFAULT '',
    triggered_by TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_update_event_target ON update_event(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_update_event_time ON update_event(started_at DESC);

-- Update policies (per-node, per-service, or global)
CREATE TABLE IF NOT EXISTS update_policy (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL DEFAULT '',
    auto_update BOOLEAN NOT NULL DEFAULT false,
    auto_restart BOOLEAN NOT NULL DEFAULT false,
    maintenance_window_start TEXT NOT NULL DEFAULT '',
    maintenance_window_end TEXT NOT NULL DEFAULT '',
    maintenance_window_days TEXT NOT NULL DEFAULT '',
    security_only BOOLEAN NOT NULL DEFAULT false,
    pre_update_backup BOOLEAN NOT NULL DEFAULT true,
    notify_on_update BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, target_type, target_id)
);
