-- Cache of Docker Swarm objects (services, tasks, nodes) mirrored from the
-- live cluster so the control plane can serve lists without hitting the API
-- on every request. swarm_id is the Docker object ID; rows are fully
-- replaced on each resync (upsert + delete-missing).

create table if not exists swarm_cache_services (
  swarm_id text primary key,
  name text not null,
  spec jsonb,
  status jsonb,
  updated_at timestamptz not null default now()
);

create index if not exists idx_swarm_cache_services_name on swarm_cache_services(name);

create table if not exists swarm_cache_tasks (
  swarm_id text primary key,
  name text not null,
  spec jsonb,
  status jsonb,
  updated_at timestamptz not null default now()
);

create index if not exists idx_swarm_cache_tasks_name on swarm_cache_tasks(name);

create table if not exists swarm_cache_nodes (
  swarm_id text primary key,
  name text not null,
  spec jsonb,
  status jsonb,
  updated_at timestamptz not null default now()
);

create index if not exists idx_swarm_cache_nodes_name on swarm_cache_nodes(name);
