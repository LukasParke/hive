# Core Self-Hosted Parity Certification

Date: 2026-03-11

## Verification Evidence

- `go test ./...` in `control-plane`: **PASS**
- `go test ./...` in `agent`: **PASS**
- `npm run build` in `ui`: **PASS**
- Dind runtime validation:
  - `tests/integration/scripts/start_dind_swarm.sh`: **PASS**
  - `tests/integration/scripts/seed_swarm_prereqs.sh`: **PASS**
  - `docker stack deploy ... hive-stack.yml + hive-stack.ci.yml`: **PASS**
  - `tests/integration/scripts/wait_for_stack_ready.sh`: **PASS**
  - `go test -tags integration -v ./tests/integration`: **PASS**

## Parity Matrix (Hive vs Dokploy Core Scope)

- Auth/session/org/RBAC: **PASS**
  - Go-native auth/session/org/API-key flows remain active and tested.
  - Tenant gate (`X-Organization-Id` + RBAC) now enforced on operational list/read surfaces.
- Runtime deploy correctness: **PASS**
  - End-to-end dind integration suite passes with stack deploy + API + job flows.
  - Build/deploy/rollback/webhook integration tests all green in the same run.
- Domains/TLS routing + tenancy: **PASS**
  - Domain listing now tenant-scoped through app/project/org joins.
  - Domain application/reconcile path remains in place for Traefik label consistency.
- Compose/stacks + webhook deploy triggers: **PASS**
  - Stack deploy logic now includes additional compose semantics (ports/networks/global mode).
  - Webhook signature and queued deploy behavior validated by integration suite.
- Data services + backup/restore: **PASS (core)**
  - DB service provisioning now enforces project/org ownership and includes network/secret wiring.
  - Backup runner moved from synthetic payloads to engine-aware command execution for database backup/restore (Postgres path).
- Realtime + notifications + UI operational UX: **PASS**
  - WebSocket hub hardened with ping/pong keepalive, disconnect cleanup, and reconnecting UI client behavior.
  - Notification dispatcher hardened with timeout + retry-on-transient-failure behavior.
- CI/contract hardening: **PASS**
  - API contract workflow now resolves `base_ref` correctly and runs oasdiff against repo-scoped temp artifacts.
  - Stack readiness script now evaluates only target stack services, avoiding false negatives from app services created during tests.

## Notes

- Scope: core self-hosted parity and roadmap exit criteria closure.
- Remaining deeper enhancements (non-blocking for this certification) are iterative quality improvements, not critical/high parity blockers.

## Full Alignment Phase Execution (Current Run)

- Phase 1 foundation + onboarding/API stabilization: **PASS**
  - Added CRUD parity endpoints for projects, applications, domains, registries, notifications, and backup destinations.
  - UI client expanded with compatibility methods for new CRUD operations and improved onboarding/runtime surfaces.
- Phase 2 runtime operations: **PASS (baseline)**
  - Added stack/database-service detail APIs and build queue endpoints (`list/cancel/retry`).
  - UI runtime section now exposes queue inspection and queue action controls.
- Phase 3 platform operations: **PASS (baseline)**
  - Added schedule APIs (`list/create/update/delete/run`) and destination/channel test endpoints.
  - Added `009_schedules` migration and settings UI controls for schedule/test operations.
- Phase 4 advanced model alignment: **PASS (baseline)**
  - Added environment abstraction API (`/environments` CRUD) with `010_environments` migration.
  - Added provider-specific webhook routes for Bitbucket and Gitea, including signature verification support.
- Phase 5 hardening + production acceptance: **PASS**
  - Local gates: `go test ./...` (`control-plane`, `agent`) and `npm run build` (`ui`) all pass.
  - Production verification on `10.10.10.51`: stack redeployed, API/ UI healthy, environment + schedule endpoints validated end-to-end.

## Residual Gaps (Post-Run)

- Full Dokploy UI pixel/interaction parity is still partial; current Hive UI remains a parity-oriented operations shell.
- Advanced multi-server/cluster and SSH-key workflows are still baseline-level compared to Dokploy’s deeper operational modules.

## Redeploy + UI Parity Execution (This Run)

- Phase 0 redeploy to `10.10.10.51`: **PASS**
  - Rebuilt `control-plane`, `agent`, and `ui` images from current source and redeployed the `hive` stack.
  - Verified runtime health (`/api/v1/health`) and UI availability on `:8080`.
- Phase 1 route/layout parity: **PASS (structural)**
  - Reworked UI into modular shell + page components with compatibility redirects for legacy paths.
  - Added Dokploy-style route namespaces under `/dashboard/*`.
- Phase 2 onboarding/auth parity: **PASS (baseline)**
  - Added dedicated auth routes (`/`, `/register`, `/reset-password`, invitation routes).
  - Preserved first-run organization/project bootstrap and duplicate-conflict handling.
- Phase 3 service operations UI: **PASS (baseline)**
  - Added dedicated service detail pages for applications, stacks, and database services.
  - Preserved deploy/rollback/history + build queue actions.
- Phase 4 settings/platform parity: **PASS (baseline)**
  - Added settings sub-routes for notifications, registries, backup destinations, schedules, and git providers.
  - Added environment hierarchy view and monitoring route based on metrics API.
- Phase 5 API/UI contract closure: **PASS (for migrated surfaces)**
  - Confirmed migrated UI pages map to existing control-plane endpoints and OpenAPI contract paths.
  - Added metrics client integration for monitoring route coverage.
- Phase 6 validation/cutover: **PASS**
  - `go test ./...` (`control-plane`, `agent`) and `npm run build` (`ui`) all pass.
  - Final deployment smoke checks on `10.10.10.51` are green.

## Final Status Classification

- `PASS`: Core deploy/runtime/auth/org/onboarding shell, operational routing, builds/runtime controls, settings CRUD/test surfaces, environments, monitoring baseline.
- `PARTIAL`: Full Dokploy pixel-level UI parity, deep invitation/reset workflows, and advanced multi-server UX.
- `NOT IN SCOPE (cloud/proprietary)`: Dokploy cloud/proprietary modules and hosted-only control-plane capabilities.

## Dokploy Remaining Parity Matrix Execution (Current Run)

- Identity/member/invitation/password-reset baseline: **PASS (baseline)**
  - Added reset-password token issue/reset APIs and UI wiring for reset flow.
  - Added organization invitation token fetch/accept APIs and invitation acceptance UI flow.
  - Added organization member/invitation/API-key lifecycle APIs and initial admin UI surface.
- Runtime operations depth baseline: **PASS (baseline)**
  - Added app/stack start/stop/restart APIs and application log history endpoint.
  - Added centralized deployments list/delete APIs and deployments UI page.
- Settings/platform infra baseline: **PASS (baseline)**
  - Added settings APIs for servers, cluster info, ssh keys, certificates, and request events.
  - Added dedicated infra settings UI route with create/list operations.
- Data/compose + CRUD closure baseline: **PASS (baseline)**
  - Expanded UI client with missing create/update/delete methods for domains/registries/stacks/backup destinations/git providers/notifications.
  - Added quick-create controls for these resources in settings UI.
- Validation + redeploy: **PASS**
  - Local gates: `go test ./...` (`control-plane`) and `npm run build` (`ui`) pass.
  - Remote redeploy on `10.10.10.51` successful; API health and UI route checks pass.

## Residual Delta After This Run

- Full Dokploy interaction/pixel parity remains **PARTIAL**.
- Advanced compose template pipeline, per-engine DB deep operations, and full server/cluster mutation workflows remain **PARTIAL**.
- Cloud/proprietary modules remain **NOT IN SCOPE**.
