# Upgrade Guide

1. Pull new images for `control-plane` and `agent`.
2. Update image tags in `deploy/dokploy-stack.yml`.
3. Re-deploy stack:

```sh
docker stack deploy -c deploy/dokploy-stack.yml hive
```

4. Validate `/api/v1/health` and `/api/v1/ready`.
5. If rollout fails, Swarm rollback policy returns previous revision.
