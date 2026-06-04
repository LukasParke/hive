<div align="center">

# 🐝 Hive

**Self-hosted deployment platform for Docker Swarm**

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](https://react.dev)
[![PostgreSQL](https://img.shields.io/badge/Postgres-16-4169E1?logo=postgresql&logoColor=white)](https://postgresql.org)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[Documentation](https://github.com/LukasParke/hive/tree/main/docs) · [Releases](https://github.com/LukasParke/hive/releases) · [Issues](https://github.com/LukasParke/hive/issues)

</div>

---

## What is Hive?

Hive is an open-source, self-hosted platform for deploying applications, databases, and services on **Docker Swarm**. It gives you the ease of a cloud PaaS with the control of your own infrastructure.

- **Deploy anything** — Git repos, Docker images, or Compose stacks
- **Automatic HTTPS** — Traefik handles certificates via Let's Encrypt
- **Preview deployments** — Every pull request gets its own URL
- **Built-in CI/CD** — Build images with Buildkit and push to your own registry
- **Self-updating** — Update the platform itself from the UI with zero downtime
- **Team-ready** — Organizations, RBAC, invitations, and API keys out of the box

No vendor lock-in. All configurations live on your servers. If you stop using Hive, your services keep running.

---

## 🚀 Quick Start

### One-line Install

```bash
curl -fsSL https://raw.githubusercontent.com/LukasParke/hive/main/hivectl | bash -s install
```

Or clone the repo and run locally:

```bash
git clone https://github.com/LukasParke/hive.git && cd hive
export HIVE_DOMAIN=hive.example.com
export ACME_EMAIL=admin@example.com
./hivectl install
```

After install, open `https://hive.example.com` and create your admin account.

### Update

```bash
./hivectl update          # Update to latest nightly
./hivectl update nightly  # Update to specific tag
```

### Uninstall

```bash
./hivectl uninstall        # Remove stack (preserves data)
./hivectl uninstall --purge  # Remove everything including volumes
```

---

## ✨ Features

| Feature | Description |
|---------|-------------|
| 🐳 **Swarm-Native** | Built for Docker Swarm with replicated control-plane, global agents, and rolling updates |
| 🔒 **Automatic HTTPS** | Traefik with Let's Encrypt — zero config TLS for every domain |
| 🏗️ **Git & Image Deploys** | Deploy from Git repos (with webhooks) or push Docker images directly |
| 🧪 **Preview Deployments** | Auto-deploy PRs with cleanup on merge/close |
| 🛡️ **Security Rules** | IP allowlists, blocklists, header security, and rate limiting via Traefik middleware |
| 🔑 **Encrypted Secrets** | AES-256-GCM secrets stored in PostgreSQL with HKDF key derivation |
| 👥 **Team Management** | Organizations, member roles, email invitations, API keys |
| 📊 **Monitoring** | Real-time metrics, build logs, container logs, and WebSocket event streams |
| 🔄 **Self-Updating** | Check for releases and trigger zero-downtime Swarm rolling updates from the UI |
| 🗄️ **Database Services** | One-click Postgres, MySQL, Redis, MongoDB with backups |
| ⏰ **Scheduled Jobs** | Cron-based backups, maintenance, and custom tasks via River queue |
| 🌐 **Multi-Node** | Scale across multiple Swarm nodes with automatic service placement |

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Docker Swarm                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  Traefik    │  │ Control     │  │ Agent (global)      │  │
│  │  (global)   │  │ Plane (x3)  │  │ One per node        │  │
│  │  :80 :443   │  │ :3000       │  │ :9090 :9091         │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
│         │                │                  │                │
│  ┌──────┴──────┐  ┌─────┴──────┐  ┌────────┴────────┐       │
│  │  PostgreSQL │  │  Buildkit  │  │  OCI Registry   │       │
│  │  + PgBouncer│  │  (builder) │  │  (registry)     │       │
│  └─────────────┘  └────────────┘  └─────────────────┘       │
└─────────────────────────────────────────────────────────────┘
```

| Component | Technology | Purpose |
|-----------|-----------|---------|
| **Control Plane** | Go 1.25 + Chi + pgx | API server, orchestration, job scheduling |
| **Agent** | Go 1.25 + ConnectRPC | Per-node operations, metrics, log streaming |
| **Queue** | River (PostgreSQL) | Reliable background job processing |
| **Database** | PostgreSQL 16 + sqlc | Migrations, typed queries, LISTEN/NOTIFY |
| **Proxy** | Traefik v3.2 | Swarm-aware routing with automatic TLS |
| **Build** | Moby Buildkit | Distributed image builds |
| **Registry** | Distribution v2 | Private OCI image storage |
| **UI** | React 19 + Vite | Dark-themed dashboard with real-time updates |

---

## 📁 Repository Layout

```
hive/
├── api/                    # OpenAPI 3.1 contract (source of truth)
├── control-plane/          # Go API server & orchestration runtime
│   ├── cmd/control-plane/  # Main entrypoint
│   ├── internal/api/       # HTTP handlers (one per domain)
│   ├── internal/db/        # sqlc queries & migrations
│   ├── internal/deploy/    # Swarm service management
│   ├── internal/jobs/      # River workers (build, preview, cleanup)
│   ├── internal/updater/   # Self-update engine
│   └── internal/version/   # Build-time version injection
├── agent/                  # Go per-node agent
│   ├── cmd/agent/          # Main entrypoint
│   └── internal/           # Docker client, metrics, host ops
├── proto/                  # ConnectRPC protobuf definitions
├── ui/                     # React frontend
│   ├── src/pages/          # Dashboard & auth pages
│   ├── src/components/     # Shared UI components
│   └── src/theme.css       # Design system
├── deploy/                 # Stack files & bootstrap scripts
│   ├── hive-stack.yml      # Main Swarm stack
│   ├── hive-stack.ci.yml   # CI overlay (reduced replicas)
│   ├── monitoring-stack.yml
│   └── patroni-stack.yml
├── tests/
│   ├── e2e/                # Playwright E2E tests
│   └── integration/        # Swarm dind integration tests
├── docs/                   # ADRs, architecture, operations
└── hivectl                 # Install / update / uninstall script
```

---

## 🛠️ Development

### Prerequisites

- Go 1.25+
- Node.js 22+
- PostgreSQL 16
- Docker & Docker Swarm

### Run Locally

```bash
# 1. Start PostgreSQL
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/hive?sslmode=disable"

# 2. Run control-plane
cd control-plane
go run ./cmd/control-plane

# 3. In another terminal, run the UI
cd ui
npm install
npm run dev

# 4. Open http://localhost:5173
```

### Build Images

```bash
make images VERSION=v0.1.0
```

### Run Tests

```bash
make test                          # All Go tests
cd ui && npm run typecheck         # TypeScript type checking
cd tests/e2e && npx playwright test # E2E tests
```

---

## 📖 Documentation

| Topic | Location |
|-------|----------|
| Architecture Overview | [`docs/architecture/overview.md`](docs/architecture/overview.md) |
| Deployment Quickstart | [`docs/deployment/quickstart.md`](docs/deployment/quickstart.md) |
| Operations Runbook | [`docs/operations/runbook.md`](docs/operations/runbook.md) |
| ADRs | [`docs/adr/`](docs/adr/) |

---

## 🔧 `hivectl` Commands

```bash
./hivectl install              # Initial installation
./hivectl update [version]     # Update to latest or specific version
./hivectl status               # Show stack status
./hivectl logs [service]       # Tail service logs
./hivectl uninstall [--purge]  # Remove stack
```

---

## 🔄 Nightly Releases

Every push to `main` triggers a nightly release:

- Images: `ghcr.io/luke/hive/control-plane:nightly` and `ghcr.io/luke/hive/agent:nightly`
- Release: [github.com/LukasParke/hive/releases/tag/nightly](https://github.com/LukasParke/hive/releases/tag/nightly)
- Changelog: Auto-generated from last 20 commits

---

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/amazing-feature`)
3. Commit your changes
4. Push to the branch
5. Open a Pull Request

All PRs must pass:

- `Go Build and Test`
- `Integration (Swarm dind)`

Set both as required checks in branch protection for `main`.

---

## 📜 License

Hive is released under the [MIT License](LICENSE).

---

<div align="center">

**[⬆ Back to Top](#-hive)**

Built with 🐝 by the Hive team

</div>
