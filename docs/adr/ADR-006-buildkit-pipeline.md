# ADR-006: Buildkit Build Pipeline

## Status
Accepted

## Decision
Image builds run via Buildkit service(s) in Swarm. The control-plane does not run local `docker build`.

## Consequences
- Build workloads can be isolated to labeled worker nodes.
- Build observability is implemented as streamed status and persisted logs.

## Implementation Note (post-deployment)

Swarm services cannot be granted `CAP_SYS_ADMIN` (the API has no equivalent of
`--privileged`), and buildkitd's OCI/native snapshotter needs `mount(2)` for
its snapshot binds. A BuildKit *swarm service* therefore fails every solve
with "operation not permitted". The shipped architecture runs one privileged
buildkitd container per `builder=true` node at the host level
(`restart=unless-stopped`, aliased `buildkit` on `hive_internal`, insecure-registry
config for the bundled registry); the control plane addresses it through
`BUILDKIT_ADDR` exactly as this ADR describes. Everything else here — queue,
push-to-registry, no docker builds in the control plane — is unchanged.
