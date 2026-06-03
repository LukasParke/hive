create type member_role as enum ('owner', 'admin', 'member');

create table if not exists users (
  id uuid primary key default gen_random_uuid(),
  email text not null unique,
  password_hash text not null,
  display_name text not null default '',
  is_active boolean not null default true,
  created_at timestamptz not null default now()
);

create table if not exists organizations (
  id uuid primary key default gen_random_uuid(),
  name text not null unique,
  slug text not null unique,
  created_at timestamptz not null default now()
);

create table if not exists organization_members (
  organization_id uuid not null references organizations(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  role member_role not null,
  created_at timestamptz not null default now(),
  primary key (organization_id, user_id)
);

create table if not exists sessions (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id) on delete cascade,
  refresh_token_hash text not null unique,
  expires_at timestamptz not null,
  created_at timestamptz not null default now()
);

create table if not exists api_keys (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id) on delete cascade,
  name text not null,
  token_hash text not null unique,
  last_used_at timestamptz,
  created_at timestamptz not null default now()
);
