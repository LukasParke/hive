# ADR-003: Tiered PostgreSQL HA

## Status
Accepted

## Decision
Hive supports three Postgres deployment tiers:

1. Single Postgres with persistent volume
2. Patroni + etcd HA overlay
3. External managed Postgres

## Consequences
- App code consumes one connection string abstraction.
- Production guidance includes explicit tier trade-offs.
