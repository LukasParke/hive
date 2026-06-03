create table if not exists domains (
  id uuid primary key default gen_random_uuid(),
  application_id uuid not null references applications(id) on delete cascade,
  hostname text not null unique,
  tls_enabled boolean not null default true,
  created_at timestamptz not null default now()
);

create table if not exists registries (
  id uuid primary key default gen_random_uuid(),
  name text not null unique,
  url text not null,
  username text,
  secret_name text,
  is_default boolean not null default false,
  created_at timestamptz not null default now()
);

create table if not exists stacks (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  name text not null,
  compose_content text not null,
  created_at timestamptz not null default now(),
  unique(project_id, name)
);

create table if not exists app_settings (
  key text primary key,
  value jsonb not null default '{}'::jsonb,
  updated_at timestamptz not null default now()
);

create table if not exists notifications (
  id uuid primary key default gen_random_uuid(),
  channel text not null,
  target text not null,
  enabled boolean not null default true,
  created_at timestamptz not null default now()
);

create table if not exists git_providers (
  id uuid primary key default gen_random_uuid(),
  type text not null,
  name text not null,
  base_url text not null,
  token_secret_name text,
  created_at timestamptz not null default now()
);

create table if not exists backup_destinations (
  id uuid primary key default gen_random_uuid(),
  name text not null unique,
  type text not null,
  config jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);

create table if not exists backup_runs (
  id uuid primary key default gen_random_uuid(),
  target_type text not null,
  target_id text not null,
  destination_id uuid references backup_destinations(id) on delete set null,
  status text not null,
  started_at timestamptz,
  completed_at timestamptz,
  details jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);
