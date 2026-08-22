# HTTP API

The Hive control plane exposes a JSON REST API under `/api/v1`, plus
WebSocket endpoints for realtime streams. The contract is OpenAPI 3.1,
version **0.4.0**, maintained at [`api/openapi.yaml`](../api/openapi.yaml) in
the repository root — it is the source of truth and what the typed UI client
is generated from.

## Spec hosting

There is currently no route that serves the spec from a running control plane
(no `/api/v1/openapi.json` or bundled Scalar/Swagger UI). Interactive spec
hosting is tracked on the roadmap. To explore the API today, open
`api/openapi.yaml` from the repo, or render it locally:

```sh
npx @redocly/cli preview-docs api/openapi.yaml
```

## Authentication

Two mechanisms, both via the `Authorization` header
(`internal/api/middleware/auth.go`):

- **JWT bearer** — issued by the login endpoints. Use for user-driven tooling:
  ```sh
  curl -H "Authorization: Bearer <jwt>" https://hive.example.com/api/v1/projects
  ```
- **API keys** — organization-scoped keys created via
  `POST /api/v1/organizations/{id}/api-keys`. Same header, same scheme:
  ```sh
  curl -H "Authorization: Bearer <api-key>" ... -H "X-Organization-Id: <org-id>"
  ```

Unauthenticated endpoints are limited to health/readiness, webhook receivers
(github/gitlab/bitbucket/gitea), invitation lookups, and auth itself.
Rate limits apply to the public endpoints (auth: 10 req/min; webhooks: a
separate bucket).

## Error Envelope

Every error response is a JSON object with two fields
(`common.WriteError`):

```json
{ "error": "not_found", "message": "project not found" }
```

- `error` — stable machine-readable code.
- `message` — human-readable detail.

HTTP statuses follow standard semantics (401 unauthenticated, 403 wrong role,
404, 409 conflicts, 422 validation).

## WebSocket Endpoints

| Endpoint | Auth | Purpose |
|----------|------|---------|
| `GET /api/v1/ws/events` | none | Global event stream (LISTEN/NOTIFY fanout over channels `system`, `deployment:{appID}`, `service:{serviceID}`) |
| `GET /api/v1/ws/terminal/{containerID}` | bearer | Interactive terminal proxy to the node agent |
| `GET /api/v1/ws/logs/{containerID}` | bearer | Live container log streaming |

Terminal sessions are proxied: browser → control plane (WebSocket) → node agent
over mTLS ConnectRPC → the container's Docker exec. Input, resize events, and
output flow bidirectionally through the same connection.

## Health & Metrics

- `GET /api/v1/health` — liveness (no auth).
- `GET /api/v1/ready` — readiness incl. database check (no auth).
- `GET /api/v1/metrics` — application metrics (no auth).
- `GET /metrics` — Prometheus scrape endpoint (no auth; cluster-internal
  scraping only — do not expose it at the edge).

## Versioning

The API version is part of the path (`/api/v1`) and tracked by the `version`
field in `api/openapi.yaml`. Breaking changes bump the path major; additive
changes bump the spec's minor version.
