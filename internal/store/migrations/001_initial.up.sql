CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE project (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    name        TEXT NOT NULL,
    org_id      TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_project_org ON project(org_id);

CREATE TABLE app (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    project_id      TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    deploy_type     TEXT NOT NULL DEFAULT 'image',
    image           TEXT NOT NULL DEFAULT '',
    git_repo        TEXT NOT NULL DEFAULT '',
    git_branch      TEXT NOT NULL DEFAULT 'main',
    dockerfile_path TEXT NOT NULL DEFAULT 'Dockerfile',
    domain          TEXT NOT NULL DEFAULT '',
    port            INTEGER NOT NULL DEFAULT 3000,
    replicas        INTEGER NOT NULL DEFAULT 1,
    env_encrypted   BYTEA,
    status          TEXT NOT NULL DEFAULT 'pending',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_app_project ON app(project_id);

CREATE TABLE deployment (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    app_id       TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'building',
    commit_sha   TEXT NOT NULL DEFAULT '',
    image_digest TEXT NOT NULL DEFAULT '',
    logs         TEXT NOT NULL DEFAULT '',
    started_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at  TIMESTAMPTZ
);

CREATE INDEX idx_deployment_app ON deployment(app_id);

CREATE TABLE managed_database (
    id                    TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    project_id            TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name                  TEXT NOT NULL,
    db_type               TEXT NOT NULL,
    version               TEXT NOT NULL DEFAULT 'latest',
    status                TEXT NOT NULL DEFAULT 'pending',
    connection_encrypted  BYTEA,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_managed_database_project ON managed_database(project_id);

CREATE TABLE domain (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    app_id     TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    domain     TEXT NOT NULL UNIQUE,
    ssl_status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE git_source (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id          TEXT NOT NULL,
    provider        TEXT NOT NULL,
    token_encrypted BYTEA,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE backup_config (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    resource_id TEXT NOT NULL,
    schedule    TEXT NOT NULL DEFAULT '0 3 * * *',
    s3_bucket   TEXT NOT NULL DEFAULT '',
    s3_prefix   TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE backup_run (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    config_id   TEXT NOT NULL REFERENCES backup_config(id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'running',
    size        BIGINT NOT NULL DEFAULT 0,
    target_path TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE TABLE audit_log (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id     TEXT NOT NULL,
    org_id      TEXT NOT NULL,
    action      TEXT NOT NULL,
    resource    TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    details     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_log_org ON audit_log(org_id, created_at DESC);

CREATE TABLE catalog_template (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT 'other',
    icon_url    TEXT NOT NULL DEFAULT '',
    template    TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE notification_channel (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    org_id     TEXT NOT NULL,
    type       TEXT NOT NULL,
    config     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
