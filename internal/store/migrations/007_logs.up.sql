CREATE TABLE IF NOT EXISTS log_entry (
    id BIGSERIAL PRIMARY KEY,
    app_id TEXT NOT NULL,
    service_name TEXT NOT NULL DEFAULT '',
    node_id TEXT NOT NULL DEFAULT '',
    stream TEXT NOT NULL DEFAULT 'stdout',
    message TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'info',
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT log_entry_app_fk FOREIGN KEY (app_id) REFERENCES app(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_log_entry_app_ts ON log_entry (app_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_log_entry_level ON log_entry (level);

CREATE TABLE IF NOT EXISTS log_forward_config (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'webhook',
    config_encrypted BYTEA,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
