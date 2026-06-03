# ADR-005: Go Agent Per Node

## Status
Accepted

## Decision
A lightweight Go agent runs on every Swarm node as a global service and exposes exec/log/stats capabilities over mTLS.

## Consequences
- Terminal and node-local metrics are available cluster-wide.
- Control-plane does not need direct remote shell access.
