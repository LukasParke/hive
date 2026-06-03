# Hive

Hive is a Swarm-native platform that rebuilds Dokploy's capabilities around:

- A Go control plane
- A Go per-node agent
- PostgreSQL + sqlc + River
- OpenAPI-first contracts
- Buildkit + OCI registry
- Traefik swarm-provider routing

## Repository Layout

- `api/` OpenAPI source contract
- `control-plane/` Go API server and orchestration runtime
- `agent/` Go node agent
- `deploy/` stack files and bootstrap scripts
- `docs/` architecture, ADRs, and operations guides
- `tests/` integration and e2e test suites
- `ui/` React frontend

## Quick Start (Development)

1. Install Go 1.25+.
2. Install Node.js 22+.
3. Start PostgreSQL 16 and export `DATABASE_URL`.
4. Run:
   - `go test ./...` in `control-plane`
   - `go test ./...` in `agent`
   - `npm install && npm run build` in `ui`

## CI Merge Gate

Hive pull requests must pass both unit and integration workflows:

- `Go Build and Test`
- `Integration (Swarm dind)`

Set both checks as required in branch protection rules for `main`.
