## Context

Frux currently has two storage topologies:

- local Compose starts MinIO with predictable development credentials;
- Prod Compose omits MinIO and uses a private Rainyun bucket through a fixed endpoint.

The target host is a 24 GB NAT machine intended for a low-traffic demonstration deployment. It has no public 80, 443, or 22, but the provider can map allocated public high ports to private host ports. The deployment is fresh: existing PostgreSQL, Redis, Kafka, and Rainyun objects do not need to move.

The current Prod pull agent validates Caddy locally through `127.0.0.1:443`. The NAT mapping can preserve that behavior by forwarding one public high HTTPS port to local 443. Browser-visible URLs must nevertheless include the allocated public port.

This change overlaps the provider wording in `reduce-media-object-storage-egress`. That change must be completed and synchronized before this change is archived; the final requirements retain its v3 logical exposure and 30-minute cache behavior while replacing Rainyun-specific storage with private self-hosted MinIO.

## Goals / Non-Goals

**Goals:**

- Run Web, API, Worker, PostgreSQL, Redis, one Kafka broker, PostgreSQL backup, and private MinIO on one NAT host.
- Serve the application and S3 API through two DNS hostnames sharing one mapped public HTTPS port.
- Keep Caddy on local 443 so the existing deployment agent's local route checks remain valid.
- Keep all data-plane services private and expose only the NAT-mapped HTTPS and SSH entry points.
- Use separate MinIO root and Frux application credentials.
- Preserve browser direct upload, signed public playback, Range, HEAD, ETag, and v3 exposure behavior.
- Provide deterministic fresh-host bootstrap, validation, cutover, and rollback instructions.

**Non-Goals:**

- Migrate the existing database, Kafka offsets, Redis state, or Rainyun objects.
- Remove Rainyun support as a historical provider from old releases.
- Disable Worker, FFmpeg, remuxing, or transcoding.
- Add high availability, multi-node MinIO, replicated Kafka, CDN delivery, or zero-downtime data migration.
- Expose the MinIO Console publicly.

## Decisions

### Forward one public high HTTPS port to local 443

The operator configures:

```text
public <https-port>/tcp -> host 443/tcp
public <ssh-port>/tcp   -> host 22/tcp
```

`FRUX_DOMAIN` and `FRUX_S3_DOMAIN` remain bare hostnames. A required
`FRUX_PUBLIC_HTTPS_PORT` is used only in browser-visible application and presign URLs. Caddy and the
deployment agent continue to use local 443.

Alternative: make Caddy and the deployment agent listen on a local high port. Rejected because it
requires unnecessary pull-agent changes and creates separate local/public port handling in health
checks.

### Use DNS-01 certificates

One certificate covers the application and S3 hostnames. Caddy loads the certificate and key on
local 443. Renewal uses the DNS provider's API because HTTP-01 and TLS-ALPN-01 cannot reach public
80/443 on the NAT host.

Alternative: use a tunnel or CDN proxy to regain public 443. Rejected for the default plan because
browser uploads can reach 512 MB and video delivery should not depend on an unselected proxy's body,
traffic, or media policies.

### Route two hostnames through one Caddy listener

```text
https://app.example:<public-port> -> NAT -> Caddy :443
  app host -> 127.0.0.1:18080 / 127.0.0.1:18081

https://s3.example:<public-port> -> NAT -> Caddy :443
  s3 host  -> 127.0.0.1:19000
```

Caddy preserves the original S3 Host, path, and query string so AWS Signature V4 validation remains
valid. The MinIO Console binds only to `127.0.0.1:19001` and is reached, when necessary, through an
SSH tunnel.

Alternative: route MinIO below the application hostname. Rejected because overlapping application
and bucket paths complicate signing, CORS, and reverse-proxy ownership.

### Separate internal and browser-facing S3 endpoints

API and Worker use `http://minio:9000` on the Compose backend network. Presigned browser requests use
`https://${FRUX_S3_DOMAIN}:${FRUX_PUBLIC_HTTPS_PORT}`. Path-style addressing remains enabled.

This avoids sending Worker traffic through NAT and TLS while ensuring every browser receives a
reachable signed URL.

### Add MinIO to Prod Compose with explicit initialization

Prod Compose adds:

- a pinned MinIO server image;
- a persistent `minio_data` volume;
- loopback API and Console bindings;
- a health check;
- an idempotent `minio-init` job;
- a private bucket;
- an application user restricted to the configured bucket;
- exact browser CORS for the configured application origin.

