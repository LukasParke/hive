# Storage Model

Hive storage is split by lifecycle:

- `ephemeral-local`: temporary build workspaces and caches (`/data/local`)
- `shared-durable`: ACME state and backup staging (`/data/shared`)
- `database-secrets`: encrypted values in `secrets_store`
- `service-volumes`: Swarm volumes for Postgres, registry, and Buildkit caches

`swarm_cache_*` tables cache observed Swarm services/tasks/nodes for fast
list endpoints; they are rebuilt by the docker-events watcher and are safe to
resync (see [runbook](runbook.md#swarm-cache-staleness)).

Docker named volumes (`hive_pgdata`, `hive_registry-data`) are node-local:
pin `db=true` / `registry=true` labels to the nodes holding them, or use a
shared/external volume driver ([production guide](../deployment/production.md#node-labels)).
For multi-manager ACME sharing options see
[NFS vs S3-backed shared storage](../deployment/production.md#shared-storage-nfs-vs-s3-backed).

## NFS Shared Volume

For multi-manager deployments, mount `shared` on a networked backend (NFS, EFS, Filestore) so Traefik and control-plane replicas share ACME and staging state.

## Minimum Node Labels

- `db=true`
- `builder=true`
- `registry=true`
