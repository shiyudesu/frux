## Context

Prod currently assumes two DNS names on one public HTTPS port. Host Caddy distinguishes application and MinIO traffic by `Host`, then proxies both to loopback Compose ports. A single IP literal cannot provide that distinction on one port, and publishing only the Web container would also miss `/media` and `/health`, which Caddy currently sends directly to the API.

The deployment must preserve existing domain/HTTPS behavior for current operators, keep all control-plane and stateful services private, and retain exact-origin CORS and SigV4 request integrity. Direct IP access is therefore an explicit personal/pre-production mode rather than a new default.

## Goals / Non-Goals

**Goals:**

- Support `http://<ipv4>:<app-port>` for Web/API/media and `http://<ipv4>:<s3-port>` for browser-presigned MinIO requests.
- Preserve the existing `https://<app-domain>:<public-port>` and `https://<s3-domain>:<public-port>` Caddy deployment without requiring immediate environment migration.
- Publish only Web and the MinIO S3 API in direct mode; keep API, MinIO Console, PostgreSQL, Redis, Kafka, and Worker private.
- Make Compose rendering, application validation, MinIO verification, deployment health gates, and documentation agree on the selected mode.

**Non-Goals:**

- Claiming that IP access avoids ICP filing, cloud-provider policy, firewall, or access-control requirements.
- Providing TLS for an IP literal or weakening HTTPS requirements automatically.
- Exposing the MinIO Console or application credentials to browsers.
- Replacing the existing Caddy/DNS deployment for normal production use.

## Decisions

### Keep host and port variables backward compatible

`FRUX_DOMAIN`, `FRUX_S3_DOMAIN`, and `FRUX_PUBLIC_HTTPS_PORT` remain valid for the existing Caddy mode. New variables select the scheme, independent application and S3 public ports, public bind address, and HTTPS enforcement. Compose uses the legacy HTTPS port as the fallback for both new public port variables, so an existing `.env.prod` continues to render securely.

Alternative considered: replace the host and port variables with two complete origin strings. That is simpler internally but would break every existing server environment at the next release.

### Use two ports for one IP

Direct mode uses the same IPv4 literal for application and S3 access but requires distinct public ports. Web binds the application port and MinIO binds the S3 port. This preserves the unmodified S3 request path, query, method, and `Host` used by SigV4.

Alternative considered: proxy MinIO below an application path. That adds path-routing collisions and risks changing signed request inputs. Alternative considered: one IP and one port with Host routing. Both origins have the same Host and cannot be distinguished reliably.

### Publish only Web and MinIO S3 in direct mode

The configurable public bind address applies only to Web and the MinIO API mapping. API remains loopback for diagnostics because Web nginx already reaches it over the private frontend network. MinIO Console remains loopback-only. Web nginx gains `/media/` and `/health` proxy routes so it can act as the complete application entry point without Caddy.

### Keep insecure transport explicit

Domain mode defaults to `https` with public-HTTPS validation enabled and loopback bindings. Direct mode requires an explicit `http` scheme, disabled public-HTTPS enforcement, a public bind address, equal application/S3 IP literals, and distinct ports. The deployment script validates the selected access mode before accepting a release.

### Make route health checks mode-aware

The deployment agent retains Compose and Worker readiness checks. In Caddy mode it continues resolving the configured domain to local port 443. In direct mode it checks the Web-published host port through loopback, including `/`, `/health`, and the protected `/media` probe. Any failure triggers the existing rollback path.

## Risks / Trade-offs

- [Direct HTTP exposes credentials and content to network observers] → Keep it non-default, require explicit insecure settings, and document HTTPS or an overseas/non-mainland compliant endpoint for public use.
- [Publishing MinIO increases the externally reachable surface] → Publish only the S3 API, retain a private bucket, scoped application credentials, exact CORS, presigned object authorization, and a loopback-only Console.
- [Operators can configure public origins that do not match NAT mappings] → Validate mode invariants, document exact port mappings, and keep route and MinIO contract checks in acceptance steps.
- [Existing `.env.prod` files lack new variables] → Use secure compatibility defaults derived from `FRUX_PUBLIC_HTTPS_PORT`.
- [The deployment agent is server-owned and is not inside release bundles] → Update the installer/runbook and require operators enabling direct mode to install the matching script before changing `.env.prod`.

## Migration Plan

1. Deploy code and documentation while existing `.env.prod` continues using compatibility defaults.
2. Existing Caddy operators may add the new variables explicitly during a maintenance window, but do not need to change routing.
3. A direct-IP operator installs the updated deployment script, opens only the selected application and S3 ports, sets the explicit direct-mode variables, and runs Compose/MinIO validation before public cutover.
4. Rollback changes the mode back to Caddy HTTPS (or restores the previous `.env.prod`) and reruns the deployment agent; persistent volumes are unchanged.

## Open Questions

None.
