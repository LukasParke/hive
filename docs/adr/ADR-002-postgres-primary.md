# ADR-002: PostgreSQL Primary Datastore

## Status
Accepted

## Decision
Hive uses PostgreSQL as the primary datastore and sqlc for compile-time typed query access.

## Consequences
- SQLite-era assumptions are removed.
- Migrations are required for all schema changes.
- Runtime SQL access is centralized through generated query layers.
