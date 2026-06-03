# Required Branch Checks

Configure branch protection on `main` to require:

- `Go Build and Test / control-plane`
- `Go Build and Test / agent`
- `Integration (Swarm dind) / integration`

This ensures unit and dind Swarm integration validations gate merges.
