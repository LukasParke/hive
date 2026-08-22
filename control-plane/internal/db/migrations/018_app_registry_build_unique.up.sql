-- Applications can be pinned to a registry for image pulls; nullable so
-- existing apps keep using the default registry resolution.

alter table applications
  add column if not exists registry_id uuid references registries(id) on delete set null;

create index if not exists idx_applications_registry on applications(registry_id);

-- At most one queued/building job per application: the build planner relies
-- on this to avoid enqueueing duplicate work while a build is in flight.
-- Completed/failed rows are exempt so history is preserved.

create unique index if not exists idx_build_jobs_active_per_application
  on build_jobs(application_id)
  where status in ('queued', 'building');
