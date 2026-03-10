ALTER TABLE backup_config DROP COLUMN IF EXISTS retention_days;
ALTER TABLE backup_config DROP COLUMN IF EXISTS local_path;
ALTER TABLE backup_config DROP COLUMN IF EXISTS nas_path;
ALTER TABLE backup_config DROP COLUMN IF EXISTS nas_host_id;
ALTER TABLE backup_config DROP COLUMN IF EXISTS destination;

ALTER TABLE managed_database DROP COLUMN IF EXISTS node_id;
ALTER TABLE managed_database DROP COLUMN IF EXISTS storage_host_id;
ALTER TABLE managed_database DROP COLUMN IF EXISTS storage_mode;

DROP TABLE IF EXISTS setting;
