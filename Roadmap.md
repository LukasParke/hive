# Hive Swarm-Native Migration Roadmap

This document is the canonical migration roadmap for rebuilding Dokploy as a Swarm-native platform in `hive`.

## Objectives

- Swarm-first architecture for all runtime operations.
- PostgreSQL with sqlc for durable state and typed data access.
- OpenAPI 3.1 contract as the single API source of truth.
- Go control-plane plus Go node agent with mTLS.
- Buildkit + OCI registry for distributed builds.
- Traefik in Swarm provider mode with label-driven routing.

## Phases

### Phase A (Weeks 1-3): Foundation

- Create ADRs.
- Define `api/openapi.yaml`.
- Add CI quality gates.
- Bootstrap repository layout.

### Phase B (Weeks 4-7): Data Core

- Implement Postgres migrations.
- Add sqlc query layer.
- Add advisory locks and LISTEN/NOTIFY.
- Introduce River job processing.

### Phase C (Weeks 7-9): Secrets and Storage

- Implement encrypted secret storage.
- Separate local ephemeral vs shared durable storage.
- Add migration utility for legacy file-based secrets.

### Phase D (Weeks 9-14): Swarm Orchestration

- Implement Engine and Swarm clients in Go.
- Add stack deploy support (compose-on-swarm).
- Wire OpenAPI handlers to orchestration services.
- Add swarm state cache and reconciliation.

### Phase E (Weeks 14-17): Build Pipeline

- Deploy registry and Buildkit in stack.
- Implement Build and Deploy workers.
- Support real-time build/deploy log streaming.

### Phase F (Weeks 17-19): Networking and TLS

- Configure Traefik swarm provider mode.
- Implement domain label manager.
- Implement overlay network lifecycle and API support.

### Phase G (Weeks 19-23): Agent and Real-Time

- Define agent protobuf API.
- Implement node-local agent for exec/stats/logs.
- Implement internal CA and CSR signing.
- Implement terminal and log websocket bridges.

### Phase H (Weeks 23-26): HA Control Plane

- Add leader election via Postgres advisory lock.
- Add robust health/readiness endpoints.
- Finalize bootstrap flow and production stack manifests.

### Phase I (Weeks 26-30): UI, Testing, and GA

- Build React UI using generated OpenAPI client.
- Complete integration test matrix on Swarm dind.
- Finalize operations/deployment/upgrade documentation.

## Exit Criteria

- All API endpoints in `api/openapi.yaml` implemented and tested.
- Cluster lifecycle is fully Swarm-native.
- HA failover scenarios pass in CI.
- Bootstrap and upgrade flows are documented and reproducible.
import { useState, useMemo } from "react";

