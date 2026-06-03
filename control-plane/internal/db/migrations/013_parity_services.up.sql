create table if not exists redirects (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id) on delete cascade,
  domain_id uuid not null references domains(id) on delete cascade,
  path text not null default '/',
  target text not null,
  status_code integer not null default 301,
  permanent boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_redirects_org on redirects(organization_id);
create index if not exists idx_redirects_domain on redirects(domain_id);

create table if not exists mounts (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id) on delete cascade,
  application_id uuid not null references applications(id) on delete cascade,
  type text not null check (type in ('volume', 'bind', 'tmpfs')),
  source text not null,
  target text not null,
  read_only boolean not null default false,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_mounts_org on mounts(organization_id);
create index if not exists idx_mounts_app on mounts(application_id);

create table if not exists port_policies (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id) on delete cascade,
  application_id uuid not null references applications(id) on delete cascade,
  published_port integer not null,
  target_port integer not null,
  protocol text not null default 'tcp' check (protocol in ('tcp', 'udp', 'sctp')),
  mode text not null default 'ingress' check (mode in ('ingress', 'host')),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_port_policies_org on port_policies(organization_id);
create index if not exists idx_port_policies_app on port_policies(application_id);

create table if not exists volume_backups (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id) on delete cascade,
  volume_name text not null,
  status text not null default 'queued' check (status in ('queued', 'running', 'complete', 'failed')),
  size_bytes bigint,
  destination_id uuid references backup_destinations(id) on delete set null,
  error_message text,
  created_at timestamptz not null default now(),
  completed_at timestamptz
);

create index if not exists idx_volume_backups_org on volume_backups(organization_id);

create table if not exists preview_deployments (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id) on delete cascade,
  application_id uuid not null references applications(id) on delete cascade,
  pr_number integer not null,
  branch text not null,
  commit_sha text,
  status text not null default 'building' check (status in ('building', 'deploying', 'ready', 'failed', 'expired')),
  url text,
  expires_at timestamptz not null default (now() + interval '7 days'),
  created_at timestamptz not null default now()
);

create index if not exists idx_preview_deployments_org on preview_deployments(organization_id);
create index if not exists idx_preview_deployments_app on preview_deployments(application_id);
create index if not exists idx_preview_deployments_status on preview_deployments(status);
