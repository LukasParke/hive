create table if not exists security_rules (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id) on delete cascade,
  application_id uuid references applications(id) on delete cascade,
  name text not null,
  type text not null check (type in ('ip_allowlist','ip_blocklist','country_block','header_security','rate_limit')),
  config jsonb not null default '{}'::jsonb,
  priority int not null default 0,
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists idx_security_rules_org on security_rules(organization_id);
create index if not exists idx_security_rules_app on security_rules(application_id);
create index if not exists idx_security_rules_enabled on security_rules(organization_id, enabled);
