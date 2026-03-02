CREATE TABLE secret (
    id               TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    project_id       TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    docker_secret_id TEXT NOT NULL DEFAULT '',
    description      TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, name)
);

CREATE INDEX idx_secret_project ON secret(project_id);

CREATE TABLE volume (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    project_id    TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    driver        TEXT NOT NULL DEFAULT 'local',
    driver_opts   JSONB NOT NULL DEFAULT '{}',
    labels        JSONB NOT NULL DEFAULT '{}',
    mount_type    TEXT NOT NULL DEFAULT 'volume',
    remote_host   TEXT NOT NULL DEFAULT '',
    remote_path   TEXT NOT NULL DEFAULT '',
    mount_options TEXT NOT NULL DEFAULT '',
    scope         TEXT NOT NULL DEFAULT 'local',
    status        TEXT NOT NULL DEFAULT 'pending',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, name)
);

CREATE INDEX idx_volume_project ON volume(project_id);

CREATE TABLE app_secret (
    app_id    TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    secret_id TEXT NOT NULL REFERENCES secret(id) ON DELETE CASCADE,
    target    TEXT NOT NULL DEFAULT '',
    uid       TEXT NOT NULL DEFAULT '0',
    gid       TEXT NOT NULL DEFAULT '0',
    mode      INTEGER NOT NULL DEFAULT 292,
    PRIMARY KEY (app_id, secret_id)
);

CREATE TABLE app_volume (
    app_id         TEXT NOT NULL REFERENCES app(id) ON DELETE CASCADE,
    volume_id      TEXT NOT NULL REFERENCES volume(id) ON DELETE CASCADE,
    container_path TEXT NOT NULL,
    read_only      BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (app_id, volume_id)
);
