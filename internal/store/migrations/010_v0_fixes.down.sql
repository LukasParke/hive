-- Revert FK to plain reference (no cascade)
ALTER TABLE volume DROP CONSTRAINT IF EXISTS volume_storage_host_id_fkey;
ALTER TABLE volume ADD CONSTRAINT volume_storage_host_id_fkey
    FOREIGN KEY (storage_host_id) REFERENCES storage_host(id);

-- Drop added indexes
DROP INDEX IF EXISTS idx_git_source_org;
DROP INDEX IF EXISTS idx_backup_config_resource;
DROP INDEX IF EXISTS idx_app_git_repo;
DROP INDEX IF EXISTS idx_audit_log_org_time;
