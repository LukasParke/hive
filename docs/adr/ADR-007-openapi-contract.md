# ADR-007: OpenAPI 3.1 API Contract

## Status
Accepted

## Decision
OpenAPI 3.1 is the external API contract source of truth. Server and client bindings are generated from the same spec.

## Consequences
- Contract changes are reviewed as schema diffs.
- Implementation and UI are validated against generated artifacts.
