DROP TABLE IF EXISTS notification_event;
ALTER TABLE app DROP COLUMN IF EXISTS cpu_limit;
ALTER TABLE app DROP COLUMN IF EXISTS memory_limit;
ALTER TABLE app DROP COLUMN IF EXISTS health_check_path;
ALTER TABLE app DROP COLUMN IF EXISTS health_check_interval;
