create table if not exists app_env_vars (
  id               uuid primary key default gen_random_uuid(),
  application_id   uuid not null references applications(id) on delete cascade,
  key              text not null,
  value            text,               -- NULL for secret vars
  is_secret        boolean not null default false,
  secret_version   integer not null default 1,
  docker_secret_id text not null default '',
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now(),
  unique(application_id, key)
);
create index if not exists idx_app_env_vars_app on app_env_vars(application_id);
