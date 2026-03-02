DROP TABLE IF EXISTS maintenance_run;
DROP TABLE IF EXISTS maintenance_task;
DROP TABLE IF EXISTS org_role;
DROP TABLE IF EXISTS service_link;
DROP TABLE IF EXISTS preview_deployment;
DROP TABLE IF EXISTS dns_record;
DROP TABLE IF EXISTS dns_provider;

ALTER TABLE custom_certificate DROP COLUMN IF EXISTS auto_renew;
ALTER TABLE custom_certificate DROP COLUMN IF EXISTS dns_provider_id;
ALTER TABLE custom_certificate DROP COLUMN IF EXISTS last_renewed_at;
ALTER TABLE custom_certificate DROP COLUMN IF EXISTS renewal_error;

ALTER TABLE app DROP COLUMN IF EXISTS build_cache_enabled;
ALTER TABLE app DROP COLUMN IF EXISTS auto_deploy_branch;
ALTER TABLE app DROP COLUMN IF EXISTS preview_environments;

ALTER TABLE git_source DROP COLUMN IF EXISTS webhook_secret_encrypted;
ALTER TABLE git_source DROP COLUMN IF EXISTS provider_name;

ALTER TABLE proxy_route DROP COLUMN IF EXISTS protocol;
ALTER TABLE proxy_route DROP COLUMN IF EXISTS upstream_port;
