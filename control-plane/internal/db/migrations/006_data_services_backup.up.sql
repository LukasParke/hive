create table if not exists database_services (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  engine text not null,
  name text not null,
  version text not null default 'latest',
  service_name text not null,
  username text,
  password_secret_name text,
  database_name text,
  port integer not null,
  created_at timestamptz not null default now()
);

alter table backup_runs
  add column if not exists artifact_path text,
  add column if not exists error_message text,
  add column if not exists schedule text not null default 'manual',
  add column if not exists restore_target text;
