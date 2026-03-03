CREATE TABLE IF NOT EXISTS auth_user (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    email         TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS auth_session (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    token      TEXT NOT NULL UNIQUE,
    user_id    TEXT NOT NULL REFERENCES auth_user(id) ON DELETE CASCADE,
    active_org TEXT NOT NULL DEFAULT 'default',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_auth_session_token ON auth_session(token);
CREATE INDEX IF NOT EXISTS idx_auth_session_user ON auth_session(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_session_expires ON auth_session(expires_at);
