# ADR-004: Registry Required for Multi-Node

## Status
Accepted

## Decision
Hive requires an OCI registry for multi-node operation. A built-in registry service is provided in the default stack.

## Consequences
- Build pipeline always pushes images.
- Deploy pipeline always references immutable image tags.
