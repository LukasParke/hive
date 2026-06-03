# Storage Model

Hive storage is split by lifecycle:

- `ephemeral-local`: temporary build workspaces and caches (`/data/local`)
- `shared-durable`: ACME state and backup staging (`/data/shared`)
- `database-secrets`: encrypted values in `secrets_store`
- `service-volumes`: Swarm volumes for Postgres, registry, and Buildkit caches

## NFS Shared Volume

For multi-manager deployments, mount `shared` on a networked backend (NFS, EFS, Filestore) so Traefik and control-plane replicas share ACME and staging state.

## Minimum Node Labels

- `db=true`
- `builder=true`
- `registry=true`
