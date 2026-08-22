-- Full-resync helpers for the Swarm object cache. The pattern is:
-- upsert every live object, then delete cached rows whose swarm_id is no
-- longer present in the cluster snapshot.

-- name: UpsertCacheService :exec
insert into swarm_cache_services (swarm_id, name, spec, status, updated_at)
values ($1, $2, $3, $4, now())
on conflict (swarm_id) do update
set name = excluded.name,
    spec = excluded.spec,
    status = excluded.status,
    updated_at = now();

-- name: ListCachedServices :many
select swarm_id, name, spec, status, updated_at
from swarm_cache_services
order by name;

-- name: DeleteMissingCacheServices :exec
delete from swarm_cache_services
where swarm_id <> all($1::text[]);

-- name: UpsertCacheTask :exec
insert into swarm_cache_tasks (swarm_id, name, spec, status, updated_at)
values ($1, $2, $3, $4, now())
on conflict (swarm_id) do update
set name = excluded.name,
    spec = excluded.spec,
    status = excluded.status,
    updated_at = now();

-- name: ListCachedTasks :many
select swarm_id, name, spec, status, updated_at
from swarm_cache_tasks
order by name;

-- name: DeleteMissingCacheTasks :exec
delete from swarm_cache_tasks
where swarm_id <> all($1::text[]);

-- name: UpsertCacheNode :exec
insert into swarm_cache_nodes (swarm_id, name, spec, status, updated_at)
values ($1, $2, $3, $4, now())
on conflict (swarm_id) do update
set name = excluded.name,
    spec = excluded.spec,
    status = excluded.status,
    updated_at = now();

-- name: ListCachedNodes :many
select swarm_id, name, spec, status, updated_at
from swarm_cache_nodes
order by name;

-- name: DeleteMissingCacheNodes :exec
delete from swarm_cache_nodes
where swarm_id <> all($1::text[]);

-- name: DeleteCacheServiceBySwarmID :exec
delete from swarm_cache_services
where swarm_id = $1;

-- name: DeleteCacheTaskBySwarmID :exec
delete from swarm_cache_tasks
where swarm_id = $1;

-- name: DeleteCacheNodeBySwarmID :exec
delete from swarm_cache_nodes
where swarm_id = $1;

-- name: UpsertCacheSecret :exec
insert into swarm_cache_secrets (swarm_id, name, spec, status, updated_at)
values ($1, $2, $3, $4, now())
on conflict (swarm_id) do update
set name = excluded.name,
    spec = excluded.spec,
    status = excluded.status,
    updated_at = now();

-- name: ListCachedSecrets :many
select swarm_id, name, spec, status, updated_at
from swarm_cache_secrets
order by name;

-- name: DeleteMissingCacheSecrets :exec
delete from swarm_cache_secrets
where swarm_id <> all($1::text[]);

-- name: DeleteCacheSecretBySwarmID :exec
delete from swarm_cache_secrets
where swarm_id = $1;

-- name: UpsertCacheConfig :exec
insert into swarm_cache_configs (swarm_id, name, spec, status, updated_at)
values ($1, $2, $3, $4, now())
on conflict (swarm_id) do update
set name = excluded.name,
    spec = excluded.spec,
    status = excluded.status,
    updated_at = now();

-- name: ListCachedConfigs :many
select swarm_id, name, spec, status, updated_at
from swarm_cache_configs
order by name;

-- name: DeleteMissingCacheConfigs :exec
delete from swarm_cache_configs
where swarm_id <> all($1::text[]);

-- name: DeleteCacheConfigBySwarmID :exec
delete from swarm_cache_configs
where swarm_id = $1;

-- name: UpsertCacheNetwork :exec
insert into swarm_cache_networks (swarm_id, name, spec, status, updated_at)
values ($1, $2, $3, $4, now())
on conflict (swarm_id) do update
set name = excluded.name,
    spec = excluded.spec,
    status = excluded.status,
    updated_at = now();

-- name: ListCachedNetworks :many
select swarm_id, name, spec, status, updated_at
from swarm_cache_networks
order by name;

-- name: DeleteMissingCacheNetworks :exec
delete from swarm_cache_networks
where swarm_id <> all($1::text[]);

-- name: DeleteCacheNetworkBySwarmID :exec
delete from swarm_cache_networks
where swarm_id = $1;
