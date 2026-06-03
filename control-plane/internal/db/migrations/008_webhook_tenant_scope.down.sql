alter table applications
  drop column if exists watch_paths;

drop index if exists idx_projects_org_created;

alter table projects
  drop column if exists organization_id;
