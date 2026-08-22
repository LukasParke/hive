# Patroni HA Overlay (Tier 2)

Hive's Postgres HA strategy is tiered (see
[ADR-003](../adr/ADR-003-tiered-postgres-ha.md)):

- **Tier 1 (default)** — single `postgres` service pinned to the `db=true`
  node, with backups. Right for most installations.
- **Tier 2 (this guide)** — 3-node Patroni cluster with automatic failover,
  fronted by PgBouncer.
- **Tier 3** — fully external/managed Postgres (see
  [external-services.md](external-services.md#external-postgresql-tier-3)).

## When to Use

Enable the Patroni overlay when Postgres downtime of minutes (node failure +
reschedule on a new node) is unacceptable and you can afford 3 dedicated DB
nodes. It adds operational complexity: etcd, Patroni failover, and split-brain
risk if you run it across unreliable networks.

**Prerequisite — verified backups.** Before enabling, take a logical backup of
the Tier-1 database and **restore it into a scratch database to prove it
works**. A failed failover with no verified backup is worse than Tier 1. Keep
the Tier-1 volume intact until the Patroni cluster is confirmed healthy.

## Deploy Order

Deploy from `deploy/patroni-stack.yml`, in this order:

1. **etcd (3 replicas, managers)** — Patroni's consensus store:
   ```sh
   docker stack deploy -c deploy/patroni-stack.yml patroni
   ```
   The stack file deploys `etcd` (3, manager-constrained), `patroni` (3), and
   `pgbouncer` together; verify each stage before moving on:
   `docker stack ps patroni`.
2. **Patroni (3 replicas)** — one per `db`-capable node. Patroni elects a
   leader and streams replication to the two replicas. Confirm a leader exists:
   check `patroni` logs for `I am the leader` on exactly one replica.
3. **PgBouncer** — connects to the Patroni cluster with `DB_HOST=patroni`,
   transaction pooling. The control-plane always talks to PgBouncer, never to a
   Patroni node directly, so failover is invisible to clients.
4. **Repoint the control plane** — set `DATABASE_URL` to the PgBouncer service
   endpoint and redeploy `hive`:
   ```sh
   DATABASE_URL="postgres://postgres:<pw>@pgbouncer:5432/hive?sslmode=disable" \
     HIVE_IMAGE_TAG=<tag> docker stack deploy -c deploy/hive-stack.yml hive
   ```
5. **Retire Tier 1** — after a full day of stable operation, stop the old
   single-node `postgres` service (keep its volume as a cold backup).

## PgBouncer Discovery

PgBouncer resolves the DNS name `patroni`, which Swarm load-balances across the
Patroni replicas. Patroni replicas that are not the leader proxy or redirect
write traffic to the current leader, so PgBouncer needs no knowledge of which
node is primary. Connection pooling mode is `transaction` — server-side
prepared statements must be compatible (pgx in the control-plane is configured
accordingly).

## Failover Behavior

- When the leader node fails, Patroni detects loss of the etcd lease, promotes
  the most-replicated replica, and updates routing.
- **Expectation: under 30 seconds** from leader loss to a writable primary.
  During the election window, writes fail or block; reads through PgBouncer may
  briefly serve stale data from replicas.
- Control-plane River workers and API requests that hit the window retry via
  River's attempt mechanism (builds: 3 attempts, deploys: 4 attempts); a short
  blip should not lose work.
- After failover, verify: `/api/v1/ready`, one Patroni leader in logs, and
  replication lag on the two new standbys.

## Operational Rules

- Never run fewer than 3 Patroni replicas; never place two on the same node.
- etcd must stay an odd number (3) and live on separate managers.
- Keep taking and verifying regular backups — HA is not a backup.
- To take the cluster down for maintenance, patroni-switchover to a healthy
  node first rather than letting the lease expire.
