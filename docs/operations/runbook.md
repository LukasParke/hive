# Operations Runbook

Operational procedures for a running Hive cluster. Deployment guides live in
[deployment/](../deployment/production.md); security hardening in
[security.md](../security.md).

## Health Checks

- Control plane: `GET /api/v1/health` (liveness)
- Readiness: `GET /api/v1/ready` (includes database check)
- Agent: `GET /healthz` on localhost:9091 of each node (healthcheck target;
  the RPC port 9090 is mTLS-only and not published)

```sh
curl -fsS https://hive.example.com/api/v1/ready
docker service ps hive_control-plane hive_agent
```

## Leader Election

Only one control-plane replica is leader at a time; leadership is a Postgres
advisory lock (`db.LockLeaderElection = 1`) retried every 5 seconds. The
leader starts the periodic River jobs (cert renewal hourly, cleanup daily);
non-leaders still run regular job workers.

Inspect who holds locks:

```sql
-- Advisory lock holders (1=leader, 2=bootstrap, 3=cert-renewal)
select l.pid, l.objid as lock_id, a.usename, a.application_name,
       a.client_addr, a.query_start
from pg_locks l join pg_stat_activity a on a.pid = l.pid
where l.locktype = 'advisory' order by l.objid;
```

Troubleshooting:

- **No lock with objid=1 held** → no leader; periodic work (backups, cert
  renewal) is paused. Check `docker service ps hive_control-plane` for restart
  loops and control-plane logs for `leader lock failed`.
- **Stale session holding the lock** (old task in `idle in transaction`):
  terminate it — `select pg_terminate_backend(<pid>);` — leadership re-races
  within ~5 s.
- **Two leaders observed** is impossible while both hold the *same* session
  lock; if you see duplicate periodic work, verify all replicas run the same
  image tag.

## River Job Inspection

All background work (builds, deploys, backups, cert renewal, cleanup,
previews) lives in the `river_job` table. Queues: `build`, `deploy`,
`default`; states include `available`, `running`, `retryable`, `discarded`.

```sql
-- Queue depth by state
select queue, state, count(*) from river_job group by 1, 2 order by 1, 2;

-- Stuck or failing jobs
select id, kind, queue, state, attempt, max_attempts, errors,
       scheduled_at, created_at
from river_job
where state in ('retryable', 'discarded')
order by scheduled_at desc limit 20;

-- Long-running jobs
select id, kind, state, started_at, now() - started_at as runtime
from river_job where state = 'running'
order by started_at;

-- Requeue a stuck job (drain workers first if possible)
update river_job set state = 'available', scheduled_at = now() where id = <id>;
```

Build/preview progress is also tracked in application tables (`build_jobs`,
`deployments`); `river_job` is the execution-level view. Discarded build jobs
with matching failed rows usually mean exhausted retries (builds: 3 attempts,
deploys: 4).

## Swarm Cache Staleness

List endpoints for services/tasks/nodes read the `swarm_cache_*` tables,
kept current by the docker-events watcher. After an event storm, watcher
crash, or manual DB surgery, cached rows can go stale (ghost services,
missing tasks).

Fix by forcing a full resync:

1. Compare cache vs reality:
   ```sql
   select name, updated_at from swarm_cache_services order by updated_at desc limit 20;
   docker service ls
   ```
2. Restart the control plane to trigger the watcher's startup resync:
   ```sh
   docker service update --force hive_control-plane
   ```
   Rolling restart keeps the UI/API available.
3. Verify timestamps refresh within a minute and NOTIFY events flow
   (`system` channel visible via `/api/v1/ws/events`).

If staleness recurs, check watcher logs for Docker API errors (socket
permissions, manager reachability).

## Agent Certificate Renewal Failures

Agent mTLS certs are valid 72h and renewed automatically; the control-plane
client cert is renewed hourly by the leader-run `cert-renewal` River job.
Failure symptoms: agents drop off `/api/v1/nodes`, control-plane logs show TLS
handshake or `certificate has expired` errors, terminal/log streaming fails.

Playbook:

1. Identify scope: expired agent leaf certs (one node) vs control-plane client
   cert (all nodes fail) vs CA mismatch (everything, after a secrets-store
   loss).
2. Check the renewal job:
   ```sql
   select id, state, attempt, errors from river_job
   where kind like '%CertRenewal%' order by created_at desc limit 5;
   ```
3. **Control-plane client cert**: confirm the leader can read its material
   from the secrets store (master key mounted?). A healthy renewal job on the
   next hour boundary should recover transient failures; force it by
   restarting the control plane.
4. **Single agent**: restart that node's agent task
   (`docker service ps hive_agent` to find it, then drain/restart the task).
   It re-bootstraps using `agent-bootstrap-token` and re-enrolls against the
   CA config `hive-agent-ca`.
5. **CA loss / full distrust**: the CA lived in the encrypted secrets store.
   If it was destroyed, a new CA is generated but existing agents reject it —
   republish the new CA as the `hive-agent-ca` Swarm config
   (`docker config create hive-agent-ca ca.pem`) and redeploy the stack so all
   agents remount it. See [security.md](../security.md#mtls-trust-chain).
6. Verify: nodes list healthy again and a terminal session opens.

## Secret Rotation Runbook

Encrypted values (registry passwords, SSH keys, certificates) live in
`secrets_store`, AES-256-GCM under the master key.

- **Rotate a single stored secret** (e.g. registry password changed upstream):
  update it in the UI (or `PUT /api/v1/registries/{id}`); the value is
  re-encrypted on write. Then run the registry "Test connection".
- **Rotate `postgres-password`**: change it inside Postgres first, then update
  the Swarm secret and redeploy so `pgbouncer`/control plane pick it up — do
  this in one window to avoid lockout.
- **Rotate `agent-bootstrap-token`**: see
  [security.md](../security.md#bootstrap-token-rotation). Running agents are
  unaffected.
- **Rotate the master key** (full re-encrypt): follow the manual procedure in
  [security.md](../security.md#rotation-procedure) — requires a maintenance
  window and offline backup of the old key until verification completes.
- After any rotation, watch `/api/v1/ready`, a test deploy, and agent
  enrollment before closing the incident.

## Node Maintenance

1. Drain node in Swarm: `docker node update --availability drain <node>`.
2. Wait for tasks to reschedule (`docker node ps <node>` empty).
3. Perform host maintenance.
4. Reactivate: `docker node update --availability active <node>`.

Stateful labels (`db=true`, `registry=true`) must stay pinned to the node
holding their volumes — never drain those nodes without migrating volumes
first.

## Backup Strategy

- PostgreSQL logical dumps daily (Hive schedules or Patroni tooling on Tier 2)
  plus volume snapshots of `hive_pgdata`.
- Registry storage snapshots.
- Shared volume snapshots (ACME state + staging).
- Back up the master key **offline** — without it, backups of encrypted
  secrets are unrecoverable.
- Restore paths: database backups restore via
  `POST /api/v1/backups/{id}/restore`; always test restores into a scratch
  database ([patroni-ha.md](../deployment/patroni-ha.md#operational-rules)).
