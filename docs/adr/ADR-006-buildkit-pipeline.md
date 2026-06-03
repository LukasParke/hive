# ADR-006: Buildkit Build Pipeline

## Status
Accepted

## Decision
Image builds run via Buildkit service(s) in Swarm. The control-plane does not run local `docker build`.

## Consequences
- Build workloads can be isolated to labeled worker nodes.
- Build observability is implemented as streamed status and persisted logs.
