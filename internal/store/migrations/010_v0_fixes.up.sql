-- Fix FK cascade on volume.storage_host_id
ALTER TABLE volume DROP CONSTRAINT IF EXISTS volume_storage_host_id_fkey;
ALTER TABLE volume ADD CONSTRAINT volume_storage_host_id_fkey
    FOREIGN KEY (storage_host_id) REFERENCES storage_host(id) ON DELETE SET NULL;

-- Add missing indexes
CREATE INDEX IF NOT EXISTS idx_git_source_org ON git_source(org_id);
CREATE INDEX IF NOT EXISTS idx_backup_config_resource ON backup_config(resource_id);
CREATE INDEX IF NOT EXISTS idx_app_git_repo ON app(deploy_type, git_repo) WHERE deploy_type = 'git';
CREATE INDEX IF NOT EXISTS idx_audit_log_org_time ON audit_log(org_id, created_at DESC);

-- Ensure notification_channel has name column (idempotent, already in 004 but belt-and-suspenders)
ALTER TABLE notification_channel ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';

-- Add template_name / template_version to app if not present (already in 009 but ensure)
ALTER TABLE app ADD COLUMN IF NOT EXISTS template_name TEXT NOT NULL DEFAULT '';
ALTER TABLE app ADD COLUMN IF NOT EXISTS template_version TEXT NOT NULL DEFAULT '';
