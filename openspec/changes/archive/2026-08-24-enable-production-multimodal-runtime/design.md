## Context

Frux already supports direct-IP HTTP as an explicit personal/pre-production public mode and now has a
working development Adapter/video-ingestion composition. Production Compose, however, does not run
the Adapter, and the HTTP provider client rejects the internal Docker hostname under the production
runtime. The public access protocol and the private model transport are independent concerns: the
Adapter can remain unexposed on the backend network regardless of whether the public site uses a
domain with HTTPS or an IP with HTTP.

## Goals / Non-Goals

**Goals:**

- Add an explicitly enabled production Adapter service with no host port.
- Enable new-video jobs and Session Semantic v4=100% through validated production environment flags.
- Keep DashScope credentials exclusive to Adapter and retain signed internal requests.
- Integrate Adapter health into immutable release deployment and rollback.
- Support both existing Caddy HTTPS and direct-IP HTTP public modes without coupling either to Adapter
  transport.

**Non-Goals:**

- Claiming that HTTP, IP literals, or alternate ports avoid ICP filing obligations.
- Exposing Adapter or Worker metrics publicly.
- Enabling Query Embedding, Hybrid Search, Similar Videos, historical backfill, or model training.
- Providing a general production plaintext-provider exception.

## Decisions

### 1. Add an optional production Compose profile

`docker-compose.prod.yml` gains `multimodal-provider` under profile `multimodal`. The service reuses the
digest-pinned API image and provider binary, receives the upstream key only in its own environment,
binds `0.0.0.0:8099` inside the backend network, and publishes no host port. Worker declares an optional
healthy dependency and restarts until the Adapter is ready when the profile is active.

`FRUX_MULTIMODAL_DEPLOYMENT_ENABLED=true` controls whether the deploy script adds `--profile
multimodal`. This is separate from application feature flags so release validation can reject partial
configurations before Compose mutation.

### 2. Permit exact private-network HTTP with an explicit production flag

Add `provider.allow_insecure_private_network` and
`FRUX_MULTIMODAL_ALLOW_INSECURE_PRIVATE_NETWORK`. It permits HTTP only when the hostname is exactly
`multimodal-provider`. `allow_insecure_local` remains local/test-only, the two flags are mutually
exclusive, and arbitrary hostnames still require HTTPS.

Alternative considered: expose Adapter on host loopback. Rejected because Worker would need a host
gateway dependency and the service would leave the Compose-private trust boundary.

### 3. Keep API and Worker environments separate

The shared API/Worker environment contains only Profile, parent runtime, Session runtime, and the
production full-rollout flag. Worker alone receives video jobs, private Adapter Endpoint, transport
HMAC, and private-network HTTP authorization. Adapter alone receives `DASHSCOPE_API_KEY`. This prevents
the application API from receiving either model credentials or an inference endpoint it does not use.

### 4. Reconcile v4 only under an explicit production flag

Add `session.production_full_rollout_enabled`, valid only for staging/production and mutually exclusive
with the development flag. After Session runtime composition, API calls the same immutable v4
reconciler. An absent compatible v4 is created and activated; a conflict fails startup; other semantic
targets are exact-disabled. This makes server deployment reproducible without shipping a separate
rollout binary or Shadow file.

### 5. Make deployment validation and rollback multimodal-aware

The deployment script validates all required multimodal values as a closed set. When enabled it pulls
the Adapter service, starts the profile, waits for Adapter health before Worker readiness, and includes
the service during rollback. Bundle and CI checks assert that Adapter has no published port and that
the API/Worker environments do not contain the DashScope key.

## Risks / Trade-offs

- **[Private HTTP traffic can be observed by a compromised container/network]** → The hostname is exact,
  the network is private, no host port is published, and every body is HMAC-authenticated. Stronger
  internal TLS can be added independently later.
- **[Production startup incurs a model probe]** → Profile activation is explicit and health-gated;
  disabling it removes the Adapter without affecting stored vectors or baseline Feed.
- **[Automatic v4 activation is too broad]** → It requires a production-only explicit flag, retains
  exact disable, and fails on policy conflict instead of overwriting.
- **[Public direct-IP HTTP is intercepted]** → Keep it documented as insecure personal/pre-production
  access; this change does not weaken public transport validation or claim regulatory exemption.

## Migration Plan

1. Deploy code with all new production variables blank/false; behavior remains unchanged.
2. On the server set the multimodal deployment/profile/key/HMAC/runtime/video/session/private-network/
   production-full flags as one maintenance change.
3. Deploy; wait for Adapter startup probe, Worker readiness, API readiness, and v4 reconciliation.
4. Publish a small test video and verify Job/Fact/Projection before normal use.
5. Roll back by setting deployment/runtime/video/session/full flags false and redeploying; stored vectors
   and policy rows remain, while v4 should be exact-disabled if recommendation must stop immediately.

## Open Questions

None. ICP filing and any later public HTTPS migration remain operational/legal work outside the
private Adapter deployment.
