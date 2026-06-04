# Hive

Hive is a self-hosted platform for deploying and managing applications on Docker Swarm.

- **Go control plane** — Replicated Swarm service with leader election
- **Go per-node agent** — Global Swarm service for node operations
- **PostgreSQL + sqlc + River** — Reliable job queue and data layer
- **OpenAPI-first** — Contract-driven API with generated TypeScript clients
- **Buildkit + OCI registry** — Integrated build pipeline
- **Traefik swarm-provider** — Automatic HTTPS routing and middleware
- **Self-updating** — Check for releases and roll out updates from the UI

## Repository Layout

| Directory | Purpose |
|-----------|---------|
| `api/` | OpenAPI source contract |
| `control-plane/` | Go API server and orchestration runtime |
| `agent/` | Go node agent |
| `deploy/` | Stack files and bootstrap scripts |
| `docs/` | Architecture, ADRs, and operations guides |
| `tests/` | Integration and E2E test suites |
| `ui/` | React frontend |

## Quick Start

### Install

```bash
# Clone and enter the repo
git clone https://github.com/LukasParke/hive.git && cd hive

# Install on a Docker Swarm manager
export HIVE_DOMAIN=hive.example.com
export ACME_EMAIL=admin@example.com
./hivectl install
```

### Update

```bash
# Update to the latest nightly release
./hivectl update

# Or update to a specific version
./hivectl update nightly-20250603-abc123
```

### Development

1. Install Go 1.25+ and Node.js 22+.
2. Start PostgreSQL 16 and export `DATABASE_URL`.
3. Run tests:
   ```bash
   cd control-plane && go test ./...
   cd ../agent && go test ./...
   cd ../ui && npm install && npm run typecheck
   ```

## CI Merge Gate

Pull requests must pass:

- `Go Build and Test`
- `Integration (Swarm dind)`

Set both checks as required in branch protection rules for `main`.

## Nightly Releases

Every push to `main` triggers a nightly release:

- Images are published to `ghcr.io/luke/hive/control-plane` and `ghcr.io/luke/hive/agent`
- A GitHub pre-release is created at `https://github.com/LukasParke/hive/releases/tag/nightly`
- The running platform can self-update via the UI or `./hivectl update`
