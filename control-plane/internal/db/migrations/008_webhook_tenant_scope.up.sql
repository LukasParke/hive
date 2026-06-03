alter table projects
  add column if not exists organization_id uuid references organizations(id) on delete cascade;

create index if not exists idx_projects_org_created on projects(organization_id, created_at desc);

alter table applications
  add column if not exists watch_paths text[] not null default '{}'::text[];
