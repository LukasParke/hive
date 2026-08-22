# Required Branch Checks

Configure branch protection on `main` to require:

- `Go Build and Test / control-plane`
- `Go Build and Test / agent`
- `Go Build and Test / lint`
- `Go Build and Test / proto-check`
- `Database Migrations / migrate`
- `UI Build / build`
- `API Contract / openapi`
- `E2E Tests / e2e`
- `Integration (Swarm dind) / integration`
- `Build and Publish Images / control-plane`
- `Build and Publish Images / agent`
- `Build and Publish Images / postgres-patroni`

This ensures unit, migration, UI, API-contract, e2e, dind Swarm integration,
and image-build validations gate merges.

## Release gate

Tagged releases are validated by the `Release` workflow (`.github/workflows/release.yml`),
triggered on `v*` tags only — it does not gate PR merges:

- `Release / version`
- `Release / Images (control-plane)`
- `Release / Images (agent)`
- `Release / Images (postgres-patroni)`
- `Release / integration` — full dind Swarm suite against the tagged commit; a
  failed run blocks the GitHub Release from being published.
- `Release / github-release`

The nightly flow (`Release / nightly-release`) remains main-push/schedule-driven
and is informational.
