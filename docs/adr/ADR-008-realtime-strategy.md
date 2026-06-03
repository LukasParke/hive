# ADR-008: Real-Time Transport Strategy

## Status
Accepted

## Decision
Use Postgres LISTEN/NOTIFY for discrete state events and WebSocket/stream transport for continuous data (logs, terminal, metrics streams).

## Consequences
- Notification payloads stay compact and state-oriented.
- Stream workloads bypass the database event path.
