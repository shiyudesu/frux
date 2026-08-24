## Why

The deployed mainland-China server can use the existing direct-IP HTTP public mode, but production
Compose does not start the multimodal Adapter, enable video jobs, or activate Session Semantic.
Frux needs an explicit production opt-in that keeps the paid model boundary private and independent
from the public site's domain/ICP/TLS situation.

## What Changes

- Add an optional `multimodal` profile to production Compose that runs the existing Adapter on the
  private backend network without publishing a host port.
- Keep `DASHSCOPE_API_KEY` exclusive to the Adapter; pass only Profile, HMAC, and the private endpoint
  to Worker, while API receives only Session runtime/full-policy settings.
- Add an explicit production-private HTTP provider flag that accepts only the exact internal Compose
  hostname and remains invalid for arbitrary/private/public hosts.
- Add an explicit production full-rollout flag that idempotently reconciles immutable v4=100% after
  Session runtime composition; development and production flags remain mutually environment-scoped.
- Extend the release deployer to validate multimodal environment invariants, enable/pull the optional
  profile, wait for Adapter and Worker health, and roll back the release on failure.
- Preserve the existing public `direct-http` mode for IP access, while documenting that HTTP/HTTPS or
  non-standard ports do not remove applicable ICP filing and provider requirements.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `multimodal-provider-runtime`: Allows an explicitly authorized exact private Compose HTTP boundary
  in production while keeping all other non-local endpoints HTTPS-only.
- `tongyi-multimodal-provider`: Adds a production profile service with health-gated startup and strict
  upstream-key isolation.
- `multimodal-video-embeddings`: Enables production Worker handoff/execution for newly published videos
  when the production multimodal profile is explicitly selected.
- `session-semantic-rollout`: Adds an explicit production full-rollout reconciliation path for v4.
- `ghcr-prod-deployment`: Extends immutable release deployment, validation, readiness, and rollback to
  the optional Adapter service.
- `simple-server-deployment`: Documents that public direct-IP HTTP and private Adapter transport are
  separate boundaries and that the Adapter is never publicly mapped.

## Impact

- Affects production configuration, Compose, deployment scripts/CI, multimodal HTTP validation,
  Session policy reconciliation, release documentation, and environment templates.
- Adds no public Adapter route, database schema, historical backfill, Query/Hybrid/Similar enablement,
  certificate bypass for arbitrary hosts, or claim that direct-IP HTTP avoids ICP obligations.
- Actual server activation still requires operators to place the existing DashScope key and a strong
  distinct multimodal HMAC in `/opt/frux/.env.prod`, then deploy the new release.
