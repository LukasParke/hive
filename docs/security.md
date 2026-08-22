# Security Guide

Hardening reference for a production Hive cluster. Deployment prerequisites
are in [production.md](deployment/production.md).

## mTLS Trust Chain

All control-plane → agent traffic (RPC, terminal, logs, metrics) is mutually
TLS by default: the stack sets `AGENT_MTLS_ENABLED=true` for both services,
agents present certificates signed by the internal CA, and the control plane
authenticates agents with its own client certificate. Only TLS 1.3 is
accepted. Certificates are short-lived (72 hours) and renewed automatically.

Trust chain layout:

- **Internal CA** — generated once by the control plane and persisted in the
  encrypted secrets store, so all replicas converge on the same authority.
- **`hive-agent-ca` Swarm config** — the CA certificate is published as a
  Swarm config and mounted read-only into every agent (`AGENT_CA_FILE`), so
  agents pin the CA and reject anything else.
- **Control-plane client cert** — issued by the CA, stored in the secrets
  store, renewed hourly by the `cert-renewal` River job (leader-run).
- **Agent leaf certs** — agents bootstrap with the `agent-bootstrap-token`
  secret to obtain their first certificate, then renew autonomously.

### CA lifecycle

- The CA private key never leaves the secrets store; only the public
  certificate is published as the `hive-agent-ca` config.
- Losing the secrets store (master key + rows) means losing the CA. Agents
  will reject a freshly generated CA — you must redeploy agents with a new
  `hive-agent-ca` config (see `docker config create hive-agent-ca ca.pem`
  note in `deploy/hive-stack.yml`).

### Certificate renewal

