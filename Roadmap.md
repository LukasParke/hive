# Hive Swarm-Native Platform Roadmap

This document tracks Hive's phased development as a self-hosted Docker Swarm
platform. It records **what shipped, what deliberately deviated from the
original plan, and what remains open**. The original interactive plan
(9 phases / ~30 weeks) is preserved in git history (`Roadmap.md` before this
revision).

## Objectives

- Swarm-first architecture for all runtime operations.
- PostgreSQL with sqlc for durable state and typed data access.
- OpenAPI 3.1 contract as the single API source of truth.
- Go control plane plus Go node agent over mTLS.
- BuildKit + OCI registry for distributed builds.
- Traefik in Swarm provider mode with label-driven routing.

## Phase Status

| Phase | Scope | Status |
|-------|-------|--------|
| P0 | Contract, governance & CI foundation | **Done** |
| P1 | Data layer: Postgres, sqlc, River | **Done** |
| P2 | Filesystem decoupling & secrets migration | **Done** |
| P3 | Docker abstraction & Swarm API coverage | **Done** (documented deviations below) |
| P4 | Build pipeline: BuildKit, registry, queue | **Core done; extra build strategies deferred** |
| P5 | Traefik Swarm mode, domains & certs | **Done** (wildcard domains + DNS-01 + Cloudflare Tunnels supported) |
| P6 | Agent, real-time & observability | **Done** (mTLS enforced by default) |
| P7 | HA control plane & self-deployment | **Done** |
| P8 | Frontend, documentation & GA | **Done** |

### P0 — Contract, Governance & CI

- ADR-001…ADR-008 written and accepted (`docs/adr/`).
- `api/openapi.yaml` v0.4.1: every implemented route specified; spectral
  clean; breaking-change detection via oasdiff in CI.
- Workflows: API contract (spectral + TS/Go generation checks + embedded-spec
  drift check + oasdiff), Go build/test/lint (**gating**, golangci-lint v2
  with errcheck/gosec/gocritic/revive/staticcheck), migrations
  (up→down→up idempotency + sqlc drift), 3-node dind Swarm integration,
  UI build + eslint, Playwright e2e, images (control-plane, agent,
  postgres-patroni), nightly + tagged releases.

### P1 — Data Layer

- 21 numbered migrations (forward-only runner; down files verified in CI),
  40+ tables including `swarm_cache_*` mirrors and `audit_log` with the
  composite resource index.
- sqlc (pgx/v5) with JSONB → `json.RawMessage` overrides; generated queries
  are the primary access path.
- pgxpool through PgBouncer (transaction mode); the dedicated LISTEN/NOTIFY
  connection bypasses PgBouncer via `HIVE_LISTEN_URL` (transaction pooling
  cannot carry session-level LISTEN) and reconnects with jittered backoff.
- River is the only job execution path: Build/Deploy/Backup/CertRenewal/
  Cleanup/Preview workers, dedicated queues (build ×2, deploy ×5, default ×1),
  retry policies, DB-enforced single-active-build-per-application, periodic
  jobs started only under leader election.

### P2 — Secrets & Filesystem Decoupling

- AES-256-GCM + HKDF store wired into the runtime (`secrets.Runtime()`):
  CA material, control-plane client certs, registry passwords, SSH keys and
  certificate private keys are sealed at rest; legacy plaintext values pass
  through until rewritten.
- `cmd/migrate-secrets` upgrades legacy file-based installs.
- Ephemeral scratch on tmpfs (`/data/local`); shared volume for Traefik ACME
  state. NFS/S3 driver choices documented in `docs/deployment/production.md`
  (default stack ships a local named volume).

### P3 — Swarm Coverage & API

- `swarm.Client` covers services/tasks/nodes/secrets/configs/networks CRUD +
  events + logs; `spec.Builder` maps the full ServiceSpec surface (resources
  incl. GPUs, placement, update/rollback config, endpoint modes, log driver,
  healthchecks, mounts, hosts/dns/capabilities/ulimits, secrets/configs refs).
- Stack deploy parses Compose via **compose-go v2** (official loader),
  translates networks/secrets/configs/services incl. `deploy.*` keys, prunes
  removed services, and offers a create/update/remove preview diff.
- Applications attach their project overlay plus `hive_proxy` when a domain
  exists — Traefik can always reach backends.
- Node admin ops (labels merge, drain, promote/demote, remove with
  last-manager protection), secret rotation workflow (versioned successor →
  re-point services → remove old), full secrets/configs/networks CRUD.
- Leader-run watcher subscribes to docker events, mirrors cluster state into
  `swarm_cache_*`, emits NOTIFY on `system` / `service:{id}` /
  `deployment:{appID}`, and reconciles domain labels; 5-minute resync loop.

### P4 — Build Pipeline

- Registry (healthchecked) as a Swarm service; **BuildKit runs as a
  privileged host-level container per builder node** (a swarm service cannot
  obtain `CAP_SYS_ADMIN`, which buildkitd's snapshotter requires — see the
  note in ADR-006). Builds resolve credentials from configured registries and
  push `{registry}/{project}/{app}:{sha}`.
- River BuildWorker streams real BuildKit SolveStatus logs into
  `build_jobs.logs` (throttled flush) and hands off to DeployWorker;
  `GET /api/v1/builds/{id}/logs` serves them.

### P5 — Networking & Domains

- Traefik v3 global on managers, host-mode 80/443, HTTP→HTTPS redirect,
  ACME on shared storage, sticky cookie for WebSockets.
