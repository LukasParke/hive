create table if not exists password_reset_tokens (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id) on delete cascade,
  token_hash text not null unique,
  expires_at timestamptz not null,
  created_at timestamptz not null default now()
);

create table if not exists organization_invitations (
  id uuid primary key default gen_random_uuid(),
  organization_id uuid not null references organizations(id) on delete cascade,
  email text not null,
  role member_role not null default 'member',
  token_hash text not null unique,
  status text not null default 'pending',
  created_by uuid references users(id) on delete set null,
  expires_at timestamptz not null default (now() + interval '7 days'),
  created_at timestamptz not null default now()
);

create unique index if not exists idx_org_invitations_org_email_pending
  on organization_invitations(organization_id, lower(email))
  where status = 'pending';

create table if not exists servers (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  host text not null,
  ssh_port integer not null default 22,
  description text not null default '',
  created_at timestamptz not null default now()
);

create table if not exists ssh_keys (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  public_key text not null,
  private_key text not null default '',
  created_at timestamptz not null default now()
);

create table if not exists certificates (
  id uuid primary key default gen_random_uuid(),
  domain text not null unique,
  cert_pem text not null,
  key_pem text not null,
  created_at timestamptz not null default now()
);

create table if not exists request_events (
  id uuid primary key default gen_random_uuid(),
  category text not null,
  message text not null,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);
