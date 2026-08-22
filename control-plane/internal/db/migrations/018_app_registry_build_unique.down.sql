drop index if exists idx_build_jobs_active_per_application;

drop index if exists idx_applications_registry;

alter table applications
  drop column if exists registry_id;
