create extension if not exists pgcrypto;

create type source_type as enum ('git', 'image', 'compose');
create type build_status as enum ('queued', 'building', 'pushing', 'deploying', 'complete', 'failed', 'cancelled');
create type secret_type as enum ('ssh_key', 'tls_cert', 'tls_key', 'signing_key', 'ca_key', 'ca_cert');

create table if not exists projects (
  id uuid primary key default gen_random_uuid(),
  name text not null unique,
  created_at timestamptz not null default now()
);

create table if not exists applications (
  id uuid primary key default gen_random_uuid(),
  project_id uuid not null references projects(id) on delete cascade,
  name text not null,
  source_type source_type not null,
  image text,
  service_spec jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  unique(project_id, name)
);

create table if not exists build_jobs (
  id uuid primary key default gen_random_uuid(),
  application_id uuid not null references applications(id) on delete cascade,
  status build_status not null default 'queued',
  trigger text not null,
  git_ref text,
  git_sha text,
  image_tag text,
  logs text not null default '',
  started_at timestamptz,
  completed_at timestamptz,
  error_message text,
  created_at timestamptz not null default now()
);

create table if not exists secrets_store (
  id uuid primary key default gen_random_uuid(),
  name text not null unique,
  type secret_type not null,
  encrypted_value bytea not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists audit_log (
  id bigserial primary key,
  user_id uuid,
  action text not null,
  resource_type text not null,
  resource_id text not null,
  details jsonb not null default '{}'::jsonb,
  ip_address inet,
  created_at timestamptz not null default now()
);