const P = [
  {
    id: "p0", phase: "Phase 0 — Contract, Governance & CI Foundation", dur: "Weeks 1–3",
    goal: "Establish the API contract, quality gates, decision records, and CI infrastructure that every subsequent phase depends on. No runtime code changes yet.",
    steps: [
      { id: "0.1", t: "Write Architecture Decision Records (ADRs)", time: "Week 1",
        b: `Create /docs/adr/ in the repo. Write and get consensus on these before any code:

ADR-001: Swarm-First Architecture — Docker Swarm is THE deployment target. Standalone Docker is a degenerate case (1-node Swarm). All design decisions optimize for multi-node Swarm.

ADR-002: PostgreSQL as Primary Datastore — Migrate from SQLite to PostgreSQL. Drizzle ORM retains its role during transition; sqlc (Go) becomes the target data access layer.

ADR-003: Tiered Postgres HA Strategy — Three tiers: (1) single Postgres + networked volume (default), (2) Patroni + etcd (production HA), (3) managed external Postgres. Application code is tier-agnostic via connection string through PgBouncer.

ADR-004: Registry Requirement for Multi-Node — A container image registry is mandatory. Bundled as distribution/distribution in the default stack. External registries supported via configuration.

ADR-005: Go Agent on Every Node — Lightweight Go agent as a global Swarm service. Handles exec, stats, logs for local containers. Communicates via ConnectRPC over mTLS.

ADR-006: Build Pipeline Architecture — Builds offloaded to Buildkit running as a Swarm service on worker nodes. Results pushed to registry. Control plane never runs docker build locally.

ADR-007: OpenAPI 3.1 as API Contract — External API defined in OpenAPI 3.1 spec. Server code generated via oapi-codegen (Go). TypeScript client types via openapi-typescript. tRPC phased out.

ADR-008: Real-Time Strategy — Postgres LISTEN/NOTIFY for discrete state-change events. Direct streaming (Swarm logs API, agent ConnectRPC) for continuous data. WebSocket with sticky sessions for browser clients.` },
      { id: "0.2", t: "Define the OpenAPI 3.1 specification", time: "Weeks 1–2",
        b: `Create /api/openapi.yaml as the source of truth. Resource structure with standard CRUD + action endpoints:

/api/v1/auth — login, refresh, logout (JWT)
/api/v1/users — CRUD (admin)
/api/v1/projects — CRUD with contained applications
/api/v1/applications — CRUD + deploy, rollback, restart, deployments history, logs (WS), terminal (WS), metrics
/api/v1/services — Direct Swarm service management, full ServiceSpec CRUD + task listing + logs
/api/v1/stacks — Compose-on-Swarm: deploy, update, remove, per-service redeploy
/api/v1/nodes — List, label, drain, promote, demote, remove
/api/v1/secrets — Swarm secret CRUD + rotate (metadata only, no value return)
/api/v1/configs — Swarm config CRUD (including data)
/api/v1/networks — Overlay network CRUD + attached services view
/api/v1/registries — Registry config CRUD + connectivity test
/api/v1/domains — Domain-to-application mapping + certificate status
/api/v1/builds — Build history, logs, cancel, retry
/api/v1/cluster — Cluster health overview + SSE event stream
/api/v1/settings — Global settings CRUD

Component schemas mirror Docker API types: ServiceSpec, TaskSpec, ContainerSpec, Resources, Placement, UpdateConfig, RollbackConfig, EndpointSpec, PortConfig, Mount, SecretReference, ConfigReference, NetworkAttachmentConfig, HealthCheck, RestartPolicy, ServiceMode, NodeSpec.

Every endpoint includes: request/response schema, error responses (400-503), examples, and description.` },
      { id: "0.3", t: "Set up CI pipeline and quality gates", time: "Week 2",
        b: `GitHub Actions workflows gating every PR:

Workflow 1 — API Spec Validation: Spectral linting (custom ruleset), breaking change detection (oasdiff), Go type generation (oapi-codegen), TypeScript type generation (openapi-typescript). Fail on lint errors or generation failures.

Workflow 2 — Go Build & Test: go vet, staticcheck, golangci-lint (errcheck, gosec, gocritic, revive), go test -race with coverage gate (start 60%, ratchet up). Fail on lint errors, test failures, race conditions.

Workflow 3 — Integration Tests (Swarm): Start 3-node dind Swarm cluster (1 manager, 2 workers). Deploy Dokploy stack. Run full test suite: service CRUD, stack deploy, secret/config lifecycle, node operations, build pipeline, failover. 15-minute timeout.

Workflow 4 — Database Migrations: Start Postgres via testcontainers. Apply all migrations forward, verify schema (sqlc compile), apply all backward (rollback), re-apply forward (idempotency). Fail on migration errors.

Workflow 5 — Frontend Build: Type check (tsc), lint (eslint), build, verify generated API client types match spec.` },
      { id: "0.4", t: "Establish the repository structure", time: "Weeks 2–3",
        b: `/api/openapi.yaml — Source of truth
/agent/ — Go agent binary (cmd/, internal/docker, exec, metrics, auth, server, proto/)
/control-plane/ — Go API server (cmd/, internal/api, swarm, build, domain, auth, realtime, leader, jobs, ca, db/)
  /internal/db/migrations/ — SQL migration files
  /internal/db/queries/ — sqlc SQL query files
  /internal/db/generated/ — sqlc output (DO NOT EDIT)
/ui/ — React frontend (src/api generated client, components, pages, hooks)
/deploy/ — dokploy-stack.yml, patroni-stack.yml, monitoring-stack.yml, init.sh
/images/postgres-patroni/ — Custom Patroni image (Dockerfile, patroni.yml.template, entrypoint.sh)
/docs/adr/ — Architecture Decision Records
/tests/integration/ — Go integration tests (dind Swarm)
/tests/e2e/ — End-to-end browser tests
Makefile, docker-bake.hcl, .github/workflows/` }
    ]
  },
  {
    id: "p1", phase: "Phase 1 — Data Layer: Postgres, sqlc, River", dur: "Weeks 4–7",
    goal: "Replace SQLite with PostgreSQL. Establish the data access layer (sqlc), job queue (River), distributed locking (advisory locks), and event bus (LISTEN/NOTIFY).",
    steps: [
      { id: "1.1", t: "Audit and design the Postgres schema", time: "Week 4",
        b: `Walk through Dokploy's existing SQLite schema. For each table document: current SQLite DDL, target Postgres DDL, type changes (TEXT→JSONB, INTEGER→BOOLEAN, TEXT dates→TIMESTAMPTZ, AUTOINCREMENT→GENERATED ALWAYS AS IDENTITY), data transformation needed.

Key new/modified tables:

applications — Add service_spec JSONB (full Docker ServiceSpec as desired state), source_type ENUM ('git','image','compose'), registry_id FK. Keep denormalized columns for fast listing.

build_jobs (NEW) — id UUID PK, application_id FK, status CHECK ('queued','building','pushing','deploying','complete','failed','cancelled'), trigger CHECK ('webhook','manual','api','rollback'), git_ref, git_sha, build_strategy, image_tag, logs TEXT, started_at/completed_at TIMESTAMPTZ, worker_id, error_message.

swarm_cache_services (NEW) — swarm_id TEXT PK, name, spec JSONB, status JSONB, updated_at. Similar tables for swarm_cache_tasks, swarm_cache_nodes.

secrets_store (NEW) — id UUID PK, name UNIQUE, type CHECK ('ssh_key','tls_cert','tls_key','signing_key','ca_key','ca_cert'), encrypted_value BYTEA (AES-256-GCM), timestamps.

audit_log (NEW) — id BIGINT GENERATED, user_id FK, action TEXT, resource_type, resource_id, details JSONB, ip_address INET, created_at. INDEX on (resource_type, resource_id, created_at).` },
      { id: "1.2", t: "Write SQL migrations and sqlc queries", time: "Weeks 4–5",
        b: `Migrations in /control-plane/internal/db/migrations/ as numbered .up.sql/.down.sql pairs. Tested in CI: apply all up, verify schema, apply all down, re-apply forward.

sqlc queries in /control-plane/internal/db/queries/ organized by domain:

applications.sql — GetApplication :one, ListApplicationsByProject :many, CreateApplication :one, UpdateApplicationSpec :exec
build_jobs.sql — EnqueueBuild :one, ClaimBuild :one (using FOR UPDATE SKIP LOCKED), CompleteBuild :exec, FailBuild :exec
Plus: users.sql, projects.sql, domains.sql, settings.sql, audit.sql, swarm_cache.sql, secrets_store.sql

sqlc.yaml config: engine postgresql, sql_package pgx/v5, emit_json_tags true, emit_prepared_queries true, JSONB overridden to json.RawMessage.

Run sqlc generate → type-safe Go functions for every query. These are the ONLY database access functions used. No hand-written SQL at runtime.` },
      { id: "1.3", t: "Implement Postgres connection layer", time: "Week 5",
        b: `pgxpool for connection management (through PgBouncer). MaxConns=20 per replica, MinConns=5, HealthCheckPeriod=30s.

Separate persistent connection (not pooled) for LISTEN/NOTIFY: stays open, receives all NOTIFY events. On drop (Postgres failover), reconnect with exponential backoff, re-issue all LISTEN commands.

Advisory lock helpers: TryAcquireLock, ReleaseLock, AcquireSessionLock. Lock ID constants: LockLeaderElection=1, LockBootstrap=2, LockCertRenewal=3.` },
      { id: "1.4", t: "Set up River job queue", time: "Weeks 5–6",
        b: `River runs inside the control plane process, using the same Postgres pool. No external broker.

Job types: BuildJob (ApplicationID, GitRef, Trigger), DeployJob (ApplicationID, ImageTag, ServiceSpec), CertRenewalJob (DomainID), BackupJob (TargetType, TargetID), CleanupJob.

Worker config: BuildJob max 2 workers (Buildkit concurrency), DeployJob max 5, others max 1. Retry: BuildJob 2 retries/30s backoff, DeployJob 3 retries/10s. Uniqueness: BuildJob unique by (ApplicationID, GitRef) while queued/building.

Periodic jobs (leader only runs scheduler, all replicas run workers): CleanupJob every 24h, CertRenewalJob every 1h.` },
      { id: "1.5", t: "Implement LISTEN/NOTIFY event fanout", time: "Week 6",
        b: `Fanout struct manages subscriptions: map of channel name → subscriber channels. Run() loop: WaitForNotification → dispatch to subscribers. Drop messages for slow subscribers (they reconcile on next full refresh).

Channels: deployment:{app_id}, service:{service_id}, system (node joined/left, leader changed).

Emit NOTIFY in transactions using sqlc custom query: SELECT pg_notify(@channel, @payload). Called within business logic transactions so events are atomic with state changes.

NOTIFY is for discrete state changes (small payloads, <8KB). NOT for data streams (logs, metrics). Log streams go directly from Swarm API/agents to the WebSocket replica.` },
      { id: "1.6", t: "Deploy Postgres in the Swarm stack", time: "Weeks 6–7",
        b: `dokploy-stack.yml: postgres service (postgres:16-alpine, replicas 1, placement node.labels.db==true, health check pg_isready, persistent volume pgdata, on dokploy_internal network). PgBouncer service (transaction mode, MAX_CLIENT_CONN=200, DEFAULT_POOL_SIZE=20, on dokploy_internal).

init.sh bootstrap script: pre-check Swarm active, generate secrets (postgres-password, dokploy-master-key, replication-password via openssl rand), label current node (db=true, builder=true, registry=true), docker stack deploy, poll health endpoint until ready.

patroni-stack.yml (Tier 2 overlay): 3x Postgres+Patroni, 3x etcd on managers, PgBouncer configured for Patroni primary discovery. Separate file — users compose with base stack.` }
    ]
  },
  {
    id: "p2", phase: "Phase 2 — Filesystem Decoupling & Secrets Migration", dur: "Weeks 7–9",
    goal: "Eliminate all hard dependencies on the local filesystem. Secrets to encrypted DB, large shared files to shared volumes, ephemeral files stay local.",
    steps: [
      { id: "2.1", t: "Audit and categorize every filesystem path", time: "Week 7",
        b: `Grep all file I/O operations. Categorize:

EPHEMERAL (stays local at /data/local/): build working dirs, temp files, nixpacks/buildpacks cache.

SHARED-DURABLE-SMALL (migrates to Postgres secrets_store): SSH keys, TLS private keys, JWT signing key, webhook secrets, internal CA key pair.

SHARED-DURABLE-LARGE (shared volume at /data/shared/): Traefik ACME state (acme.json), backup staging before S3 upload, registry storage (if filesystem backend).

NODE-LOCAL-FIXED: /var/run/docker.sock — no change needed.` },
      { id: "2.2", t: "Implement the encrypted secrets store", time: "Weeks 7–8",
        b: `AES-256-GCM encryption with per-record random nonce. Master key from Swarm secret (DOKPLOY_MASTER_KEY → /run/secrets/dokploy-master-key). Key derivation via HKDF with unique context per secret type.

Store.Put(): generate nonce, encrypt, prepend nonce to ciphertext, upsert to DB.
Store.Get(): read from DB, split nonce/ciphertext, decrypt.
Store.MaterializeToFile(): decrypt to tmpfs path with 0600 permissions for tools requiring file paths (ssh2, buildctl). Caller responsible for cleanup.

Migration tool: reads existing key files from /data/.ssh, /data/certs, encrypts, stores in Postgres, removes originals. Run once during upgrade.` },
      { id: "2.3", t: "Configure shared volume mounts in stack file", time: "Weeks 8–9",
        b: `Volume definition with NFS driver_opts (users set NFS_SERVER and NFS_PATH, or replace with their storage driver).

Mounts: Traefik gets shared:/data/shared (for acme.json). Control plane gets shared:/data/shared (backup staging) + tmpfs /data/local:size=2G (ephemeral scratch). Registry gets its own persistent volume.

Document NFS setup for: Ubuntu/Debian, RHEL/CentOS, Synology NAS, AWS EFS, GCP Filestore. Document alternative: S3 for backups + single Traefik instance eliminates shared volume requirement entirely.` }
    ]
  },
  {
    id: "p3", phase: "Phase 3 — Docker Abstraction & Swarm API Coverage", dur: "Weeks 9–14",
    goal: "Build the complete Docker interaction layer in Go: EngineClient for local ops, SwarmClient for cluster orchestration. 100% Swarm API coverage. Largest phase.",
    steps: [
      { id: "3.1", t: "Implement EngineClient (local Docker ops)", time: "Weeks 9–10",
        b: `Wraps Docker Go SDK (github.com/docker/docker/client) for LOCAL engine operations. Shared Go module imported by both agent and control plane.

Image ops: BuildImage (context, dockerfile, tags, buildArgs → stream), PushImage, PullImage, TagImage.
Container ops (agent use): ExecCreate, ExecAttach, ContainerStats, ContainerLogs.
Cleanup: PruneImages, PruneContainers, PruneVolumes.

Each method wraps the corresponding Docker SDK call with error handling, context propagation, and structured logging.` },
      { id: "3.2", t: "Implement SwarmClient (Swarm orchestrator ops)", time: "Weeks 10–12",
        b: `Wraps Docker Go SDK for Swarm-specific operations. Only works on manager nodes.

Service CRUD: CreateService, UpdateService, RemoveService, GetService, ListServices, ServiceLogs.
Tasks: ListTasks, GetTask.
Nodes: ListNodes, GetNode, UpdateNode, RemoveNode.
Secrets: CreateSecret, ListSecrets, GetSecret, RemoveSecret, UpdateSecret.
Configs: CreateConfig, ListConfigs, GetConfig, RemoveConfig, UpdateConfig.
Networks: CreateNetwork, ListNetworks, RemoveNetwork.
Events: Events (Swarm event stream for leader's event watcher).

SpecBuilder for full ServiceSpec construction — covers EVERY field: ContainerSpec (image, cmd, args, env, mounts, secrets, configs, hosts, dns, healthcheck, init, capabilities, ulimits, stop_signal, stop_grace_period), Resources (limits + reservations including GPUs), Placement (constraints, preferences, max_replicas), UpdateConfig, RollbackConfig, EndpointSpec (vip/dnsrr, ports), Mode (replicated/global/replicated-job/global-job), LogDriver, RestartPolicy, Labels (service-level + container-level separately).` },
      { id: "3.3", t: "Implement Stack Deploy (Compose-on-Swarm)", time: "Weeks 12–13",
        b: `Uses compose-go (github.com/compose-spec/compose-go/v2) to parse Compose files, then translates to Swarm API calls.

DeployStack flow: (1) Parse with loader.LoadWithContext, (2) Create/update overlay networks (prefixed {stackname}_{netname}), (3) Create/update secrets, (4) Create/update configs, (5) For each service: convert to ServiceSpec via composeServiceToSwarmSpec + create/update, (6) Prune removed services.

Compose 'deploy' key translation: replicas→Mode.Replicated, resources→TaskTemplate.Resources, placement.constraints→Placement, update_config→UpdateConfig, rollback_config→RollbackConfig, restart_policy→RestartPolicy, endpoint_mode→EndpointSpec.Mode, deploy labels→ServiceSpec.Labels, non-deploy labels→ContainerSpec.Labels.

Stack labels: com.docker.stack.namespace on both service and container level.

PreviewStackDeploy: returns diff of what would change (create/update/remove) before user confirms.` },
      { id: "3.4", t: "Implement Secrets, Configs, Node management", time: "Weeks 13–14",
        b: `API handlers bridge OpenAPI spec to SwarmClient.

Secret rotation workflow: create new secret → list all referencing services → update each service's SecretReferences to new ID → remove old secret → audit log.

Node management: listing with cached task counts/resource usage, label CRUD (key for placement constraints: disk=ssd, zone=us-east-1a, gpu=true), drain (availability=drain, Swarm reschedules tasks), promote/demote (role change), removal with pre-checks.

Swarm event watcher (leader singleton): subscribes to docker system events (scope=swarm), dispatches by type (service/node/secret/config/network), updates swarm_cache tables, fires NOTIFY. Keeps DB cache in sync with Swarm state for fast UI rendering.` },
      { id: "3.5", t: "Implement the API server (oapi-codegen)", time: "Week 14",
        b: `Generate Go server: oapi-codegen -package api -generate types,chi-server → produces ServerInterface with one method per endpoint.

Server struct holds all dependencies: db queries, pool, SwarmClient, EngineClient, secrets store, fanout, River client, CA authority.

Example handler: PostApplicationsIdDeploy → validate → enqueue BuildJob/DeployJob/StackDeployJob based on source_type → respond 202 Accepted immediately (deployment is async).

Mount: Chi router with Logger/Recoverer/auth middleware, API handlers, static UI files (React build), /api/health, /api/ready endpoints. Serve on :3000, Traefik routes external traffic.` }
    ]
  },
  {
    id: "p4", phase: "Phase 4 — Build Pipeline: Buildkit, Registry, Queue", dur: "Weeks 14–17",
    goal: "Images built by Buildkit, pushed to registry, deployed via Swarm service update. No local docker build anywhere.",
    steps: [
      { id: "4.1", t: "Deploy registry as a Swarm service", time: "Weeks 14–15",
        b: `registry:2 in dokploy-stack.yml: replicas 1, placement node.labels.registry==true, persistent volume, on dokploy_internal + dokploy_proxy (if external access needed), health check on /v2/.

RegistryConfig model: URL, Username, Password (in secrets_store), IsDefault. Connectivity test via /v2/ endpoint.

Image naming convention: {registry}/{project}/{application}:{git-sha-short}. Enforced in build pipeline — control plane generates tag, passes to Buildkit, stores in build_jobs.

External registry support: Docker Hub, GHCR, ECR, GCR — just configure URL + credentials.` },
      { id: "4.2", t: "Deploy Buildkit as a Swarm service", time: "Week 15",
        b: `moby/buildkit:latest in dokploy-stack.yml: replicas 1, placement node.labels.builder==true, 4G memory limit / 2G reservation, persistent cache volume, on dokploy_internal, entrypoint buildkitd --addr tcp://0.0.0.0:1234.

Control plane connects via Go client (github.com/moby/buildkit/client) at tcp://buildkit:1234. Registry auth passed via session auth provider.

Multi-builder: increase replicas for high build volume, each on different worker nodes with builder=true label.` },
      { id: "4.3", t: "Implement the build worker (River job handler)", time: "Weeks 15–17",
        b: `BuildWorker River handler: (1) Update status to 'building' + NOTIFY, (2) git.Clone to /data/local/builds/{jobID}, (3) Determine strategy (Dockerfile direct / Nixpacks generate / Buildpacks via pack CLI), (4) Build with Buildkit SolveOpt (frontend dockerfile.v0, exports to image with push=true, session auth for registry), (5) Stream build logs to DB for real-time UI, (6) On success: update build record with image tag + NOTIFY, (7) Enqueue DeployJob.

DeployWorker: load app + spec, find existing Swarm service by label dokploy.app.id, create if first deploy or update with new image tag (triggers Swarm rolling update per UpdateConfig).

Build log streaming: Buildkit SolveStatus channel → buffer → periodic flush to DB. UI subscribes via WebSocket → fanout → NOTIFY signals progress, UI fetches latest logs from builds API.` }
    ]
  },
  {
    id: "p5", phase: "Phase 5 — Traefik Swarm Mode, Domains & Certs", dur: "Weeks 17–19",
    goal: "Switch routing from file-provider to Swarm service labels. Domains and TLS work natively across the cluster.",
    steps: [
      { id: "5.1", t: "Configure Traefik for Swarm-native mode", time: "Week 17",
        b: `Traefik v3 in dokploy-stack.yml: global service on managers, ports 80/443 in host mode (real client IP, no double NAT), Docker socket read-only.

Command flags: --providers.swarm=true, --providers.swarm.exposedByDefault=false, --providers.swarm.network=dokploy_proxy, entrypoints web (80) + websecure (443), HTTP→HTTPS redirect, ACME cert resolver with storage on shared volume.

Install swarm-refresh plugin for reliable label change detection. Host mode means each Traefik instance binds directly — Swarm's ingress mesh routes any-node requests to a manager running Traefik.` },
      { id: "5.2", t: "Implement domain-to-label management", time: "Weeks 17–18",
        b: `DomainManager.ApplyDomain(): find Swarm service by dokploy.app.id label, set Traefik labels: traefik.enable=true, traefik.http.routers.{name}.rule=Host(), .entrypoints=websecure, .tls=true, .tls.certresolver=letsencrypt, traefik.http.services.{name}.loadbalancer.server.port={port}. Update service atomically via Swarm API.

Remove domain: strip the corresponding traefik.http.routers.* and .services.* labels.

Multi-domain: multiple routers on same service with different Host rules. Wildcard: HostRegexp rules + DNS challenge for wildcard TLS certs.` },
      { id: "5.3", t: "Implement overlay network management", time: "Weeks 18–19",
        b: `Automatic per-project network creation: dokploy_project_{slug} as overlay, attachable=false, internal=false, labeled with dokploy.project.id.

Service network attachment: project network (for inter-service communication, service name as DNS alias) + dokploy_proxy (if domain configured, for external traffic via Traefik). Both set in ServiceSpec.TaskTemplate.Networks.

Network CRUD via API: create/delete overlay networks with options (encrypted, attachable, internal, subnet/gateway). UI shows topology: which services connect to which networks.` }
    ]
  },
  {
    id: "p6", phase: "Phase 6 — Agent, Real-Time & Observability", dur: "Weeks 19–23",
    goal: "Build the Go agent. Terminal exec, log streaming, metrics collection, WebSocket fanout. Full UX parity across the cluster.",
    steps: [
      { id: "6.1", t: "Define agent protobuf contract", time: "Week 19",
        b: `agent/v1/agent.proto with ConnectRPC:

ExecStream (bidirectional): ExecInput has oneof: ExecStart (container_id, command, tty, rows, cols), stdin bytes, ResizeTerminal. ExecOutput has oneof: stdout bytes, stderr bytes, exit_code int32.

GetContainerStats (unary): StatsRequest (container_ids, empty=all) → StatsResponse with ContainerStats (cpu_percent, memory_usage/limit, network_rx/tx, block_read/write).

StreamContainerLogs (server streaming): LogRequest (container_id, follow, tail, timestamps) → stream of LogChunk (data, is_stderr, timestamp).

Health (unary): → HealthResponse (agent_version, docker_version, node_id, hostname, total_memory, cpu_count, disk_total/used).

Generate Go code via buf generate → agentv1connect package.` },
      { id: "6.2", t: "Implement the agent binary", time: "Weeks 19–21",
        b: `Single Go binary, ~2500 lines. main.go: load config from env, init Docker EngineClient (local socket), setup mTLS (load CA cert from Swarm config, own cert from registration), register with control plane if first boot (CSR flow), start ConnectRPC server on :9090 with mTLS, expose /metrics (Prometheus), serve.

ExecStream handler: receive ExecStart → engine.ExecCreate → engine.ExecAttach → bridge bidirectional: goroutine 1 reads Docker stdout/stderr → sends ExecOutput, goroutine 2 receives ExecInput stdin/resize → writes to Docker connection. Send exit code on completion.

Dockerfile: multi-stage, CGO_ENABLED=0, scratch base, ~15MB image. Deploy as global Swarm service, Docker socket read-only, CA cert via Swarm config, bootstrap token via Swarm secret, on dokploy_internal network.` },
      { id: "6.3", t: "Implement internal CA for agent mTLS", time: "Week 20",
        b: `Authority struct manages a private CA. On first boot: generate ECDSA P-256 key pair, self-sign CA cert (10yr), store encrypted in secrets_store DB, create Swarm config for CA cert distribution to agents.

SignAgentCSR: parse CSR, create short-lived cert (72h), set CN=agent-{nodeID}, sign with CA key, return PEM. Agents auto-renew before expiry.

Agent registration endpoint (internal): POST /internal/agent/register with CSR + node_id + bootstrap_token. Validates bootstrap token (Swarm secret), signs CSR, returns certificate.

No static API keys, no shared secrets to rotate. CA is single trust anchor in Swarm's encrypted Raft log.` },
      { id: "6.4", t: "Implement WebSocket hub and terminal proxy", time: "Weeks 21–22",
        b: `WSHub manages browser WebSocket connections, bridges to data sources.

Terminal flow: browser → WS upgrade → find task's node (Swarm API: GET /tasks/{id} → NodeID) → connect to agent via ConnectRPC (agentDialer.DialNode) → open bidirectional ExecStream → bridge: WS recv → ExecInput stdin, ExecOutput stdout → WS send.

Service log flow: browser → WS upgrade → Swarm service logs API (GET /services/{id}/logs?follow=true) on manager socket → stream chunks to WS. No agent involvement — Swarm aggregates logs from all tasks natively.

Event subscription flow: browser → WS upgrade → fanout.Subscribe("deployment:{appID}") → forward NOTIFY events as JSON to WS.

Sticky sessions: Traefik label on control plane service: traefik.http.services.control-plane.loadbalancer.sticky.cookie.name=dokploy_sticky. Keeps WS on same replica for session duration.` },
      { id: "6.5", t: "Integrate metrics (cAdvisor + Prometheus)", time: "Weeks 22–23",
        b: `Tier A (built-in, no extra infra): control plane queries agents on-demand when user views metrics. Agent calls docker stats locally, returns results. Simple, no persistence.

Tier B (production, optional): monitoring-stack.yml deploys Prometheus (replicated on manager), Grafana (with Traefik domain), cAdvisor (global, all nodes). Prometheus scrapes cAdvisor (tasks.cadvisor:8080), agent /metrics (tasks.agent:9090), control plane /metrics (:3000).

Ship pre-built Grafana dashboards: Cluster Overview, Per-Service metrics, Build Pipeline stats, Control Plane Health. Alerting rules: replication lag, standby count, service task failures, resource exhaustion, cert expiry.

Control plane and agent expose /metrics via prometheus/client_golang: API latency, WS connections, job queue depth, Swarm API call duration.` }
    ]
  },
  {
    id: "p7", phase: "Phase 7 — HA Control Plane & Self-Deployment", dur: "Weeks 23–26",
    goal: "Control plane as replicated Swarm service. Leader election, health checks, zero-downtime updates, automatic rollback. Dokploy manages itself.",
    steps: [
      { id: "7.1", t: "Implement leader election", time: "Week 23",
        b: `Elector uses session-level Postgres advisory lock. Run loop: acquire connection → pg_try_advisory_lock(LockLeaderElection) → if acquired: set isLeader=true, start singleton tasks (Swarm event watcher, periodic job scheduler, state reconciler) in cancellable context. Hold lock by keeping connection alive with periodic SELECT 1. On connection loss: cancel context, set isLeader=false, stop singletons, retry after 15s.

If leader crashes, Postgres connection closes, lock auto-releases. Another replica acquires within 15 seconds. Safe: advisory lock is on Postgres primary (single source of truth), no split-brain possible.

Leader enqueues work (periodic jobs into River), but ALL replicas process work (River's SKIP LOCKED distributes jobs).` },
      { id: "7.2", t: "Implement health and readiness endpoints", time: "Weeks 23–24",
        b: `/api/health (Swarm health check, must be fast): check database connectivity (pool.Ping, 2s timeout) + Docker socket (swarm.Info). Return 200 if all ok, 503 with details if any unhealthy.

/api/ready (load balancer readiness): health checks + initialization complete (migrations applied, caches warm, connections pooled). Returns 503 during startup.

Swarm health check config: wget -q --spider http://localhost:3000/api/health, interval 10s, timeout 5s, retries 3, start_period 30s.` },
      { id: "7.3", t: "Implement first-boot initialization", time: "Week 24",
        b: `main.go startup sequence: connect Postgres pool → acquire LockBootstrap advisory lock (race protection between replicas) → if acquired: run migrations, check if first boot (no settings record) → if first boot: generate admin user with random password, initialize internal CA, create settings record → release lock. If lock not acquired: wait for settings record to appear (another replica is bootstrapping).

Initialize all subsystems: secrets store, EngineClient, SwarmClient, CA authority, fanout, River client, API server. Set initialized=true. Start background: fanout.Run, leader elector, River workers. Serve HTTP on :3000.

Adding nodes: docker swarm join on new node → agent global service auto-deploys → agent registers with control plane → node appears in UI via leader's event watcher.` },
      { id: "7.4", t: "Finalize the canonical Swarm stack file", time: "Weeks 24–25",
        b: `Complete dokploy-stack.yml with all services:

control-plane: replicas 3, managers only, max 1 per node, start-first updates with rollback, 1G memory, Traefik labels (domain routing + sticky sessions), DATABASE_URL through PgBouncer, master-key secret, Docker socket + shared volume + tmpfs, health check.

agent: global, 256M memory, Docker socket ro, CA cert config, bootstrap token secret, internal network.

traefik: global on managers, ports 80/443 host mode, Docker socket ro, shared volume, Swarm provider config.

postgres: replicas 1, db=true label, 1G memory, password secret, persistent volume, internal network.
pgbouncer: replicas 1, manager, transaction mode.
buildkit: replicas 1, builder=true label, 4G/2G memory, cache volume.
registry: replicas 1, registry=true label, persistent volume.

Networks: dokploy_internal (overlay, encrypted) + dokploy_proxy (overlay, attachable).
Volumes: pgdata, shared, buildkit-cache, registry-data.
Secrets: dokploy-master-key, postgres-password, agent-bootstrap-token (all external: true).` },
      { id: "7.5", t: "Write bootstrap script and upgrade process", time: "Weeks 25–26",
        b: `init.sh: pre-check Swarm active, prompt for DOKPLOY_DOMAIN + ACME_EMAIL, generate secrets (openssl rand), label current node (db, builder, registry), export env vars, docker stack deploy, poll /api/health until ready, print URL + join command for workers.

Upgrade process: pull new images → update tag in stack file → docker stack deploy (idempotent redeploy) → Swarm does rolling update with start-first + auto rollback on health failure → new replicas run pending migrations on startup (with advisory lock) → agent updates follow same pattern (global service rolling update, one node at a time).

Self-update via UI (future): detect new Docker Hub tags → "Update available" banner → user clicks → control plane updates its own Swarm service image tag → rolling update handles the rest.` }
    ]
  },
  {
    id: "p8", phase: "Phase 8 — Frontend, Documentation & GA", dur: "Weeks 26–30",
    goal: "React UI against OpenAPI-generated TypeScript client. Comprehensive docs. Integration tests. Ship.",
    steps: [
      { id: "8.1", t: "Build the React frontend", time: "Weeks 26–28",
        b: `Vite + React SPA served as static files by control plane. TypeScript API client generated from OpenAPI spec (openapi-typescript + openapi-fetch).

Key views: Dashboard (cluster overview, recent deploys, alert banners). Projects → Project detail (applications, shared settings). Application detail (status, deployment history + logs, domains + TLS, ServiceSpec editor with Advanced toggle, terminal WS, logs WS, metrics). Swarm management (Nodes with label editor + drain/promote, Services raw list, Stacks with compose editor + deploy preview diff, Secrets with rotate, Configs CRUD, Networks topology visualization). Settings (registries, Postgres tier, monitoring config, users, backup config).

All data fetching via generated TypeScript client. WebSocket with auto-reconnect (exponential backoff + full state reconciliation on reconnect).` },
      { id: "8.2", t: "Write all documentation", time: "Weeks 28–29",
        b: `In /docs/, published as static site:

Architecture Overview: component diagram, communication paths, data flow diagrams for deploy/build/terminal/logs.

Deployment Guide: prerequisites, quick start (single-node), production (3 managers + N workers + NFS), Patroni HA overlay, external Postgres, external registry. Cloud-specific: AWS (EBS, EFS, ECR), GCP, DigitalOcean, Hetzner.

Operations Runbook: scaling, node maintenance, backup/restore/PITR, disaster recovery (total cluster loss, Postgres loss, registry loss), cert management, troubleshooting decision tree.

Upgrade Guide: rolling upgrade procedure, breaking change migrations, rollback, Postgres tier upgrades.

API Reference: Scalar from OpenAPI spec at /docs/api, interactive try-it-out, auth guide, webhook config.

Security Hardening: encrypted overlay, mTLS internals, secret rotation, audit logging, least privilege.` },
      { id: "8.3", t: "Build the integration test suite", time: "Weeks 29–30",
        b: `Go tests against real dind Swarm cluster in CI. Each test is independent (setup + teardown own state). testcontainers-go for Postgres, dind cluster for Swarm.

Test scenarios (each 5min timeout, full suite ~25min):
TestClusterBootstrap — deploy stack, verify all services Running, health 200, admin exists, agents registered.
TestApplicationGitDeploy — create project+app, trigger deploy, verify build completes, image in registry, Swarm service running.
TestApplicationRollback — deploy v1, deploy v2, rollback, verify image tag is v1.
TestStackDeploy — multi-service compose, verify services+networks, update compose, verify changes, remove stack.
TestDomainRouting — create app+service, add domain, verify Traefik labels on Swarm service.
TestSecretLifecycle — create secret, attach to service, verify mounted, rotate, verify updated.
TestControlPlaneHA — verify 3 replicas, identify leader, kill leader, verify new election, verify API continues.
TestNodeDrain — deploy 3-replica service, drain worker, verify reschedule, undrain.
TestBuildkitRecovery — kill Buildkit, trigger build, restart Buildkit, verify build completes (River retry).
TestPostgresFailover (Tier 2) — Patroni cluster, kill primary, verify failover <30s, verify API resumes, verify no data loss.` },
      { id: "8.4", t: "Release engineering and GA", time: "Week 30",
        b: `Release artifacts: Docker images (control-plane, agent, postgres-patroni) tagged v1.0.0. Stack files. init.sh. Documentation site.

Release process: Git tag → CI builds all images + runs full integration suite + pushes to Docker Hub/GHCR → builds docs site → conventional commit release notes → GitHub Release with stack files attached.

Semver for control plane + agent. Stack files include matching image tags. Breaking API changes require major version bump.

Post-GA priorities: self-update UI, CLI tool (generated from OpenAPI spec), GitHub/GitLab webhook auto-configuration, plugin/extension system for custom build strategies.` }
    ]
  }
];

