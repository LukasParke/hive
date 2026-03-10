-- Hive baseline migration: creates all tables from scratch.
-- For upgrades from pre-Prisma (Go-only) installations, run:
--   npx prisma migrate resolve --applied 0001_baseline

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- BetterAuth tables ----------------------------------------------------------

CREATE TABLE IF NOT EXISTS "user" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "name" TEXT NOT NULL DEFAULT '',
    "email" TEXT NOT NULL,
    "emailVerified" BOOLEAN NOT NULL DEFAULT false,
    "image" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    CONSTRAINT "user_pkey" PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "user_email_key" ON "user"("email");

CREATE TABLE IF NOT EXISTS "session" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "expiresAt" TIMESTAMP(3) NOT NULL,
    "token" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    "ipAddress" TEXT,
    "userAgent" TEXT,
    "userId" TEXT NOT NULL,
    "activeOrg" TEXT NOT NULL DEFAULT 'default',
    CONSTRAINT "session_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "session_userId_fkey" FOREIGN KEY ("userId") REFERENCES "user"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS "session_token_key" ON "session"("token");

CREATE TABLE IF NOT EXISTS "account" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "accountId" TEXT NOT NULL,
    "providerId" TEXT NOT NULL,
    "userId" TEXT NOT NULL,
    "accessToken" TEXT,
    "refreshToken" TEXT,
    "idToken" TEXT,
    "accessTokenExpiresAt" TIMESTAMP(3),
    "refreshTokenExpiresAt" TIMESTAMP(3),
    "scope" TEXT,
    "password" TEXT,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3) NOT NULL,
    CONSTRAINT "account_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "account_userId_fkey" FOREIGN KEY ("userId") REFERENCES "user"("id") ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "verification" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "identifier" TEXT NOT NULL,
    "value" TEXT NOT NULL,
    "expiresAt" TIMESTAMP(3) NOT NULL,
    "createdAt" TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP,
    "updatedAt" TIMESTAMP(3),
    CONSTRAINT "verification_pkey" PRIMARY KEY ("id")
);

-- Application domain tables --------------------------------------------------

CREATE TABLE IF NOT EXISTS "project" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "name" TEXT NOT NULL,
    "org_id" TEXT NOT NULL,
    "description" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "project_pkey" PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_project_org" ON "project"("org_id");

CREATE TABLE IF NOT EXISTS "app" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "project_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "deploy_type" TEXT NOT NULL DEFAULT 'image',
    "image" TEXT NOT NULL DEFAULT '',
    "git_repo" TEXT NOT NULL DEFAULT '',
    "git_branch" TEXT NOT NULL DEFAULT 'main',
    "dockerfile_path" TEXT NOT NULL DEFAULT 'Dockerfile',
    "domain" TEXT NOT NULL DEFAULT '',
    "port" INTEGER NOT NULL DEFAULT 3000,
    "replicas" INTEGER NOT NULL DEFAULT 1,
    "env_encrypted" BYTEA,
    "status" TEXT NOT NULL DEFAULT 'pending',
    "cpu_limit" DOUBLE PRECISION NOT NULL DEFAULT 0,
    "memory_limit" BIGINT NOT NULL DEFAULT 0,
    "health_check_path" TEXT NOT NULL DEFAULT '',
    "health_check_interval" INTEGER NOT NULL DEFAULT 30,
    "homepage_labels" JSONB NOT NULL DEFAULT '{}',
    "extra_labels" JSONB NOT NULL DEFAULT '{}',
    "placement_constraints" JSONB NOT NULL DEFAULT '[]',
    "placement_preferences" JSONB NOT NULL DEFAULT '[]',
    "update_strategy" TEXT NOT NULL DEFAULT 'rolling',
    "update_parallelism" INTEGER NOT NULL DEFAULT 1,
    "update_delay" TEXT NOT NULL DEFAULT '5s',
    "update_failure_action" TEXT NOT NULL DEFAULT 'rollback',
    "update_order" TEXT NOT NULL DEFAULT 'stop-first',
    "build_cache_enabled" BOOLEAN NOT NULL DEFAULT true,
    "auto_deploy_branch" TEXT NOT NULL DEFAULT 'main',
    "preview_environments" BOOLEAN NOT NULL DEFAULT false,
    "template_name" TEXT NOT NULL DEFAULT '',
    "template_version" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "app_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "app_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "project"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_app_project" ON "app"("project_id");
