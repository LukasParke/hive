# Dokploy to Hive Compatibility Matrix

| Dokploy feature | Hive target |
|---|---|
| tRPC routers | OpenAPI handlers in `control-plane/internal/api` |
| Dockerode + CLI hybrid deploy | Swarm API clients in `control-plane/internal/swarm` |
| Redis/BullMQ deployment queue | Postgres-backed workers in `control-plane/internal/jobs` |
| File-based secrets | Encrypted DB store in `control-plane/internal/secrets` |
| Traefik file provider | Service-label routing via `control-plane/internal/proxy` |
| Node shell/ssh assumptions | Agent mTLS control channel in `agent/` |
