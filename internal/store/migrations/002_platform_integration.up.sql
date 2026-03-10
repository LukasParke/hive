-- Settings key-value table for persistent configuration
CREATE TABLE IF NOT EXISTS setting (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Add storage mode columns to managed_database
ALTER TABLE managed_database ADD COLUMN IF NOT EXISTS storage_mode TEXT NOT NULL DEFAULT 'local';
ALTER TABLE managed_database ADD COLUMN IF NOT EXISTS storage_host_id TEXT;
ALTER TABLE managed_database ADD COLUMN IF NOT EXISTS node_id TEXT;

-- Add backup destination columns to backup_config  
ALTER TABLE backup_config ADD COLUMN IF NOT EXISTS destination TEXT NOT NULL DEFAULT 'local';
ALTER TABLE backup_config ADD COLUMN IF NOT EXISTS nas_host_id TEXT;
ALTER TABLE backup_config ADD COLUMN IF NOT EXISTS nas_path TEXT NOT NULL DEFAULT '';
ALTER TABLE backup_config ADD COLUMN IF NOT EXISTS local_path TEXT NOT NULL DEFAULT '';
ALTER TABLE backup_config ADD COLUMN IF NOT EXISTS retention_days INT NOT NULL DEFAULT 30;
