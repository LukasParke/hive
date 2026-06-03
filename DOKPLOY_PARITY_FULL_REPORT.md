# Dokploy vs Hive Parity Full Report

Date: 2026-03-11

## Scope

This report compares Dokploy self-hosted API/UI capability families against the current Hive implementation across:

- `control-plane/internal/api/server.go`
- `api/openapi.yaml`
- `ui/src/App.tsx`
- `ui/src/api/client.ts`
- `ui/src/pages/*`

Classification:

- `PASS`: Functionality is available and usable end-to-end in Hive.
- `PARTIAL`: Functionality exists in baseline form but is not at Dokploy depth.
- `MISSING`: Functionality is absent or not wired.

## Executive Summary

- Core self-hosted operational parity is now **strong baseline**.
- Full Dokploy parity remains **PARTIAL** due to advanced runtime operations, deep settings/platform workflows, and some API contract alignment gaps.
- Cloud/proprietary Dokploy modules remain out of core self-hosted parity scope unless explicitly included.

## API Parity Matrix

### PASS

- Auth core (`register`, `login`, `refresh`, `logout`, `me`)
- Organizations baseline (`list`, `create`, API key create)
- Projects CRUD
- Environments CRUD
- Applications CRUD + deploy + rollback + deployments
- Stacks CRUD + deploy
- Domains CRUD
- Registries CRUD + test
- Builds queue/list + cancel/retry
- Backups list/create/restore
- Backup destinations CRUD + test
- Schedules CRUD + run-now
- Database services list/create/get
- Git providers list/create
- Notifications CRUD + test
- Provider webhooks (GitHub, GitLab, Bitbucket, Gitea)
- Metrics and realtime events

### PARTIAL

- Identity extension:
  - Reset-password and invitation/member/admin endpoints are implemented, but still lighter than Dokploy lifecycle semantics.
- Runtime operation depth:
  - App/stack start-stop-restart and app logs exist, but no full Dokploy-grade process controls, queue cleanup, exec/terminal depth.
- Deployments:
  - Central deployments list/delete exists; preview deployment and richer filtering semantics are still missing.
- Settings/platform operations:
  - Server/cluster/SSH/certificate/request APIs exist, but Dokploy-grade setup/security and advanced operations remain partial.
- Data service depth:
  - Generic DB service path exists; per-engine advanced lifecycle parity is not complete.
- Compose advanced pipeline:
  - No Dokploy-level template/import/randomize/process capabilities yet.

### MISSING

- Dedicated Dokploy-equivalent API families not yet present:
  - redirects, port policies, volume backups, mounts depth, patch system, AI surfaces (if in scope), security surfaces.
- Full OpenAPI coverage for newer parity endpoints added after earlier contract baseline:
  - reset-password, invitation/member lifecycle, runtime lifecycle additions, settings sub-routes, deployments list/delete.

## UI Parity Matrix

### PASS

- Modular route shell and dashboard layout
- Auth route set (`/`, `/register`, `/reset-password`, invitation routes)
- Core route namespaces for projects, deployments, runtime, monitoring, settings, events
- Service detail routes for application/stack/database
- Runtime queue controls and baseline operational actions
- Settings baseline pages and infra page wiring

### PARTIAL

- Onboarding depth vs Dokploy first-run setup wizard
- Invitation/reset UX branch depth and edge-case handling
- Runtime operations UX:
  - Limited diagnostics/logs/terminal behavior versus Dokploy
- Settings module depth:
  - Many pages are baseline forms/summaries, not full Dokploy interaction depth
- Monitoring/requests drill-down depth
- Member/admin UI lifecycle depth (resend/revoke/default-org semantics)

### MISSING

- Full Dokploy nested route/feature depth for all service families:
  - compose and per-engine DB pages with equivalent controls
- Traefik management UX parity (file/env/middleware/webserver controls)
- Docker/swarm management depth parity
- Complete UI CRUD coverage for secrets/configs/networks/API key lifecycle
- Cloud/proprietary surfaces (billing/invoices/license/sso/whitelabeling) unless explicitly in scope

## Family-by-Family Status

| Family | Status | Notes |
|---|---|---|
| Auth core | PASS | End-to-end works |
| Reset password | PARTIAL | Implemented baseline, not Dokploy-complete UX/flows |
| Invitations + members | PARTIAL | Implemented baseline lifecycle, needs deeper semantics |
| Organization admin | PARTIAL | Members/invites/API keys baseline only |
| Projects/environments | PASS | CRUD and routing present |
| Application lifecycle | PARTIAL | Core deploy/rollback plus baseline start/stop/restart/logs |
| Stack/compose lifecycle | PARTIAL | CRUD/deploy/start/stop/restart baseline; advanced compose missing |
| DB services | PARTIAL | Generic path available; per-engine depth missing |
| Domains/registry | PASS | Core CRUD/test available |
| Builds/deployments | PARTIAL | Queue controls + baseline deployments page |
| Backups/schedules | PASS | Core workflows present |
| Notifications/git providers | PASS | CRUD/test/list/create baseline complete |
| Settings infra (servers/cluster/ssh/certs/requests) | PARTIAL | Baseline API+UI present, advanced workflows missing |
| Monitoring/realtime | PARTIAL | Baseline metrics/events present, deep dashboards missing |
| Secrets/configs/networks UI | MISSING | API exists, UI mostly absent |
| Traefik/docker/swarm deep operations | PARTIAL | Baseline visibility exists, management depth missing |
| OpenAPI parity for recent additions | PARTIAL | Needs final contract sync |

## High-Priority Remaining Work

1. **Contract closure**
   - Update `api/openapi.yaml` for all newly added parity endpoints.
   - Ensure generated clients/tests match.
2. **Identity/admin hardening**
   - Complete invitation lifecycle semantics (resend/revoke/expiration UX).
   - Expand API key lifecycle and member admin UX parity.
3. **Runtime depth**
   - Add robust app/stack/database operational controls and richer logs/exec pathways.
   - Expand deployments and queue diagnostics.
4. **Settings/platform depth**
   - Traefik/docker/swarm/server operations parity UX.
   - Harden server/cluster mutation flows.
5. **Data/compose depth**
   - Advanced compose template/import workflows.
   - Per-engine DB parity workflows.
6. **Final acceptance**
   - Full parity matrix test run + dind + production smoke + certification update.

## Out of Scope (Unless Explicitly Included)

- Dokploy cloud/proprietary modules:
  - Stripe/billing/invoices
  - Enterprise SSO/license/whitelabeling

## Conclusion

Hive now provides a substantial self-hosted parity baseline with broad API/UI coverage and deployable operational workflows. Remaining work is concentrated in advanced behavior depth and contract hardening rather than missing core primitives.
