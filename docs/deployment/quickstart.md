# Quickstart

## Prerequisites

- Docker Engine 24+ with Swarm mode initialized
- One manager node with ports 80/443/3000 open
- A domain pointing to the manager IP
- `openssl` for secret generation

## Bootstrap

1. Clone the repository and navigate to the project root.

2. Set environment variables:

```sh
export HIVE_DOMAIN=example.com
export ACME_EMAIL=ops@example.com
# Optional: pin to a specific release instead of latest
# export HIVE_IMAGE_TAG=v0.1.0
```

3. Run the bootstrap script:

```sh
./deploy/init.sh
```

This script will:
- Verify Swarm is active
- Generate required secrets (`hive-master-key`, `postgres-password`, `agent-bootstrap-token`)
- Label the current node with `db=true`, `builder=true`, `registry=true`
- Deploy the Hive stack
- Poll the health endpoint until the control plane is ready

4. Access the UI at `https://example.com`.

## First Login

On first boot, the control plane creates an admin user. Check the control plane logs for the auto-generated admin password:

```sh
docker service logs hive_control-plane --tail 50
```

## Scale Managers (Production)

1. Add additional manager nodes:

```sh
docker swarm join --token <manager-token> <manager-ip>:2377
```

2. Scale the control plane to 3 replicas:

```sh
docker service scale hive_control-plane=3
```

3. Verify all replicas are healthy:

```sh
curl https://example.com/api/v1/health
```

## Stack Services

The default `hive-stack.yml` deploys:

| Service | Replicas | Purpose |
|---------|----------|---------|
| `control-plane` | 3 (managers) | API server + static UI |
| `agent` | global (all nodes) | Node-local exec/stats/logs |
| `traefik` | global (managers) | Swarm-native reverse proxy |
| `postgres` | 1 (db label) | Primary datastore |
| `pgbouncer` | 1 (managers) | Connection pooling |
| `buildkit` | 1 (builder label) | Image builds |
| `registry` | 1 (registry label) | OCI image registry |

## Upgrade

To upgrade to a new version:

```sh
export HIVE_IMAGE_TAG=v0.1.0
docker pull ghcr.io/hive/control-plane:${HIVE_IMAGE_TAG}
docker pull ghcr.io/hive/agent:${HIVE_IMAGE_TAG}
docker stack deploy -c deploy/hive-stack.yml hive
```

Swarm performs a rolling update with `start-first` and auto-rollback on health failure.

For reproducible deployments, always set `HIVE_IMAGE_TAG` to a specific semver tag rather than `latest`.