CREATE INDEX IF NOT EXISTS "idx_app_git_repo" ON "app"("deploy_type", "git_repo");

CREATE TABLE IF NOT EXISTS "deployment" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "app_id" TEXT NOT NULL,
    "status" TEXT NOT NULL DEFAULT 'building',
    "commit_sha" TEXT NOT NULL DEFAULT '',
    "image_digest" TEXT NOT NULL DEFAULT '',
    "logs" TEXT NOT NULL DEFAULT '',
    "started_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "finished_at" TIMESTAMP(3),
    CONSTRAINT "deployment_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "deployment_app_id_fkey" FOREIGN KEY ("app_id") REFERENCES "app"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_deployment_app" ON "deployment"("app_id");

CREATE TABLE IF NOT EXISTS "managed_database" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "project_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "db_type" TEXT NOT NULL,
    "version" TEXT NOT NULL DEFAULT 'latest',
    "status" TEXT NOT NULL DEFAULT 'pending',
    "connection_encrypted" BYTEA,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "managed_database_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "managed_database_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "project"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_managed_database_project" ON "managed_database"("project_id");

CREATE TABLE IF NOT EXISTS "domain" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "app_id" TEXT NOT NULL,
    "domain" TEXT NOT NULL,
    "ssl_status" TEXT NOT NULL DEFAULT 'pending',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "domain_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "domain_app_id_fkey" FOREIGN KEY ("app_id") REFERENCES "app"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS "domain_domain_key" ON "domain"("domain");

CREATE TABLE IF NOT EXISTS "git_source" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "org_id" TEXT NOT NULL,
    "provider" TEXT NOT NULL,
    "token_encrypted" BYTEA,
    "webhook_secret_encrypted" BYTEA,
    "provider_name" TEXT NOT NULL DEFAULT '',
    "webhook_ids" JSONB NOT NULL DEFAULT '{}',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "git_source_pkey" PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_git_source_org" ON "git_source"("org_id");

CREATE TABLE IF NOT EXISTS "backup_config" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "resource_id" TEXT NOT NULL,
    "schedule" TEXT NOT NULL DEFAULT '0 3 * * *',
    "s3_bucket" TEXT NOT NULL DEFAULT '',
    "s3_prefix" TEXT NOT NULL DEFAULT '',
    "backup_type" TEXT NOT NULL DEFAULT 'database',
    "volume_id" TEXT,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "backup_config_pkey" PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_backup_config_resource" ON "backup_config"("resource_id");

CREATE TABLE IF NOT EXISTS "backup_run" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "config_id" TEXT NOT NULL,
    "status" TEXT NOT NULL DEFAULT 'running',
    "size" BIGINT NOT NULL DEFAULT 0,
    "target_path" TEXT NOT NULL DEFAULT '',
    "started_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "finished_at" TIMESTAMP(3),
    CONSTRAINT "backup_run_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "backup_run_config_id_fkey" FOREIGN KEY ("config_id") REFERENCES "backup_config"("id") ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "audit_log" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "user_id" TEXT NOT NULL,
    "org_id" TEXT NOT NULL,
    "action" TEXT NOT NULL,
    "resource" TEXT NOT NULL,
    "resource_id" TEXT NOT NULL DEFAULT '',
    "details" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "audit_log_pkey" PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_audit_log_org_time" ON "audit_log"("org_id", "created_at" DESC);

CREATE TABLE IF NOT EXISTS "catalog_template" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL DEFAULT '',
    "category" TEXT NOT NULL DEFAULT 'other',
    "icon_url" TEXT NOT NULL DEFAULT '',
    "template" TEXT NOT NULL,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "catalog_template_pkey" PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "catalog_template_name_key" ON "catalog_template"("name");

