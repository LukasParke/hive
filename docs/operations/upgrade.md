# Upgrade Guide

## Preferred path: `hivectl update`

```sh
./hivectl update            # latest GitHub release (falls back to prompt if unreachable)
./hivectl update <tag>      # specific tag, e.g. ./hivectl update 0.4.0
HIVE_IMAGE_TAG=<tag> ./hivectl update
```

What `hivectl update` does:

1. Reads the currently running image tags of `control-plane` and `agent`.
2. Resolves the target tag (`$1` argument, else `HIVE_IMAGE_TAG`, else the
   latest GitHub release).
3. No-ops if already running that version.
4. Re-deploys the stack with `docker stack deploy -c deploy/hive-stack.yml`,
   preserving the host-management setting and re-checking stateful placement
   labels.
5. Waits for both services to reach their desired replica count (agent:
   desired = node count) and reports per-service rollout status.

## Manual path

```sh
export HIVE_IMAGE_TAG=<tag>
docker pull ghcr.io/lukasparke/hive/control-plane:${HIVE_IMAGE_TAG}
docker pull ghcr.io/lukasparke/hive/agent:${HIVE_IMAGE_TAG}
docker stack deploy -c deploy/hive-stack.yml hive
```

Swarm performs a rolling update with `start-first` ordering; failed health
checks trigger automatic rollback to the previous revision.

If host management is enabled, keep it enabled for manual deploys:

```sh
HIVE_HOST_MGMT=true HIVE_IMAGE_TAG=<tag> docker stack deploy -c deploy/hive-stack.yml hive
```

(`./hivectl update` preserves the current setting automatically.)

## Migration Behavior

Migrations run automatically at **control-plane boot** — never by hand:

1. The forward-only runner applies every `internal/db/migrations/*.up.sql`
   not yet recorded in `schema_migrations`, in lexical order. There are no
   down migrations executed at runtime; `.down.sql` files exist for local
   development only.
2. River's queue schema is then migrated up via `rivermigrate` — this also
   happens at boot, before workers start.

Consequences:

- On a rolling update, new control-plane replicas migrate the schema *before*
  old replicas finish draining. Migrations must therefore be backward-
  compatible with the previous release's code (they are, in released tags).
- Do not scale control-plane to zero mid-upgrade; let Swarm roll it.

## Downgrades Are NOT Supported

Because the migration runner is forward-only, you cannot move an upgraded
database back to an older Hive version: older binaries will not understand a
newer schema. To "roll back" an upgrade:

- Use Swarm's automatic rollback (previous revision) **only** if no new
  migrations have applied yet.
- Otherwise restore from your pre-upgrade Postgres backup and deploy the old
  tag. This loses writes since the upgrade — which is why you should always
  take a verified backup before upgrading.

## Durability Notes

Upgrades must not remove Docker volumes or secrets. `docker stack deploy`
updates services in place and preserves `hive_pgdata`, `hive_registry-data`,
`hive_shared`, and the `hive-*`/`agent-bootstrap-token` secrets.

Do not use `./hivectl uninstall --purge`, `docker volume rm`, or
`docker volume prune` during an upgrade. On multi-node Swarm clusters, Postgres
and the registry use node-local named volumes by default, so keep `db=true`
and `registry=true` on the nodes that already hold those volumes or use an
external/shared volume driver ([production.md](../deployment/production.md)).

## Post-Upgrade Verification

```sh
curl -fsS https://<domain>/api/v1/health
curl -fsS https://<domain>/api/v1/ready
docker service ps hive_control-plane hive_agent   # all Running, same new tag
```

Then smoke-test one application deploy or terminal session to confirm the
CP→agent mTLS path survived the rollout. If agents fail to reconnect, see the
[cert renewal playbook](runbook.md#agent-certificate-renewal-failures).

For self-service updates triggered from the UI (self-updater), the same
rolling-update semantics apply — see
[architecture overview](../architecture/overview.md#self-update).
