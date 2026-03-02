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
