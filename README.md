# Hive

A Docker Swarm orchestrator for homelabs. Deploy a single container and Hive bootstraps your entire infrastructure -- Postgres, Traefik, NATS, and a container registry -- then gives you a dashboard to manage apps, databases, and nodes.

## Features

- **Single container deployment** -- one `docker run` command sets up everything
- **Docker Swarm native** -- works on 1 node, scales to 6 seamlessly
- **App deployment** -- from Docker images, Git repos (Dockerfile/Nixpacks), or Compose files
- **One-click catalog** -- curated app templates (Gitea, Nextcloud, Grafana, n8n, etc.)
- **Managed databases** -- provision Postgres, MySQL, Redis, or MongoDB as Swarm services
- **Automatic SSL** -- Traefik with Let's Encrypt on every node
- **Teams & orgs** -- BetterAuth with organizations, roles, and invitations
- **Backup to S3** -- scheduled database backups to any S3-compatible storage
- **Real-time logs** -- WebSocket log streaming from builds and deployments
- **Auto-scaling** -- add nodes and Hive automatically deploys a registry and redistributes workloads

## Quick Start

```bash
docker run -d \
  --name hive \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v hive-data:/data \
  -p 80:80 \
  -p 443:443 \
  -p 8080:8080 \
  hive:latest
```

Then visit `http://<your-ip>:8080` to complete setup.

## Architecture

The Hive container runs a single Go binary. The SvelteKit frontend is compiled at Docker build time and served as pre-built assets -- Node.js is **not** required at runtime.

```
┌────────────────────────────────────────┐
│           Hive Container               │
│                                        │
│  ┌──────────────────────────────────┐  │
│  │  Go Binary (PID 1)              │  │
│  │  ├─ HTTP API :8080              │  │
│  │  ├─ Embedded NATS (JetStream)   │  │
│  │  └─ Worker Goroutine Pool       │  │
│  └────┬─────────────────────────────┘  │
│       │                                │
│  ┌────┴─────┐  ┌────────────────────┐  │
│  │ Pre-built│  │ Catalog Templates  │  │
│  │ UI assets│  │ (YAML)            │  │
│  └──────────┘  └────────────────────┘  │
└───────┬────────────────────────────────┘
        │ Docker SDK
        ▼
┌─────────────────┐
│  Docker Swarm   │
│                 │
│  - Postgres     │
│  - Traefik      │
│  - Registry     │
│  - hive-agent   │
│  - Your Apps    │
│  - Your DBs     │
└─────────────────┘
```

## Development

### Prerequisites

- Go 1.23+
- Docker with Swarm mode
- Node.js 22+ *(frontend development only)*

### Setup

```bash
# Build and run Go API in dev mode
make run

# (Optional) Run SvelteKit frontend in dev mode (separate terminal)
cd ui && npm install
make ui-dev
```

### Project Structure

```
hive/
├── cmd/hive/           # Go entrypoint
├── internal/
│   ├── api/            # HTTP API (Chi router)
│   ├── bootstrap/      # Infrastructure provisioning
│   ├── swarm/          # Docker Swarm client
│   ├── deploy/         # Deployment engine
│   ├── worker/         # NATS job workers
│   ├── nats/           # Embedded NATS server
│   ├── store/          # Database layer + migrations
│   ├── proxy/          # Traefik label generation
│   ├── catalog/        # One-click app templates
│   ├── database/       # Managed DB provisioning
│   ├── backup/         # Backup scheduling + S3
│   └── monitor/        # Health checks + metrics
├── pkg/
│   ├── config/         # Configuration
│   └── encryption/     # AES-GCM encryption
├── ui/                 # SvelteKit frontend (compiled at build time)
├── templates/          # Catalog app templates
├── Dockerfile          # Multi-stage build
└── Makefile
```

## Adding Nodes

From the Hive dashboard, go to **Nodes** and copy the join command. Run it on your new machine:

```bash
docker swarm join --token <TOKEN> <MANAGER_IP>:2377
```

Hive automatically detects the new node and:
1. Deploys a container registry (if not already running)
2. Enables Traefik on the new node
3. Distributes app workloads using spread placement

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `HIVE_ROLE` | `manager` | `manager`, `worker`, or `agent` |
| `HIVE_DATA_DIR` | `/data` | Persistent data directory |
| `HIVE_API_PORT` | `8080` | Go API port |
| `HIVE_UI_DIR` | `/app/ui` | Pre-built frontend assets directory |
| `HIVE_NATS_PORT` | `4222` | NATS port (multi-node) |
| `HIVE_DEV` | `` | Enable dev mode |
| `HIVE_ENCRYPTION_KEY` | auto | 64-char hex AES-256 key |
| `DATABASE_URL` | auto | Postgres connection string |

## License

MIT
