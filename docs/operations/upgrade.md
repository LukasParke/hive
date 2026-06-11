# Upgrade Guide

Preferred path:

```sh
./hivectl update latest
```

Manual path:

1. Pull new images for `control-plane` and `agent`.
2. Set `HIVE_IMAGE_TAG` to the target tag.
3. Re-deploy stack:

```sh
HIVE_IMAGE_TAG=<tag> docker stack deploy -c deploy/hive-stack.yml hive
```

4. Validate `/api/v1/health` and `/api/v1/ready`.
5. If rollout fails, Swarm rollback policy returns previous revision.

## Durability notes

Upgrades must not remove Docker volumes or secrets. `docker stack deploy` updates services in place and preserves `hive_pgdata`, `hive_registry-data`, `hive_shared`, and the `hive-*`/`agent-bootstrap-token` secrets.

Do not use `./hivectl uninstall --purge`, `docker volume rm`, or `docker volume prune` during an upgrade. On multi-node Swarm clusters, Postgres and the registry use node-local named volumes by default, so keep `db=true` and `registry=true` on the nodes that already hold those volumes or use an external/shared volume driver.

## Host management during upgrades

If host management is enabled, keep it enabled during future manual deploys:

```sh
HIVE_HOST_MGMT=true HIVE_IMAGE_TAG=<tag> docker stack deploy -c deploy/hive-stack.yml hive
```

`./hivectl update` preserves the current host-management setting automatically.
