ALTER TABLE app ADD COLUMN IF NOT EXISTS cpu_limit DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE app ADD COLUMN IF NOT EXISTS memory_limit BIGINT NOT NULL DEFAULT 0;
ALTER TABLE app ADD COLUMN IF NOT EXISTS health_check_path TEXT NOT NULL DEFAULT '';
ALTER TABLE app ADD COLUMN IF NOT EXISTS health_check_interval INTEGER NOT NULL DEFAULT 30;

CREATE TABLE IF NOT EXISTS notification_event (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    channel_id TEXT NOT NULL REFERENCES notification_channel(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    message    TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'sent',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_event_channel ON notification_event(channel_id, created_at DESC);
