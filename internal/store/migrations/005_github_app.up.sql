CREATE TABLE IF NOT EXISTS github_app (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id TEXT NOT NULL,
    app_id INTEGER NOT NULL,
    app_slug TEXT NOT NULL DEFAULT '',
    pem_encrypted BYTEA,
    webhook_secret TEXT NOT NULL DEFAULT '',
    client_id TEXT NOT NULL DEFAULT '',
    client_secret_encrypted BYTEA,
    installation_id BIGINT,
    html_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_github_app_org ON github_app(org_id);
