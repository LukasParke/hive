# Operations Runbook

## Health Checks

- Control plane: `GET /api/v1/health`
- Readiness: `GET /api/v1/ready`
- Agent: `GET /health`

## Node Maintenance

1. Drain node in Swarm.
2. Wait for tasks to reschedule.
3. Perform host maintenance.
4. Activate node.

## Backup Strategy

- PostgreSQL logical dump daily.
- Registry storage snapshots.
- Shared volume snapshots.
