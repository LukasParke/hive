# Cloud Provider Notes

Concise guidance per provider. The networking requirements are identical
everywhere:

| Port | Protocol | Purpose |
|------|----------|---------|
| 22 | tcp | SSH admin (restrict to your IP) |
| 80 / 443 | tcp | HTTP/HTTPS ingress (Traefik) |
| 2377 | tcp | Swarm cluster management (managers only) |
| 7946 | tcp + udp | Swarm gossip (all nodes) |
| 4789 | udp | VXLAN overlay network traffic (all nodes) |

Also allow ICMP between nodes if you want path-MTU discovery for VXLAN, and
make sure the cloud security group applies to **node-to-node** traffic, not
just ingress from the internet.

## AWS

- **Instances**: managers `t3.large`+; builder node compute-optimized.
- **Volumes**: gp3 EBS for `hive_pgdata` and registry data. Named volumes are
  node-local — pin `db=true` to the EBS-backed node.
- **Shared storage**: use **EFS** mounted at `/data/shared` on all managers for
  ACME state (see [production.md](production.md#shared-storage-nfs-vs-s3-backed)).
  S3 is for backup destinations only, not the shared mount.
- **Security groups**: allow 2377/tcp among managers; 7946/tcp+udp and
  4789/udp among all nodes in the cluster SG.
- ECR works as an external registry — see
  [external-services.md](external-services.md#external-registries). Note ECR
  tokens expire; use long-lived IAM user keys as documented there.

## GCP

- **Instances**: `e2-standard-2`+ for managers.
- **Volumes**: `pd-balanced` persistent disks for Postgres/registry.
- **Shared storage**: **Filestore** (NFS) for `/data/shared`.
- **Firewall rules**: create VPC firewall rules for 2377/tcp, 7946/tcp+udp,
  4789/udp with the cluster's source tag/range; 80/443 from `0.0.0.0/0`.
- Artifact Registry as external registry: username `_json_key`, password = the
  service-account JSON.

## Hetzner

- **Instances**: CPX31/CX42 class for managers; CCX for the DB node.
- **Volumes**: Hetzner Cloud Volumes attach per-node — fine for node-local
  `hive_pgdata`/registry volumes.
- **Shared storage**: no managed NFS; run a small NFS server VM or use a
  dedicated storage node exporting NFS to the managers.
- **Firewall**: Hetzner Cloud Firewalls are stateful per-server; add the full
  port table above for both directions within your private network, or disable
  the cloud firewall and use host firewalls carefully (Docker bypasses ufw for
  forwarded traffic — rely on the cloud layer).

## DigitalOcean

- **Droplets**: `s-4vcpu-8gb`+ managers.
- **Volumes**: block storage volumes attach per-node for Postgres/registry.
- **Shared storage**: no managed NFS; options are a NFS droplet or a Ceph-based
  shared volume driver. For single-region small clusters, consider keeping one
  manager as the ACME entry point instead of sharing state.
- **Cloud firewalls**: allow 2377/tcp between droplets tagged `hive-manager`;
  7946/tcp+udp and 4789/udp between all `hive` droplets; 80/443 public.
