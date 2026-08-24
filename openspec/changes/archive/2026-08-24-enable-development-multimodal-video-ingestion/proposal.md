## Why

Development recommendation now selects Session Semantic v4 for every request, but newly published
videos still do not receive active-contract multimodal vectors because the Adapter and video-job
executor are not running. The development stack needs an explicit billable ingestion composition so
new public videos become semantically eligible without leaking the DashScope key into API or Worker.

## What Changes

- Add a strict environment override for `multimodal.video_jobs_enabled` while preserving YAML defaults.
- Add a Docker Compose multimodal overlay that starts the existing Tongyi Adapter, waits for its real
  startup probe, and enables the Worker video-job runtime against an internal service endpoint.
- Pass the DashScope API key only to the Adapter; pass only the selected profile and shared Frux HMAC
  to Worker, and keep API independent from upstream credentials.
- Permit plaintext provider transport only to the exact development Compose service hostname under a
  local/test runtime; production and arbitrary remote HTTP remain rejected.
- Configure this machine through ignored local Compose metadata so ordinary local Compose commands
  retain the multimodal overlay without committing credentials or forcing paid calls for new clones.
- Verify signed readiness, Worker startup, durable job execution, Fact/Projection persistence, and
  Session Semantic eligibility using a real newly published video or equivalent isolated acceptance run.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `multimodal-provider-runtime`: Adds a strict video-job environment switch and an explicitly
  allowlisted local Docker provider boundary while retaining remote HTTPS requirements.
- `tongyi-multimodal-provider`: Adds a health-gated Compose Adapter service with upstream credential
  isolation and real startup probing.
- `multimodal-video-embeddings`: Adds an explicit billable development composition in which new public
  videos automatically enter durable multimodal jobs and active-contract projection.

## Impact

- Affects multimodal configuration validation/env loading, development Compose, Adapter/Worker startup
  ordering, local ignored Compose configuration, documentation, and integration verification.
- Adds no database schema, historical backfill, query embedding, Hybrid Search, Similar Videos,
  production-default inference, training, or ANN index.
- Enabling the overlay performs one billable startup probe and normally one billable video embedding
  call per newly published eligible video, subject to the existing bounded retry policy.
