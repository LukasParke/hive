alter table applications
  add column if not exists repository_url text,
  add column if not exists git_ref text default 'main',
  add column if not exists container_port integer default 3000;

create table if not exists deployments (
  id uuid primary key default gen_random_uuid(),
  application_id uuid not null references applications(id) on delete cascade,
  image_tag text not null,
  status text not null,
  trigger text not null,
  created_at timestamptz not null default now()
);

create index if not exists idx_deployments_app_created on deployments(application_id, created_at desc);
