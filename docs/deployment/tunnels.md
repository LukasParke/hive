# Cloudflare Tunnels

Hive can expose services running inside the swarm to the public internet
through [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/)
(cloudflared) without opening inbound ports. Each tunnel is fully managed
from the control plane: it is created through the Cloudflare API, its
connector runs as a swarm service, and DNS CNAME routes are published
automatically for every ingress hostname.

## Overview

Creating a tunnel from Hive performs these steps:

1. A named tunnel is provisioned via `POST /accounts/{id}/cfd_tunnel` on
   the Cloudflare REST v4 API.
2. The connector credentials JSON is stored **encrypted at rest** in Hive's
   secret store (`secrets_store`, AES-GCM with a key derived from the
   master key). The Cloudflare API token submitted with the request is
   encrypted the same way; neither value is ever returned by the API.
3. The cloudflared config file is rendered from the ingress rules
   (ordered `[[ingress]]` blocks plus an implicit catch-all
   `http_status:404`) and mounted into the connector container together
   with the credentials as Swarm secrets
   (`hive-tunnel-<name>-cred` / `hive-tunnel-<name>-config-rN`).
4. A single-replica service `hive_tunnel_<name>` (image
   `cloudflare/cloudflared:latest`) is deployed on the `hive_internal`
   overlay network so the connector reaches origins such as
   `http://traefik:80`.
5. If a zone ID was supplied, every ingress hostname gets a proxied CNAME
   record `<hostname> -> <tunnel-id>.cfargotunnel.com`. Record IDs are
   tracked per hostname so updates and deletes reconcile routes
   incrementally.

## Prerequisites

- A Cloudflare account with at least one zone (the domain whose hostnames
  should be routed).
- An API token with **Zone → DNS → Edit** (for automatic CNAME publishing)
  and **Account → Cloudflare Tunnel → Edit** permissions.
  - Without a zone ID, tunnels still work — you manage DNS records
    yourself or route only from hosts already on Cloudflare.
- `HIVE_MASTER_KEY` configured on the control plane. Without it no
  encryption-at-rest is available and tunnel creation fails fast rather
  than storing plaintext tokens.

## Wildcards

Ingress rules accept exact hostnames (`app.example.com`) and single-level
wildcards (`*.example.com`). Two pieces make wildcards work end to end:

- **cloudflared ingress**: a `hostname = "*.example.com"` rule matches any
  subdomain of the zone at the connector level.
- **DNS**: Hive publishes a wildcard CNAME record (`*.example.com`) just
  like an exact-hostname record — the Cloudflare API supports this natively.

Wildcard TLS certificates are issued automatically by Cloudflare's edge,
so no certificate management is needed in the cluster.

> Note: if you need certificates for origins *inside* the cluster (e.g.
> `https://backend.internal:8443` upstreams), use Hive's ACME support with
> a DNS-01 challenge instead of HTTP-01 — HTTP-01 cannot reach origins
> behind the tunnel before routing exists.

## Security model

- **API token**: submitted once per create call over TLS, encrypted with
  the cluster master key and stored in `secrets_store`. It is never
  included in API responses, never logged, and purged when the tunnel is
  deleted.
- **Connector credentials** (`<account>/<tunnel>/<secret>` JSON): stored
  encrypted under the name `tunnel:<cf-tunnel-id>` and additionally
  distributed to the swarm as the `hive-tunnel-<name>-cred` secret, which
  only the tunnel's own service can read.
- **Rendered config**: versioned as `hive-tunnel-<name>-config-rN` swarm
  secrets; stale revisions are pruned after every successful redeploy.
- **RBAC**: every `/api/v1/tunnels*` endpoint requires organization owner
  or admin role. All mutating operations write audit-log entries.

## Troubleshooting

| Symptom | Likely cause / fix |
| --- | --- |
| Create returns `502 runtime_error` | Check the control-plane logs for the embedded Cloudflare error snippet — usually an expired/under-scoped token or duplicate tunnel name in the account. |
| Tunnel shows `deployed` but connector has `0/N` replicas | Inspect `docker service ps hive_tunnel_<name>`; common causes are the `hive_internal` network missing or the image not pullable. |
| Hostname resolves but returns Cloudflare 1033 | No healthy replica is connected. Verify credentials secret exists and cloudflared logs show `Registered tunnel connection`. |
| Wildcard route does not resolve | Confirm the wildcard CNAME exists in the Cloudflare dashboard and the ingress rule uses the `*.` prefix exactly once. |
| Update ingress did not take effect | Ingress replacement rotates the config secret revision and restarts the service; if the swarm update failed the old revision stays active — retry after fixing the swarm error. |
| Delete fails with `502` | The upstream delete is authoritative: if Cloudflare refuses (tunnel already gone), remove the row manually after verifying the tunnel no longer exists in the Cloudflare dashboard. |
