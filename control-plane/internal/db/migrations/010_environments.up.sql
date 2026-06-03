create table if not exists environments (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  name text not null,
  slug text not null,
  created_at timestamptz not null default now(),
  unique(project_id, name),
  unique(project_id, slug)
);

create index if not exists idx_environments_project_created on environments(project_id, created_at desc);