const colors = ["#8b5cf6","#ec4899","#f59e0b","#3b82f6","#10b981","#06b6d4","#f97316","#6366f1","#84cc16"];

const techStack = [
  ["Control Plane", "Go, oapi-codegen, Chi, pgx, sqlc, River"],
  ["Agent", "Go, ConnectRPC, Docker Go SDK, ~15MB"],
  ["Database", "PostgreSQL 16, PgBouncer, Patroni (opt)"],
  ["API Contract", "OpenAPI 3.1, Spectral, Scalar docs"],
  ["Build Pipeline", "Buildkit (Swarm svc), Nixpacks, Buildpacks"],
  ["Registry", "distribution/distribution (OCI)"],
  ["Proxy", "Traefik v3, Swarm provider, ACME"],
  ["Frontend", "React, TypeScript, openapi-fetch"],
  ["Compose Parsing", "compose-go/v2 (official Docker)"],
  ["Monitoring", "Prometheus, Grafana, cAdvisor (opt)"],
  ["Internal Auth", "mTLS, ECDSA P-256, Internal CA"],
  ["CI/Testing", "GitHub Actions, dind Swarm, testcontainers-go"],
];

export default function App() {
  const [ep, setEp] = useState(null);
  const [es, setEs] = useState(null);
  const totalSteps = useMemo(() => P.reduce((s, p) => s + p.steps.length, 0), []);

  return (
    <div style={{ fontFamily: "system-ui, -apple-system, sans-serif", background: "#0f1117", color: "#e2e8f0", minHeight: "100vh", padding: "20px" }}>
      <div style={{ maxWidth: 900, margin: "0 auto" }}>
        <h1 style={{ fontSize: 21, fontWeight: 700, marginBottom: 2, color: "#f1f5f9" }}>Dokploy Swarm-Native — Master Implementation Plan</h1>
        <p style={{ fontSize: 13, color: "#94a3b8", marginBottom: 4 }}>{P.length} phases, {totalSteps} steps, ~30 weeks</p>
        <p style={{ fontSize: 12, color: "#64748b", marginBottom: 20 }}>Click a phase to expand, then click any step for full implementation detail.</p>

        <div style={{ display: "flex", gap: 2, marginBottom: 20, height: 7, borderRadius: 4, overflow: "hidden" }}>
          {P.map((p, i) => (
            <div key={p.id} style={{ flex: p.steps.length, background: colors[i], opacity: ep === p.id ? 1 : 0.3, cursor: "pointer", transition: "opacity 0.15s" }}
              onClick={() => { setEp(ep === p.id ? null : p.id); setEs(null); }} />
          ))}
        </div>

        {P.map((phase, pi) => {
          const open = ep === phase.id;
          const c = colors[pi];
          return (
            <div key={phase.id} style={{ marginBottom: 8 }}>
              <div onClick={() => { setEp(open ? null : phase.id); setEs(null); }}
                style={{ background: open ? "#1a1d27" : "#13151d", border: "1px solid", borderColor: open ? c : "#2a2d3a",
                  borderRadius: open ? "8px 8px 0 0" : 8, padding: "12px 16px", cursor: "pointer", transition: "all 0.15s" }}>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                      <div style={{ width: 7, height: 7, borderRadius: "50%", background: c, flexShrink: 0 }} />
                      <span style={{ fontSize: 13.5, fontWeight: 600, color: "#f1f5f9" }}>{phase.phase}</span>
                    </div>
                    {!open && <p style={{ fontSize: 12, color: "#64748b", margin: "5px 0 0 15px", lineHeight: 1.4 }}>{phase.goal}</p>}
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 8, flexShrink: 0, marginLeft: 10 }}>
                    <span style={{ fontSize: 11, color: "#64748b" }}>{phase.dur}</span>
                    <span style={{ fontSize: 11, color: "#64748b", background: "#1e2130", padding: "1px 7px", borderRadius: 4 }}>{phase.steps.length}</span>
                    <span style={{ color: "#64748b", fontSize: 15, transform: open ? "rotate(180deg)" : "none", transition: "transform 0.15s" }}>▾</span>
                  </div>
                </div>
              </div>
              {open && (
                <div style={{ background: "#1a1d27", border: "1px solid", borderColor: c, borderTop: "none", borderRadius: "0 0 8px 8px" }}>
                  <div style={{ padding: "10px 16px 12px", borderBottom: "1px solid #2a2d3a" }}>
                    <p style={{ fontSize: 12.5, color: "#94a3b8", lineHeight: 1.5, margin: 0 }}><span style={{ color: c, fontWeight: 600 }}>Goal: </span>{phase.goal}</p>
                  </div>
                  {phase.steps.map((step, si) => {
                    const sk = `${phase.id}-${si}`;
                    const so = es === sk;
                    return (
                      <div key={si}>
                        <div onClick={(e) => { e.stopPropagation(); setEs(so ? null : sk); }}
                          style={{ padding: "10px 16px", cursor: "pointer", background: so ? "rgba(255,255,255,0.02)" : "transparent",
                            borderBottom: si < phase.steps.length - 1 || so ? "1px solid #2a2d3a" : "none" }}>
                          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
                            <div style={{ display: "flex", gap: 8, alignItems: "flex-start" }}>
                              <span style={{ color: c, fontSize: 11.5, fontWeight: 600, minWidth: 24, marginTop: 1 }}>{step.id}</span>
                              <span style={{ fontSize: 13, fontWeight: 500, color: "#e2e8f0" }}>{step.t}</span>
                            </div>
                            <div style={{ display: "flex", alignItems: "center", gap: 6, flexShrink: 0, marginLeft: 10 }}>
                              <span style={{ fontSize: 11, color: "#64748b" }}>{step.time}</span>
                              <span style={{ color: "#64748b", fontSize: 13, transform: so ? "rotate(180deg)" : "none", transition: "transform 0.15s" }}>▾</span>
                            </div>
                          </div>
                        </div>
                        {so && (
                          <div style={{ padding: "0 16px 14px 48px", background: "rgba(255,255,255,0.01)" }}>
                            <div style={{ fontSize: 12.5, color: "#cbd5e1", lineHeight: 1.65, whiteSpace: "pre-wrap" }}>{step.b}</div>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          );
        })}

        <div style={{ marginTop: 28, background: "#1a1d27", border: "1px solid #2a2d3a", borderRadius: 8, padding: "16px 18px" }}>
          <h3 style={{ fontSize: 13, fontWeight: 600, marginBottom: 10, color: "#f1f5f9" }}>Technology Stack</h3>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "3px 20px", fontSize: 12, color: "#94a3b8", lineHeight: 1.7 }}>
            {techStack.map(([k, v], i) => (
              <div key={i}><span style={{ color: "#e2e8f0", fontWeight: 500 }}>{k}:</span> {v}</div>
            ))}
          </div>
        </div>

        <div style={{ marginTop: 12, background: "#1a1d27", border: "1px solid #2a2d3a", borderRadius: 8, padding: "16px 18px" }}>
          <h3 style={{ fontSize: 13, fontWeight: 600, marginBottom: 8, color: "#f1f5f9" }}>Critical Path</h3>
          <div style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 4, marginBottom: 8 }}>
            {P.map((p, i) => (
              <span key={p.id} style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                <span style={{ background: colors[i], color: "#fff", fontSize: 10, padding: "1px 7px", borderRadius: 3, fontWeight: 600 }}>P{i}</span>
                {i < P.length - 1 && <span style={{ color: "#475569", fontSize: 11 }}>→</span>}
              </span>
            ))}
          </div>
          <p style={{ fontSize: 12, color: "#94a3b8", lineHeight: 1.6, margin: 0 }}>
            Phases are sequentially dependent. P0–P1 strictly sequential. P2 can start late in P1. P3 is longest (6 weeks) and the core. P4–P5 can partially overlap with different owners. P6 depends on P3 (agent needs SwarmClient). P7 depends on all prior phases. P8 runs continuously but has a final push at GA.
          </p>
        </div>
      </div>
    </div>
  );
}