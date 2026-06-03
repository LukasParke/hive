drop index if exists idx_deployments_app_created;
drop table if exists deployments;
alter table applications
  drop column if exists container_port,
  drop column if exists git_ref,
  drop column if exists repository_url;