CREATE TABLE IF NOT EXISTS "notification_channel" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "org_id" TEXT NOT NULL,
    "name" TEXT NOT NULL DEFAULT '',
    "type" TEXT NOT NULL,
    "config" JSONB NOT NULL DEFAULT '{}',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "notification_channel_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "notification_event" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "channel_id" TEXT NOT NULL,
    "event_type" TEXT NOT NULL,
    "title" TEXT NOT NULL DEFAULT '',
    "message" TEXT NOT NULL DEFAULT '',
    "status" TEXT NOT NULL DEFAULT 'sent',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "notification_event_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "notification_event_channel_id_fkey" FOREIGN KEY ("channel_id") REFERENCES "notification_channel"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_notification_event_channel" ON "notification_event"("channel_id", "created_at" DESC);

CREATE TABLE IF NOT EXISTS "secret" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "project_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "docker_secret_id" TEXT NOT NULL DEFAULT '',
    "description" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "secret_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "secret_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "project"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS "secret_project_id_name_key" ON "secret"("project_id", "name");
CREATE INDEX IF NOT EXISTS "idx_secret_project" ON "secret"("project_id");

CREATE TABLE IF NOT EXISTS "storage_host" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "name" TEXT NOT NULL,
    "node_id" TEXT,
    "address" TEXT NOT NULL,
    "type" TEXT NOT NULL DEFAULT 'nas',
    "default_export_path" TEXT NOT NULL DEFAULT '',
    "default_mount_type" TEXT NOT NULL DEFAULT 'nfs',
    "mount_options_default" TEXT NOT NULL DEFAULT '',
    "credentials_encrypted" BYTEA,
    "capabilities" JSONB NOT NULL DEFAULT '{}',
    "node_label" TEXT NOT NULL DEFAULT '',
    "status" TEXT NOT NULL DEFAULT 'active',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "storage_host_pkey" PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "storage_host_name_key" ON "storage_host"("name");

CREATE TABLE IF NOT EXISTS "volume" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "project_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "driver" TEXT NOT NULL DEFAULT 'local',
    "driver_opts" JSONB NOT NULL DEFAULT '{}',
    "labels" JSONB NOT NULL DEFAULT '{}',
    "mount_type" TEXT NOT NULL DEFAULT 'volume',
    "remote_host" TEXT NOT NULL DEFAULT '',
    "remote_path" TEXT NOT NULL DEFAULT '',
    "mount_options" TEXT NOT NULL DEFAULT '',
    "scope" TEXT NOT NULL DEFAULT 'local',
    "status" TEXT NOT NULL DEFAULT 'pending',
    "storage_host_id" TEXT,
    "local_path" TEXT NOT NULL DEFAULT '',
    "ceph_pool" TEXT NOT NULL DEFAULT '',
    "ceph_image" TEXT NOT NULL DEFAULT '',
    "ceph_fs_name" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "volume_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "volume_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "project"("id") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "volume_storage_host_id_fkey" FOREIGN KEY ("storage_host_id") REFERENCES "storage_host"("id") ON DELETE SET NULL ON UPDATE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS "volume_project_id_name_key" ON "volume"("project_id", "name");
CREATE INDEX IF NOT EXISTS "idx_volume_project" ON "volume"("project_id");

