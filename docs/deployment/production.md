# Production Deployment (3 Managers + N Workers)

This guide covers a production-grade Hive installation: a 3-manager Swarm cluster
with N dedicated worker nodes. For a single-node evaluation, see the
[Quickstart](quickstart.md).

## Prerequisites

- Docker Engine 24+ on every node, Swarm initialized
- A DNS record for the Hive UI (e.g. `hive.example.com`) pointing at a load
  balancer or any manager node, plus DNS for every application domain you plan
  to serve
- `openssl` on the bootstrap node for secret generation
- Open firewall ports between all nodes (see [cloud.md](cloud.md) for
  provider-specific notes):
  - `2377/tcp` — Swarm cluster management
  - `7946/tcp+udp` — node gossip
  - `4789/udp` — VXLAN overlay traffic
  - `80/tcp` and `443/tcp` — HTTP/HTTPS ingress (Traefik)

## Sizing Guidance

| Role | Nodes | Suggested size | Notes |
|------|-------|----------------|-------|
| Manager / control-plane | 3 | 2 vCPU, 4 GB RAM | Runs control-plane, Traefik, pgbouncer. Never fewer than 3 for HA. |
| `db=true` | 1 (pin!) | 4 vCPU, 8 GB RAM, SSD | PostgreSQL data volume lives here. |
| `registry=true` | 1 (pin!) | 2 vCPU, 20–100 GB disk | Registry storage sized to image churn. |
| `builder=true` | 1+ | 4–8 vCPU, 8–16 GB RAM | BuildKit builds; more builders parallelize the build queue. |

> **BuildKit runs at the host level, not in the stack.** Swarm services cannot
> be granted `CAP_SYS_ADMIN`, and buildkitd's snapshotter requires `mount(2)`.
> On every `builder=true` node, run:
>
> ```bash
> printf '[registry."registry:5000"]
  http = true
  insecure = true
' > /etc/hive/buildkitd.toml
> docker run -d --privileged --restart=unless-stopped >   --name hive-buildkit --network hive_internal --network-alias buildkit >   -v /etc/hive/buildkitd.toml:/etc/buildkit/buildkitd.toml:ro >   -v hive_buildkit-cache:/var/lib/buildkit >   moby/buildkit:latest --addr tcp://0.0.0.0:1234
> ```
>
> `hivectl install` and `deploy/init.sh` do this automatically on the first
> (manager) node.
| Workers (apps) | N | per workload | Run user applications and databases. |

## Shared Storage: NFS vs S3-backed

Hive mounts a `shared` volume at `/data/shared` on control-plane and Traefik
replicas for **ACME state** (`/data/shared/acme.json`) and **backup staging**.

- **NFS / EFS / Filestore (recommended for `shared`)** — POSIX filesystem, so
  all Traefik replicas share one ACME account and certificate store. Required
  on multi-manager clusters; otherwise each Traefik replica keeps its own
  `acme.json` and they will independently rate-limit against Let's Encrypt.
  Downside: another moving part; EFS/Filestore cost more than object storage.
- **S3-backed (via s3fs/GeeseFS)** — cheap and durable, but not a real POSIX
  filesystem: file locking and atomic renames are unreliable, which corrupts
  ACME state under concurrent renewal. **Do not** back the `shared` volume with
  object storage.
- **S3-compatible object storage is the right choice for backup destinations**
  (`POST /api/v1/backup/destinations`) — configure it in the UI, not as the
  `shared` mount.

## Node Labels

Hive pins stateful services with node labels:

```sh
docker node update --label-add db=true <postgres-node>
docker node update --label-add registry=true <registry-node>
docker node update --label-add builder=true <builder-node>
```

`db=true` and `registry=true` must each match **exactly one node** — the node
that already holds the `hive_pgdata` / `hive_registry-data` volumes (Docker
named volumes are node-local). If you later move these labels, migrate the
volumes first or use a shared/external volume driver.

## External Secrets

Create the four Swarm secrets before deploying (the quickstart `init.sh` does
this automatically on single-node installs):

```sh
printf '%s' "$(openssl rand -hex 32)" | docker secret create hive-master-key -
printf '%s' "$(openssl rand -hex 24)" | docker secret create postgres-password -
printf '%s' "$(openssl rand -hex 32)" | docker secret create hive-jwt-secret -
printf '%s' "$(openssl rand -hex 24)" | docker secret create agent-bootstrap-token -
```

