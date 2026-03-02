ALTER TABLE app ADD COLUMN IF NOT EXISTS template_name TEXT NOT NULL DEFAULT '';
ALTER TABLE app ADD COLUMN IF NOT EXISTS template_version TEXT NOT NULL DEFAULT '';

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