CREATE TABLE IF NOT EXISTS "app_secret" (
    "app_id" TEXT NOT NULL,
    "secret_id" TEXT NOT NULL,
    "target" TEXT NOT NULL DEFAULT '',
    "uid" TEXT NOT NULL DEFAULT '0',
    "gid" TEXT NOT NULL DEFAULT '0',
    "mode" INTEGER NOT NULL DEFAULT 292,
    CONSTRAINT "app_secret_pkey" PRIMARY KEY ("app_id", "secret_id"),
    CONSTRAINT "app_secret_app_id_fkey" FOREIGN KEY ("app_id") REFERENCES "app"("id") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "app_secret_secret_id_fkey" FOREIGN KEY ("secret_id") REFERENCES "secret"("id") ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "app_volume" (
    "app_id" TEXT NOT NULL,
    "volume_id" TEXT NOT NULL,
    "container_path" TEXT NOT NULL,
    "read_only" BOOLEAN NOT NULL DEFAULT false,
    CONSTRAINT "app_volume_pkey" PRIMARY KEY ("app_id", "volume_id"),
    CONSTRAINT "app_volume_app_id_fkey" FOREIGN KEY ("app_id") REFERENCES "app"("id") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "app_volume_volume_id_fkey" FOREIGN KEY ("volume_id") REFERENCES "volume"("id") ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "proxy_route" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "project_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "domain" TEXT NOT NULL,
    "target_service" TEXT NOT NULL,
    "target_port" INTEGER NOT NULL DEFAULT 80,
    "protocol" TEXT NOT NULL DEFAULT 'http',
    "upstream_port" INTEGER,
    "ssl_mode" TEXT NOT NULL DEFAULT 'letsencrypt',
    "custom_cert_id" TEXT,
    "middleware_config" JSONB NOT NULL DEFAULT '{}',
    "enabled" BOOLEAN NOT NULL DEFAULT true,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "proxy_route_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "proxy_route_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "project"("id") ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "dns_provider" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "org_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "type" TEXT NOT NULL,
    "config_encrypted" BYTEA NOT NULL,
    "is_default" BOOLEAN NOT NULL DEFAULT false,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "dns_provider_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "custom_certificate" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "project_id" TEXT NOT NULL,
    "domain" TEXT NOT NULL,
    "cert_pem" TEXT NOT NULL,
    "key_pem_encrypted" BYTEA NOT NULL,
    "is_wildcard" BOOLEAN NOT NULL DEFAULT false,
    "provider" TEXT NOT NULL DEFAULT 'manual',
    "expires_at" TIMESTAMP(3),
    "auto_renew" BOOLEAN NOT NULL DEFAULT false,
    "dns_provider_id" TEXT,
    "last_renewed_at" TIMESTAMP(3),
    "renewal_error" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "custom_certificate_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "custom_certificate_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "project"("id") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "custom_certificate_dns_provider_id_fkey" FOREIGN KEY ("dns_provider_id") REFERENCES "dns_provider"("id")
);

CREATE TABLE IF NOT EXISTS "stack" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "project_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "compose_content" TEXT NOT NULL DEFAULT '',
    "status" TEXT NOT NULL DEFAULT 'pending',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "stack_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "stack_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "project"("id") ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "alert_threshold" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "org_id" TEXT NOT NULL,
    "metric" TEXT NOT NULL,
    "operator" TEXT NOT NULL DEFAULT '>',
    "value" DOUBLE PRECISION NOT NULL,
    "cooldown_minutes" INTEGER NOT NULL DEFAULT 5,
    "enabled" BOOLEAN NOT NULL DEFAULT true,
    "last_fired_at" TIMESTAMP(3),
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "alert_threshold_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "node_metrics_snapshot" (
    "id" BIGSERIAL NOT NULL,
    "node_id" TEXT NOT NULL,
    "metrics" JSONB NOT NULL,
    "collected_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "node_metrics_snapshot_pkey" PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_node_metrics_node_time" ON "node_metrics_snapshot"("node_id", "collected_at" DESC);

CREATE TABLE IF NOT EXISTS "dns_record" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "provider_id" TEXT NOT NULL,
    "app_id" TEXT,
    "domain" TEXT NOT NULL,
    "record_type" TEXT NOT NULL DEFAULT 'A',
    "value" TEXT NOT NULL,
    "proxied" BOOLEAN NOT NULL DEFAULT false,
    "managed" BOOLEAN NOT NULL DEFAULT true,
    "external_id" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "dns_record_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "dns_record_provider_id_fkey" FOREIGN KEY ("provider_id") REFERENCES "dns_provider"("id") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "dns_record_app_id_fkey" FOREIGN KEY ("app_id") REFERENCES "app"("id") ON DELETE SET NULL ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "preview_deployment" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "app_id" TEXT NOT NULL,
    "branch" TEXT NOT NULL,
    "pr_number" INTEGER,
    "domain" TEXT NOT NULL DEFAULT '',
    "status" TEXT NOT NULL DEFAULT 'pending',
    "service_name" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "preview_deployment_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "preview_deployment_app_id_fkey" FOREIGN KEY ("app_id") REFERENCES "app"("id") ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "service_link" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "source_app_id" TEXT NOT NULL,
    "target_app_id" TEXT,
    "target_database_id" TEXT,
    "env_prefix" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "service_link_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "service_link_source_app_id_fkey" FOREIGN KEY ("source_app_id") REFERENCES "app"("id") ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT "service_link_target_app_id_fkey" FOREIGN KEY ("target_app_id") REFERENCES "app"("id") ON DELETE SET NULL ON UPDATE CASCADE,
    CONSTRAINT "service_link_target_database_id_fkey" FOREIGN KEY ("target_database_id") REFERENCES "managed_database"("id") ON DELETE SET NULL ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "org_role" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "org_id" TEXT NOT NULL,
    "user_id" TEXT NOT NULL,
    "role" TEXT NOT NULL DEFAULT 'viewer',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "org_role_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "org_role_userId_fkey" FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS "org_role_org_id_user_id_key" ON "org_role"("org_id", "user_id");
CREATE INDEX IF NOT EXISTS "idx_org_role_user" ON "org_role"("user_id");
CREATE INDEX IF NOT EXISTS "idx_org_role_org" ON "org_role"("org_id");

CREATE TABLE IF NOT EXISTS "maintenance_task" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "org_id" TEXT NOT NULL,
    "type" TEXT NOT NULL,
    "schedule" TEXT NOT NULL DEFAULT '0 3 * * 0',
    "enabled" BOOLEAN NOT NULL DEFAULT true,
    "last_run_at" TIMESTAMP(3),
    "last_status" TEXT NOT NULL DEFAULT '',
    "config" JSONB NOT NULL DEFAULT '{}',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "maintenance_task_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "maintenance_run" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "task_id" TEXT NOT NULL,
    "status" TEXT NOT NULL DEFAULT 'running',
    "details" TEXT NOT NULL DEFAULT '',
    "started_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "finished_at" TIMESTAMP(3),
    CONSTRAINT "maintenance_run_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "maintenance_run_task_id_fkey" FOREIGN KEY ("task_id") REFERENCES "maintenance_task"("id") ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "app_env_var" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "app_id" TEXT NOT NULL,
    "key" TEXT NOT NULL,
    "value_encrypted" BYTEA NOT NULL,
    "is_secret" BOOLEAN NOT NULL DEFAULT false,
    "source" TEXT NOT NULL DEFAULT 'user',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "app_env_var_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "app_env_var_app_id_fkey" FOREIGN KEY ("app_id") REFERENCES "app"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS "app_env_var_app_id_key_key" ON "app_env_var"("app_id", "key");

CREATE TABLE IF NOT EXISTS "log_entry" (
    "id" BIGSERIAL NOT NULL,
    "app_id" TEXT NOT NULL,
    "service_name" TEXT NOT NULL DEFAULT '',
    "node_id" TEXT NOT NULL DEFAULT '',
    "stream" TEXT NOT NULL DEFAULT 'stdout',
    "message" TEXT NOT NULL,
    "level" TEXT NOT NULL DEFAULT 'info',
    "timestamp" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "log_entry_pkey" PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_log_entry_app_ts" ON "log_entry"("app_id", "timestamp" DESC);
CREATE INDEX IF NOT EXISTS "idx_log_entry_level" ON "log_entry"("level");

CREATE TABLE IF NOT EXISTS "log_forward_config" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "org_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "type" TEXT NOT NULL DEFAULT 'webhook',
    "config_encrypted" BYTEA,
    "enabled" BOOLEAN NOT NULL DEFAULT true,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "log_forward_config_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "template_source" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "org_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "url" TEXT NOT NULL,
    "type" TEXT NOT NULL DEFAULT 'git',
    "last_synced_at" TIMESTAMP(3),
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "template_source_pkey" PRIMARY KEY ("id")
);

CREATE TABLE IF NOT EXISTS "custom_template" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::text,
    "org_id" TEXT NOT NULL,
    "source_id" TEXT,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL DEFAULT '',
    "category" TEXT NOT NULL DEFAULT 'custom',
    "icon" TEXT NOT NULL DEFAULT '',
    "image" TEXT NOT NULL DEFAULT '',
    "version" TEXT NOT NULL DEFAULT '1.0.0',
    "ports" TEXT NOT NULL DEFAULT '[]',
    "env" TEXT NOT NULL DEFAULT '{}',
    "volumes" TEXT NOT NULL DEFAULT '[]',
    "domain" TEXT NOT NULL DEFAULT '',
    "replicas" INTEGER NOT NULL DEFAULT 1,
    "is_stack" BOOLEAN NOT NULL DEFAULT false,
    "compose_content" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "custom_template_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "custom_template_source_id_fkey" FOREIGN KEY ("source_id") REFERENCES "template_source"("id") ON DELETE SET NULL ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS "ceph_cluster" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "name" TEXT NOT NULL,
    "fsid" TEXT,
    "status" TEXT NOT NULL DEFAULT 'pending',
    "bootstrap_node_id" TEXT NOT NULL,
    "mon_hosts" TEXT[] NOT NULL,
    "public_network" TEXT NOT NULL DEFAULT '',
    "cluster_network" TEXT NOT NULL DEFAULT '',
    "ceph_conf_encrypted" BYTEA,
    "admin_keyring_encrypted" BYTEA,
    "replication_size" INTEGER NOT NULL DEFAULT 3,
    "storage_host_id" TEXT,
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "ceph_cluster_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "ceph_cluster_storage_host_id_fkey" FOREIGN KEY ("storage_host_id") REFERENCES "storage_host"("id") ON DELETE SET NULL ON UPDATE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS "ceph_cluster_name_key" ON "ceph_cluster"("name");
CREATE UNIQUE INDEX IF NOT EXISTS "ceph_cluster_fsid_key" ON "ceph_cluster"("fsid");

CREATE TABLE IF NOT EXISTS "ceph_osd" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "cluster_id" TEXT NOT NULL,
    "node_id" TEXT NOT NULL,
    "hostname" TEXT NOT NULL,
    "osd_id" INTEGER,
    "device_path" TEXT NOT NULL,
    "device_size" BIGINT NOT NULL DEFAULT 0,
    "device_type" TEXT NOT NULL DEFAULT 'hdd',
    "status" TEXT NOT NULL DEFAULT 'pending',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "ceph_osd_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "ceph_osd_cluster_id_fkey" FOREIGN KEY ("cluster_id") REFERENCES "ceph_cluster"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_ceph_osd_cluster" ON "ceph_osd"("cluster_id");

CREATE TABLE IF NOT EXISTS "ceph_pool" (
    "id" TEXT NOT NULL DEFAULT gen_random_uuid()::TEXT,
    "cluster_id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "pool_id" INTEGER,
    "pg_num" INTEGER NOT NULL DEFAULT 32,
    "size" INTEGER NOT NULL DEFAULT 3,
    "type" TEXT NOT NULL DEFAULT 'replicated',
    "application" TEXT NOT NULL DEFAULT 'rbd',
    "created_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "ceph_pool_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "ceph_pool_cluster_id_fkey" FOREIGN KEY ("cluster_id") REFERENCES "ceph_cluster"("id") ON DELETE CASCADE ON UPDATE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_ceph_pool_cluster" ON "ceph_pool"("cluster_id");
