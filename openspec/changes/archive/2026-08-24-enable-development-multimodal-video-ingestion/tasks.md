## 1. Configuration and Transport Boundary

- [x] 1.1 Add a strict optional `FRUX_MULTIMODAL_VIDEO_JOBS_ENABLED` override and Frux env-file allowlist coverage.
- [x] 1.2 Allow only the exact `multimodal-provider` HTTP hostname under local/test insecure-local configuration while retaining production HTTPS enforcement.
- [x] 1.3 Add configuration tests for blank/true/false/invalid video-job overrides, exact Docker hostname acceptance, arbitrary hostname rejection, and production rejection.

## 2. Development Compose Ingestion

- [x] 2.1 Add `docker-compose.multimodal.yml` with a health-gated Adapter service and Worker-only video-job/provider configuration.
- [x] 2.2 Ensure DashScope API key is injected only into Adapter while Worker receives only Profile, internal Endpoint, and HMAC.
- [x] 2.3 Add ignored local Compose metadata so this machine keeps the multimodal overlay active without committing credentials.
- [x] 2.4 Validate base Compose remains non-billable and overlay Compose fails closed when required local credentials are absent.

## 3. Real Runtime Verification

- [x] 3.1 Build and start the overlay, verify the real startup probe, Adapter health/metrics, and signed Worker readiness.
- [x] 3.2 Publish one eligible development video or execute an equivalent isolated real workflow through normal APIs.
- [x] 3.3 Verify the new video reaches succeeded Job, active-contract Fact, current Projection, and becomes eligible for Exact/Session Semantic retrieval.
- [x] 3.4 Verify API/Worker environments contain no DashScope key, model-call counters are bounded, and v4 remains enabled at100%.

## 4. Tests, Documentation, and Completion

- [x] 4.1 Add focused config/env/Compose/runtime tests and run real PostgreSQL tests, `go test ./...`, entrypoint builds, Docker build, and strict OpenSpec validation.
- [x] 4.2 Update README, embedding/recommendation/monitoring/product/roadmap documentation with the paid overlay command, cost boundary, rollback, and new-video semantics.
- [x] 4.3 Sync delta specs, archive the change, and leave the full local stack healthy with a clean worktree.
