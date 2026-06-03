create table if not exists schedules (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  cron_expr text not null,
  target_type text not null,
  target_id text not null,
  enabled boolean not null default true,
  last_run_at timestamptz,
  created_at timestamptz not null default now()
);

create index if not exists idx_schedules_created_at on schedules(created_at desc);