- `hive-master-key` — root key for the encrypted secrets store
  (AES-256-GCM/HKDF). Losing it makes stored secrets undecryptable; see
  [security.md](../security.md#master-key-management).
- `agent-bootstrap-token` — used by agents to bootstrap mTLS trust; rotate via
  the procedure in [security.md](../security.md#bootstrap-token-rotation).

If the internal CA certificate is managed manually, also publish it as a Swarm
config: `docker config create hive-agent-ca ca.pem`.

## Deploy the Stack

```sh
git clone https://github.com/LukasParke/hive.git && cd hive
export HIVE_DOMAIN=hive.example.com
export ACME_EMAIL=ops@example.com
export HIVE_IMAGE_TAG=<pinned-semver-tag>   # avoid `latest` in production
docker stack deploy -c deploy/hive-stack.yml hive
```

### Wildcard certificates (DNS-01)

Traefik issues certificates via the TLS-ALPN-01 challenge by default, which
cannot cover wildcard names. To route wildcard application domains
(`*.apps.example.com`), switch the Let's Encrypt solver to the DNS-01 challenge
by setting two environment variables on the bootstrap node before
`hivectl install` / `deploy/init.sh` (and on later `hivectl update` runs):

```sh
export HIVE_ACME_DNS_PROVIDER=cloudflare   # any lego DNS provider name
export HIVE_ACME_DNS_TOKEN=<provider-api-token>
./hivectl install   # or: deploy/init.sh
```

When `HIVE_ACME_DNS_PROVIDER` is set, the bootstrap scripts:

1. store the token as the Swarm secret `hive-acme-dns-token`;
2. generate a temporary Compose overlay that repeats Traefik's command with the
   `--certificatesresolvers.letsencrypt.acme.dnschallenge.*` flags added and
   mounts the token at `/run/secrets/acme-dns-token`;
3. run `docker stack deploy -c hive-stack.yml -c <overlay>` so every
   redeploy (updates, host-management toggles) keeps DNS-01 enabled.

For Cloudflare the token is exposed as `CF_DNS_API_TOKEN_FILE`; other providers
need their own environment variables in the generated overlay (see the
[lego DNS provider reference](https://go-acme.github.io/lego/dns/) for exact
names). Without these variables nothing changes: Traefik keeps using
TLS-ALPN-01.

## Verification Checklist

1. `docker node ls` — all 3 managers `Ready`, `Leader`/`Reachable`.
2. `docker stack services hive` — every service at desired replicas:
   `control-plane` 3/3, `agent` 1 per node (global), `traefik` global on
   managers, `postgres` 1/1 on the `db` node, `registry` 1/1.
3. `curl -fsS https://hive.example.com/api/v1/health` returns healthy.
4. `curl -fsS https://hive.example.com/api/v1/ready` returns ready (checks
   database reachability).
5. `docker service ps hive_agent --format '{{.Node}} {{.CurrentState}}'` — an
   agent task runs on every node.
6. Open `https://hive.example.com`, register the first admin account at
   `/register`, then remove public access to `/register` if your policy
   requires it.
7. TLS: the certificate served is a real Let's Encrypt cert (check the
   issuer). If not, check Traefik logs and the `shared` ACME mount.

## Joining Nodes

Generate join tokens on any manager:

```sh
# Worker node
docker swarm join-token worker
#   docker swarm join --token <worker-token> <manager-ip>:2377

# Additional manager (keep the raft at 3 or 5, never 4)
docker swarm join-token manager
#   docker swarm join --token <manager-token> <manager-ip>:2377
```

After a node joins:

1. Apply the appropriate labels (`db=true` / `registry=true` / `builder=true`)
   — the agent global service picks the node up automatically.
2. Confirm connectivity: `docker node ls` shows it `Ready`, and the agent task
   on it reaches `Running`.
3. Verify overlay reachability from the new node:
   `docker network inspect hive_internal` lists it as an attachment target once
   a task lands there.

## Next Steps

- [Patroni HA overlay](patroni-ha.md) — Tier-2 Postgres HA
- [External services](external-services.md) — external Postgres, external
  registries, BYO Traefik
- [Operations runbook](../operations/runbook.md)
- [Security hardening](../security.md)