`FRUX_MINIO_ROOT_USER` and `FRUX_MINIO_ROOT_PASSWORD` are used only by the MinIO server and init job.
API and Worker continue to receive `FRUX_S3_ACCESS_KEY` and `FRUX_S3_SECRET_KEY`. The init job creates
or updates the application user and least-privilege policy without enabling anonymous access. The
policy permits only required Bucket listing/location and object operations under `uploads/`,
`processed/`, `moderation/`, and `media/`; it excludes Bucket policy, anonymous access, CORS, and
administrative mutations. A root-managed marker records the current application Access Key so
changing that key revokes the previous managed identity instead of leaving old credentials active.

Alternative: use MinIO root credentials in API and Worker. Rejected because compromise of an
application container would grant server-wide MinIO administration.

### Add an explicit MinIO deployment health gate

API and Worker depend on successful `minio-init`, which depends on a healthy MinIO server. The
deployment agent also checks the MinIO container directly before accepting API, Web, Worker, and
PostgreSQL backup readiness. A MinIO startup or initialization failure therefore fails the release,
and a later MinIO health failure cannot be hidden behind an otherwise healthy API process.

The deployment agent preserves `minio_data` during image rollback in the same way it preserves the
other named volumes.

Alternative: rely only on startup `depends_on`. Rejected because Compose dependencies do not make a
running API health check reflect later object-storage failure.

### Treat this as a fresh deployment

The new host receives new secrets, empty database and broker volumes, and an empty MinIO bucket.
Nothing is copied from Rainyun or the old host. The old deployment remains running during
validation and remains stopped-but-intact for at least 72 hours after cutover.

Because the public application URL contains a high port, documentation and repository links must
use the complete origin including that port.

### Keep the current media processing contract

The Worker image still contains FFmpeg. Compatible H.264/AAC sources use the existing stream-copy
path; incompatible sources continue to normalize through the existing processing profile.

Removing FFmpeg or treating the uploaded original as a ready baseline requires a separate media
state-machine change and is not safe as a deployment-only toggle.

## Risks / Trade-offs

- **Public URLs include a non-standard port** -> Update all published links and test desktop and
  mobile networks before cutover.
- **DNS-01 renewal depends on DNS API credentials** -> Restrict the token to the required zones,
  store it outside the repository, and monitor certificate expiry.
- **One host owns application state and media** -> Keep PostgreSQL backups, preserve MinIO data on a
  durable disk, and configure provider snapshots or an external object mirror before relying on the
  deployment for non-demo data.
- **Caddy proxy changes can invalidate S3 signatures** -> Preserve Host, path, query, and method and
  validate signed PUT and GET through the public hostname.
- **MinIO CORS can block direct upload** -> Configure the exact application origin including the
  public port and test all signed upload headers.
- **Single-disk MinIO is not highly available** -> Document that this remains a personal/demo
  topology and makes no durability claim beyond the host and its backups.
- **Overlapping active media changes can overwrite requirements** -> Archive or synchronize
  `reduce-media-object-storage-egress` first and validate the combined specs before implementation
  completion.
- **Rollback after accepting new writes loses fresh-host data** -> Keep the validation window short,
  take a final PostgreSQL backup before rollback, and explicitly accept that a full return to the
  old deployment does not merge new data.

## Migration Plan

1. Complete and synchronize the active media-egress change so v3 exposure and cache requirements
   are the baseline.
2. Implement and validate Prod Compose, configuration, CI, and documentation changes.
3. Allocate fixed NAT mappings from a public HTTPS high port to local 443 and from a public SSH high
   port to local 22.
4. Create DNS records for the application and S3 hostnames.
5. Issue a DNS-01 certificate containing both hostnames and configure host Caddy on local 443.
6. Install Docker, Compose, Caddy, curl, and the existing GHCR pull agent on the fresh host.
7. Create `/opt/frux/.env.prod` with new application, database, Redis, MinIO root, and MinIO
   application secrets.
8. Publish and approve the release, then run the pull agent to create empty persistent volumes and
   start the full stack.
9. Validate private bucket behavior, direct upload, processing, publication, signed playback,
   Range/HEAD, restart persistence, and PostgreSQL backup.
10. Update public links to the high-port application origin and switch users to the new deployment.
11. Retain the old host and Rainyun bucket unchanged for at least 72 hours.
12. Roll back by restoring the previous public link/DNS destination if acceptance fails; do not
    attempt to merge fresh-host writes into the old deployment.

## Open Questions

None. Exact hostnames and allocated public ports remain operator-provided environment values.
