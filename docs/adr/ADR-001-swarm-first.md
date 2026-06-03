# ADR-001: Swarm-First Architecture

## Status
Accepted

## Decision
Hive targets Docker Swarm as the primary deployment platform. Single-node deployment is implemented as a one-node Swarm.

## Consequences
- Control-plane orchestration APIs map to Swarm concepts (service, task, node, network, secret, config).
- Feature design optimizes for multi-node operations first.
- No local-only deployment path becomes a hard dependency.
