DROP TABLE IF EXISTS alert_threshold;
DROP TABLE IF EXISTS stack;
DROP TABLE IF EXISTS custom_certificate;
DROP TABLE IF EXISTS proxy_route;

ALTER TABLE app DROP COLUMN IF EXISTS homepage_labels;
ALTER TABLE app DROP COLUMN IF EXISTS extra_labels;
ALTER TABLE app DROP COLUMN IF EXISTS placement_constraints;
ALTER TABLE app DROP COLUMN IF EXISTS placement_preferences;
ALTER TABLE app DROP COLUMN IF EXISTS update_strategy;
ALTER TABLE app DROP COLUMN IF EXISTS update_parallelism;
ALTER TABLE app DROP COLUMN IF EXISTS update_delay;
ALTER TABLE app DROP COLUMN IF EXISTS update_failure_action;
ALTER TABLE app DROP COLUMN IF EXISTS update_order;

ALTER TABLE backup_config DROP COLUMN IF EXISTS backup_type;
ALTER TABLE backup_config DROP COLUMN IF EXISTS volume_id;

ALTER TABLE notification_channel DROP COLUMN IF EXISTS name;

DROP INDEX IF EXISTS idx_node_metrics_node_time;
DROP TABLE IF EXISTS node_metrics_snapshot;

ALTER TABLE volume DROP COLUMN IF EXISTS storage_host_id;
ALTER TABLE volume DROP COLUMN IF EXISTS local_path;
ALTER TABLE volume DROP COLUMN IF EXISTS ceph_pool;
ALTER TABLE volume DROP COLUMN IF EXISTS ceph_image;
ALTER TABLE volume DROP COLUMN IF EXISTS ceph_fs_name;

DROP TABLE IF EXISTS storage_host;
