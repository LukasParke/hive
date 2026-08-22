# External Services

Hive works out of the box with its bundled Postgres, registry, and Traefik.
Each can be replaced by an external service when your environment requires it.

## External PostgreSQL (Tier 3)

Tier 3 replaces the bundled database entirely with one you operate or a managed
offering (see [ADR-003](../adr/ADR-003-tiered-postgres-ha.md)).

### Configuration

The control-plane takes a single connection string:

```sh
DATABASE_URL="postgres://<user>:<password>@<host>:5432/hive?sslmode=disable"
```

- **Through PgBouncer (recommended)** — point `DATABASE_URL` at a PgBouncer in
  transaction-pool mode. The control plane opens many short-lived connections
  (API requests, River workers, LISTEN/NOTIFY fanout); pooling keeps external
  Postgres under its `max_connections`.
- **Direct** — also supported; size `max_connections` accordingly.

Notes:

- Migrations run automatically at control-plane boot: the numbered forward-only
  runner applies `internal/db/migrations/*.up.sql`, then River's own schema is
  migrated. The database user therefore needs DDL rights on first boot.
- LISTEN/NOTIFY is used for realtime fanout — the external Postgres must allow
  `LISTEN`/`NOTIFY` on the configured channel names (standard behavior).
- Remove the bundled `postgres`/`pgbouncer` services with a stack override if
  you don't want them deployed.
- Backups via Hive's backup feature target the *database services it manages*;
  for an external Postgres use the provider's native backup tooling.

## External Registries

Builds push to an internal OCI registry by default. You can instead push to any
external registry by registering it in the UI (**Settings → Registries**,
`POST /api/v1/registries`) with:

| Field | Value per provider |
|-------|--------------------|
| Docker Hub | URL `docker.io`, your Docker Hub username + password/access token |
| GitHub Container Registry | URL `ghcr.io`, username + a PAT with `write:packages` |
| Amazon ECR | URL like `<acct>.dkr.ecr.<region>.amazonaws.com`; use an IAM user's token pair as username/password (long-lived keys) since builds authenticate with static credentials |
| Google Artifact Registry / GCR | URL like `<region>-docker.pkg.dev`, `_json_key` as username + service-account JSON key as password |

### How builds authenticate

When a build runs, the control plane resolves the push target in this order:
the application's pinned registry → the organization default registry → the
internal registry. Credentials stored with the registry row are decrypted from
the encrypted secrets store (`AES-256-GCM`, keyed from the master key) and
handed to BuildKit as push credentials. They are never written to images or
logs. Passwords are encrypted **at rest only** — see
[security.md](../security.md#known-limitations).

Pulls on worker nodes: nodes pulling private images from external registries
(rolling updates, pulls outside builds) need their own credentials, e.g.
`docker login` per node or Swarm registry-auth secrets — Hive does not
distribute registry credentials to node Docker daemons.

## BYO Traefik

You may run your own Traefik (or another proxy) instead of the bundled global
Traefik service. Caveats:

- **Swarm provider network** — bundled Traefik runs with
  `--providers.swarm.network=hive_proxy`. Your proxy must attach to the
  `hive_proxy` overlay network (or override that flag), or it cannot resolve
  container IPs behind Swarm routing.
- **Application routers are label-driven** — application domains generate
  `traefik.http.*` labels on the app services. Your Traefik must enable the
  Swarm provider and honor those labels; non-Traefik proxies must translate
  labels themselves.
- **Security rules** — IP allow/blocklists, rate limiting, and header policies
  are emitted as Traefik middleware labels. A custom proxy must implement the
  equivalent enforcement itself. Country block is not supported regardless of
  proxy (see [security.md](../security.md#known-limitations)).
- **TLS/ACME** — certificate management becomes fully yours. Point DNS at your
  proxy and configure its own resolver; do not mount the Hive `shared` ACME
  store into two different Traefik operators at once.
- **Control-plane routing** — replicate the bundled setup: route to the
  `hive_control-plane` service on port 3000 with sticky sessions (cookie name
  `hive_sticky` in the default stack) so UI websockets stay pinned to one
  replica.
- **Direct fallback** — even without any proxy, the control plane publishes
  port 8080 → 3000 directly (`HIVE_DIRECT_PORT`), which keeps LAN/Cloudflare
  setups working without Traefik.
