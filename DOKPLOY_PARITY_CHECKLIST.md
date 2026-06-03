# Dokploy vs Hive Parity Checklist (Side-by-Side)

This checklist is intended for execution tracking.  
Use it together with `DOKPLOY_PARITY_FULL_REPORT.md`.

Status legend:

- `[x]` PASS
- `[~]` PARTIAL
- `[ ]` MISSING

## API Routers (Dokploy -> Hive)

| Dokploy API Family | Hive Equivalent | Status | Action Needed |
|---|---|---|---|
| auth (core) | `/api/v1/auth/*` core | [x] | none |
| auth reset-password | `/api/v1/auth/send-reset-password`, `/api/v1/auth/reset-password` | [~] | finalize semantics + OpenAPI docs |
| organization | `/api/v1/organizations*` | [~] | complete invite/member/api-key depth + docs |
| user/admin profile depth | partial in org/auth flows | [~] | add full profile/admin behavior parity |
| project | `/api/v1/projects*` | [x] | none |
| environment | `/api/v1/environments*` | [x] | none |
| application | `/api/v1/applications*` | [~] | expand runtime ops depth (exec/process/cleanup) |
| compose | `/api/v1/stacks*` | [~] | advanced compose/template/import features |
| postgres/mysql/mariadb/mongo/redis routers | `/api/v1/database-services*` generic | [~] | per-engine lifecycle parity |
| deployment | `/api/v1/builds*`, `/api/v1/deployments*` | [~] | preview deployment + richer filters/actions |
| rollback | `/api/v1/applications/{id}/rollback` | [x] | none |
| domain | `/api/v1/domains*` | [x] | none |
| registry | `/api/v1/registries*` | [x] | none |
| redirects | none | [ ] | implement redirects router/endpoints |
| port | none | [ ] | implement port policy endpoints |
| certificates | `/api/v1/settings/certificates*` baseline | [~] | attachment/workflow depth |
| mounts | none | [ ] | add mounts endpoints |
| backup | `/api/v1/backups*`, `/api/v1/backup/destinations*` | [x] | none |
| volumeBackups | none | [ ] | add volume backup APIs |
| schedule | `/api/v1/schedules*` | [x] | none |
| notification | `/api/v1/notifications*` | [x] | none |
| gitProvider/github/gitlab/gitea/bitbucket | `/api/v1/git/providers`, `/api/v1/webhooks/*` | [~] | provider setup/callback depth |
| docker/swarm | `/api/v1/services`, `/api/v1/nodes` | [~] | deeper node/app control |
| server/cluster | `/api/v1/settings/servers`, `/api/v1/settings/cluster` baseline | [~] | setup/validation/add-remove node depth |
| sshKey | `/api/v1/settings/ssh-keys*` baseline | [~] | update/delete/generate/assign workflows |
| settings | `/api/v1/settings*` + settings subroutes | [~] | traefik/webserver mutation depth |
| security | none | [ ] | implement security endpoints |
| patch | none | [ ] | implement patch APIs if in scope |
| ai | none | [ ] | implement AI APIs if in scope |

## UI Pages (Dokploy -> Hive)

| Dokploy UI Family | Hive Route/Page | Status | Action Needed |
|---|---|---|---|
| login/register | `/`, `/register` | [x] | none |
| send-reset-password/reset-password | `/reset-password` | [~] | fully match Dokploy validation/flow UX |
| invitation/accept-invitation | `/invitation`, `/accept-invitation/:id` | [~] | deepen token branch/error/retry semantics |
| projects list | `/dashboard/projects` | [x] | none |
| project/environment deep navigation | partial (`/dashboard/environments`) | [~] | full nested project->env->service route model |
| application service page | `/dashboard/services/application/:id` | [~] | full env/provider/log/terminal diagnostics UX |
| compose service page | stack detail pages | [~] | full compose editor/import/template UX |
| db service pages per engine | generic db detail | [~] | per-engine pages + controls |
| deployments page | `/dashboard/deployments` | [~] | tabs/filters/by-server/by-type parity |
| schedules page | `/dashboard/settings/schedules` + main settings | [x] | none |
| monitoring page | `/dashboard/monitoring` | [~] | charts/drill-down/range parity |
| requests page | `/dashboard/settings/infra` (request events section) | [~] | dedicated requests UX parity |
| docker page | none dedicated | [ ] | add docker management page |
| swarm page | `/dashboard/swarm` summary | [~] | deeper node/app operations |
| traefik page | none dedicated | [ ] | add full traefik management UI |
| settings profile | none dedicated | [ ] | add profile page |
| settings servers/server/cluster | `/dashboard/settings/infra` baseline | [~] | split into full Dokploy modules |
| settings ssh-keys | `/dashboard/settings/infra` baseline | [~] | full lifecycle UX |
| settings certificates | `/dashboard/settings/infra` baseline | [~] | full lifecycle + bind flows |
| settings registry | `/dashboard/settings/registries` | [~] | full create/edit/test UX depth |
| settings notifications | `/dashboard/settings/notifications` | [~] | full channel-specific forms parity |
| settings users | `/dashboard/settings/users` | [~] | full invite/member/admin parity UX |
| settings git-providers | `/dashboard/settings/git-providers` | [~] | full provider setup UX parity |
| settings destinations | `/dashboard/settings/backups` | [~] | full destination workflow parity |
| swagger page | none | [ ] | add swagger/openapi UI route if desired |
| billing/invoices/license/sso/whitelabeling | none | [ ] | out-of-scope unless explicitly included |

## Contract and Test Tracking

| Item | Status | Action Needed |
|---|---|---|
| OpenAPI coverage for all current server endpoints | [~] | sync new parity endpoints into `api/openapi.yaml` |
| UI client coverage for server endpoints | [~] | add remaining secrets/configs/networks/api-key-create calls |
| dind/runtime parity integration tests | [~] | add tests for new identity/runtime/settings paths |
| final parity certification | [~] | update PASS/PARTIAL/MISSING after closure |

## Immediate Next Execution Order

1. Close OpenAPI gaps for all newly added endpoints.
2. Complete identity/member/admin lifecycle semantics and UI polish.
3. Implement missing dedicated pages: Docker, Traefik, Profile, Requests.
4. Add advanced compose and per-engine DB lifecycle parity.
5. Re-run full validation matrix and update certification.
