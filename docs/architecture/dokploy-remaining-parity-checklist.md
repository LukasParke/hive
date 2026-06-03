# Dokploy Remaining Parity Checklist

This file freezes the current Dokploy-to-Hive parity matrix as the execution checklist for migration closure.

## Execution Checklist

- [x] Freeze parity gap matrix as source checklist.
- [ ] Identity/membership parity:
  - [ ] Password reset request + reset confirmation APIs.
  - [ ] Invitation token validation + acceptance APIs.
  - [ ] Organization members + invitation lifecycle APIs.
  - [ ] API key list/revoke lifecycle.
  - [ ] UI wiring for reset + invitation acceptance.
- [ ] Runtime operations parity:
  - [ ] Application start/stop/restart + logs.
  - [ ] Stack start/stop/restart.
  - [ ] Central deployments list + deletion controls.
  - [ ] UI runtime controls for new operations.
- [ ] Settings/platform parity:
  - [ ] Servers API + UI.
  - [ ] Cluster info API + UI.
  - [ ] SSH key API + UI.
  - [ ] Certificate API + UI.
  - [ ] Requests/events API + UI.
- [ ] Data/compose + CRUD closure:
  - [ ] Missing client methods + forms for domain/registry/stack/backup-destination/git-provider/notification create flows.
- [ ] Certification:
  - [ ] Full tests + build.
  - [ ] Redeploy + smoke checks.
  - [ ] PASS/PARTIAL/OUT-OF-SCOPE publication.
