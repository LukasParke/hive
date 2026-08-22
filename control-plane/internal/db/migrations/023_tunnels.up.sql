create table tunnels (
  id uuid primary key default gen_random_uuid(),
  name text not null unique,
  cf_tunnel_id text not null unique,
  account_id text not null,
  zone_id text,
  credential_secret_name text not null,
  ingress jsonb not null default '[]',
  dns_records jsonb not null default '{}',
  status text not null default 'creating' check (status in ('creating','deployed','error','deleting')),
  error_message text,
  created_at timestamptz default now(),
  updated_at timestamptz default now()
);