Renewal is automatic: the control-plane client certificate is renewed by the
hourly periodic River job; agent certs renew before expiry. If renewal fails,
see the [runbook playbook](operations/runbook.md#agent-certificate-renewal-failures).

### Bootstrap token rotation

`agent-bootstrap-token` authenticates *new* agents at first enrollment. To
rotate:

1. Create the new secret value and update the Swarm secret:
   `printf '%s' '<new-token>' | docker secret create agent-bootstrap-token.v2 -`
   then reference the new secret in the stack and redeploy (Swarm secrets are
   immutable; use a new secret name or `docker service update`).
2. Redeploy the stack so both control-plane and agent pick up the new value.
3. Running agents already hold valid certificates and are unaffected; only
   fresh enrollments need the token.
4. Remove the old secret after all nodes have re-enrolled or been refreshed.

## Master Key Management

The master key is the AES-256-GCM root key for the encrypted secrets store
(database rows in `secrets_store`), delivered via
`/run/secrets/hive-master-key` (`MASTER_KEY_FILE`). Every encrypted value —
registry passwords, SSH keys, CA material, client certificates — is encrypted
with a key derived from it via HKDF with per-purpose context.

Rules:

- Generate with high entropy: `openssl rand -hex 32`.
- **Back it up offline** (password manager / HSM-backed vault). Losing the
  master key makes every stored secret permanently undecryptable, including
  the internal CA.
- Never commit it, log it, or reuse it across environments.

### Rotation procedure

There is no automated rotation command yet. Rotation is a manual,
maintenance-window procedure, because values can only be re-encrypted from
plaintext:

1. **Inventory** the encrypted rows:
   ```sql
   select name, type, created_at from secrets_store order by name;
   ```
2. **Export plaintexts** using the old key — run a small Go snippet against
   `secrets.NewStore` with the current master key and `Get` each `(name,
   type)` pair into a temporary in-memory buffer. Do not write plaintexts to
   disk.
3. **Rotate the Swarm secret**:
   ```sh
   printf '%s' "$(openssl rand -hex 32)" | docker secret create hive-master-key.v2 -
   ```
   Update the stack to mount `hive-master-key.v2` as `hive-master-key` and
   redeploy.
4. **Re-import** each plaintext with `Put` under the new key (the store
   re-encrypts on write), then verify a representative value decrypts (e.g.
   registry "Test connection" in the UI, agent terminal session).
5. Remove the old secret and destroy any temporary buffers.

If you only need to re-key *one* value, decrypt and re-`Put` it after rotating
the key — all values share the master key, so a full pass is required for a
real rotation.

## Secret Encryption at Rest

- All sensitive values live in the `secrets_store` table, encrypted with
  AES-256-GCM; the data-encryption keys are derived from the master key via
  HKDF with distinct per-type contexts, so compromise of one ciphertext type
  does not yield keys for another.
- Plaintext secrets are never stored in Postgres and are materialized to disk
  only transiently (e.g. Docker secret values, TLS key files) on the nodes
  that need them.
- Docker-native secrets (`hive-master-key`, `postgres-password`,
  `hive-jwt-secret`, `agent-bootstrap-token`) are Swarm secrets, encrypted at
  rest by Docker on managers.

## RBAC

Organizations scope all resources; membership carries one of three roles
(`member_role` enum):

| Role | Capabilities |
|------|--------------|
| `owner` | Full control: delete organization, manage owners, everything below |
| `admin` | Manage members and invitations, manage projects/applications/infrastructure |
| `member` | Day-to-day operations: deploy, view logs, manage apps within granted projects |

Enforcement is server-side (`rbac.Require` checks on every mutating handler);
the UI merely mirrors it. API keys are organization-scoped and hashed at rest
(`token_hash`); use `X-Organization-Id` to select the acting organization.
Rotate API keys with the regenerate endpoint; delete unused keys.

## Audit Log

Security-relevant actions are recorded in the `audit_log` table (indexed by
resource for investigation). Review it after suspected incidents:

```sql
select created_at, action, resource_type, resource_id, actor
from audit_log order by created_at desc limit 100;
```

## Network Encryption

- **`hive_internal`** overlay is created with `--opt encrypted=true` (IPsec
  encryption of VXLAN traffic). All control-plane ↔ agent ↔ Postgres ↔
  registry ↔ BuildKit traffic rides this network. If you create it manually,
  always pass `--opt encrypted=true`.
- **`hive_proxy`** carries public HTTP/TLS traffic; TLS terminates at Traefik
  (Let's Encrypt, ACME state shared via `/data/shared/acme.json`).
- Postgres, registry, and BuildKit publish **no ports** to the host — they are
  reachable only over `hive_internal`. Do not add host port mappings.
- Agent RPC (9090) and health (9091) are likewise un-published; the control
  plane reaches agents by task IP over the encrypted overlay.

## Traefik Security Rules

Security rules (UI / `POST /api/v1/security-rules`) compile to Traefik v3
middleware labels on application services:

- `ip_allowlist` — Traefik `ipAllowlist` middleware with `sourcerange`.
  (`ip_blocklist` is rejected at creation: Traefik has no native blocklist
  middleware, and the previous allow-localhost-only approximation was a
  footgun. Use an external WAF or Traefik plugin for deny lists.)
- `rate_limit` — Traefik rate-limit middleware.
- `header_security` — custom response headers (HSTS, CSP, etc.).

Middlewares are attached to every domain router for the application. Note the
control-plane's own UI is exposed through the same Traefik; restrict port
8080 (direct mode) at the firewall if you don't intend to expose it.

## Known Limitations

- **Country block is not supported.** The `country_block` rule type exists in
  the schema, but enforcement requires a third-party Traefik plugin that is
  not bundled; creating such a rule has no effect at the proxy.
- **Registry passwords are encrypted at rest only.** They are decrypted in
  memory for builds; there is no per-secret vault, HSM integration, or
  in-memory mlocking.
- **No automated master-key rotation** — see the manual
  [rotation procedure](#rotation-procedure).
- The audit log is append-only in the database but has no export/tamper-proof
  shipping; ship DB backups off-cluster for retention.
