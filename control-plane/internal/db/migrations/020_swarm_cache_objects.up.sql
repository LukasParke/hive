-- Cache tables for the remaining Swarm object kinds (secrets, configs,
-- networks) so the watcher can mirror the whole cluster, not just
-- services/tasks/nodes. Same shape as the 016 tables: swarm_id is the Docker
-- object ID; rows are upserted on events and fully replaced on resync.

create table if not exists swarm_cache_secrets (
  swarm_id text primary key,
  name text not null,
  spec jsonb,
  status jsonb,
  updated_at timestamptz not null default now()
);

create index if not exists idx_swarm_cache_secrets_name on swarm_cache_secrets(name);

create table if not exists swarm_cache_configs (
  swarm_id text primary key,
  name text not null,
  spec jsonb,
  status jsonb,
  updated_at timestamptz not null default now()
);

create index if not exists idx_swarm_cache_configs_name on swarm_cache_configs(name);

create table if not exists swarm_cache_networks (
  swarm_id text primary key,
  name text not null,
  spec jsonb,
  status jsonb,
  updated_at timestamptz not null default now()
);

create index if not exists idx_swarm_cache_networks_name on swarm_cache_networks(name);
