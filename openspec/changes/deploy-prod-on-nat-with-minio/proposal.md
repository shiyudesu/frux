## Why

The current Prod deployment assumes a host with public ports 80/443 and an external Rainyun bucket. The target demonstration host is a high-port NAT machine with ample memory but no public 80/443/22, so Frux needs a supported single-host topology that serves Web/API and a private MinIO bucket through DNS-validated HTTPS on mapped high ports.

## What Changes

- Add self-hosted MinIO and bucket initialization to the single-server Prod Compose stack while keeping the bucket private and persistent.
- Separate the internal S3 endpoint from the browser-facing presign endpoint so API and Worker use the Compose network while browsers use a dedicated HTTPS storage hostname and mapped public port.
- Support a configured public HTTPS port in application and object-storage URLs while NAT forwards that port to the host's local Caddy port 443.
- Define host Caddy routing for two hostnames on the existing local 443 listener: one for Frux Web/API and one for MinIO S3 traffic.
- Require DNS-01 certificate issuance because HTTP-01 and TLS-ALPN-01 cannot work without public ports 80/443.
- Preserve loopback-only host bindings for Web, API, MinIO API, and MinIO Console; PostgreSQL, Redis, Kafka, and Worker remain unexposed.
- Extend deployment validation to cover MinIO health, private-bucket behavior, direct-upload CORS, signed playback redirects, Range requests, persistence, and backup.
- Document a fresh-deployment cutover that does not copy the existing PostgreSQL, Kafka, Redis, or Rainyun data and retains the old deployment temporarily for rollback.
- Keep the current Worker and FFmpeg processing path unchanged; disabling or bypassing media processing is outside this change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `simple-server-deployment`: Change the personal/pre-production single-server topology from external Rainyun storage and standard HTTPS ingress to bundled private MinIO with mapped high-port NAT ingress.
- `production-media-delivery`: Generalize the production S3-compatible storage contract from Rainyun-specific behavior to private self-hosted MinIO with separate internal and public endpoints, explicit CORS, and provider verification.
- `ghcr-prod-deployment`: Include the required MinIO service in the controlled release and health-gated rollback unit without weakening digest pinning, secret ownership, or rollback behavior.

## Impact

- Affected deployment files: `apps/docker-compose.prod.yml`, `apps/.env.prod.example`, `apps/api/configs/config.prod.yaml`, `scripts/prod-deploy.sh`, deployment workflow validation, and related tests.
- Affected infrastructure: host Caddy, DNS records, DNS-01 certificate automation, NAT port mappings, Docker volumes, PostgreSQL backups, and MinIO storage backup.
- Affected documentation: deployment architecture, Prod operations, media delivery, object storage, security, and rollback procedures.
- No public API shape, frontend route, database schema, Kafka contract, or media-processing state-machine change is intended.