- Domain manager applies/strips router labels atomically; multi-domain works.
- **Wildcard domains are supported end-to-end:** `*.zone` hostnames route via
  Traefik `HostRegexp` and TLS uses ACME DNS-01 (`HIVE_ACME_DNS_PROVIDER` +
  `HIVE_ACME_DNS_TOKEN` in hivectl/init.sh; Cloudflare first-class). The
  swarm-refresh plugin remains omitted (Traefik v3 provider handles label
  updates).
- **Cloudflare Tunnels are natively integrated** (`/api/v1/tunnels`): Hive
  provisions the tunnel via the Cloudflare API, stores credentials encrypted,
  deploys a cloudflared connector service, publishes proxied CNAME routes
  (wildcards included), and tears everything down on delete.

### P6 — Agent & Real-Time

- ConnectRPC contract exactly as planned (+ host-management RPCs beyond it).
- Internal CA persisted in the encrypted store, published to agents as the
  `hive-agent-ca` Swarm config; agents bootstrap via token+CSR, get 72h
  certificates, auto-renew; **mTLS (TLS 1.3, client cert required) is enabled
  by default in `hive-stack.yml`**; the control plane presents its own
  certificate, renewed hourly by the leader.
- WebSocket hub proxies terminal (browser → control plane → node agent),
  container logs, and NOTIFY events; sticky sessions via Traefik cookie.
- Tier-B monitoring stack ships Prometheus rules/alerts and four curated
  Grafana dashboards (cluster, services, build pipeline, control-plane health)
  built on metrics that exist in the codebase.

### P7 — HA Control Plane

- Advisory-lock elector on a dedicated connection with keepalive ownership
  probes; losing leadership cancels singleton context and retries after 15s.
- First-boot bootstrap wrapped in `LockBootstrap` race protection with
  settings seeding; River periodic scheduling and the cluster watcher run
  only under leadership.
- `/api/v1/health` + `/api/v1/ready`; stack runs control-plane ×3
  (start-first + rollback), agent global, stateful services pinned by labels.
- `hivectl install/update/uninstall/status/logs/host-management` prints join
  commands and preserves stateful placement across updates; self-updater
  polls GitHub releases and performs zero-downtime rolling updates.

### P8 — UI, Tests, Docs, Release

- 30-page React dashboard (projects, applications, stacks, databases,
  previews, runtime/nodes, terminal/logs/metrics, security, settings, orgs);
  TypeScript types generated from the spec; Scalar API reference served at
  `/docs/api` with the spec at `/api/v1/openapi.yaml`.
- Integration suite (dind): bootstrap, git deploy, rollback queue flow, stack
  lifecycle, multi-service stacks, database provisioning, auth/org/RBAC,
  webhooks, domain endpoints, secret/config/network CRUD **plus** node drain
  rescheduling, BuildKit recovery via River retries, real leader-failover
  (kill the lock holder, verify re-election + continuity), end-to-end secret
  rotation, and network-attachment regression coverage. Patroni tier-2
  failover runs as a scheduled/dispatched workflow.
- Docs set: architecture overview, quickstart, production guide, Patroni HA,
  external services, cloud notes, security hardening, runbook, storage model,
  upgrade guide, API reference.
- Releases: GHCR images (semver/major.minor/sha/latest/nightly), tagged
  release flow gated on the dind integration suite, artifacts attached.

## Deliberate Deviations From The Original Plan

| Plan said | Shipped instead | Why |
|-----------|-----------------|-----|
| oapi-codegen-generated chi server | Hand-written chi router; CI validates the spec stays generable and generates the TS client | Handler code predates generation setup; hand-written routes allow org/RBAC middleware the generated server would complicate. Contract drift is prevented by CI reconciliation. |
| `/users` CRUD group | Organizations + members + invitations + API keys | Product outgrew flat users during development; RBAC supersedes it. |
| SSE cluster stream | WebSocket `/ws/events` | Single transport for events/logs/terminal simplifies sticky-session story. |
| Generated admin password on first boot | Self-service registration of the first admin | Simpler UX; bootstrap still race-protected and seeded. |
| EngineClient image ops in control plane | Builds exclusively via BuildKit (ADR-006) | Control plane never builds locally; agent keeps container exec/stats/logs ops. |
| compose-go "preview diff before user confirms" | `PreviewStackDeploy` diff available via API/UI | Interactive confirm flow not wired into deploy endpoint yet. |
| BuildKit as a swarm service | Privileged host-level container per builder node (`hivectl`/`init.sh` manage it) | Swarm services cannot get `CAP_SYS_ADMIN`; buildkitd's snapshotter needs mount(2). Discovered by the BuildkitRecovery integration test. |
| Nixpacks/Buildpacks build strategies | Dockerfile strategy only | Deferred; extension point isolated in the build worker. |
| Wildcard domains via DNS-01 | Rejected at validation | Requires per-provider DNS credentials; tracked as future work. |
| Scratch-based ~15MB agent image | Alpine-based image (~25MB) | Needs ca-certificates + util-linux for host management. |
| openapi-fetch client in UI | Generated types + hand-written fetch wrapper | Wrapper predates codegen; types stay spec-synced via CI. |

## Known Limitations

- IP blocklists and country blocking require an external Traefik plugin/WAF;
  both rule types are rejected at creation (the built-in approximation was
  unsafe). See `docs/security.md`.
- Downgrades are not supported; upgrades are forward-only.
- Grafana dashboards cover metrics that exist today; River queue-depth and
  HTTP-latency panels await custom collectors in the control plane.
- Postgres tier-2 failover verification runs scheduled, not per-PR.
