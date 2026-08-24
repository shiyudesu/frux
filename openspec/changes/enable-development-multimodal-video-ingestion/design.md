## Context

The API already consumes persisted active-contract vectors for100% development Session Semantic
recommendation. The Worker has a complete durable multimodal job runtime and the image already contains
the Tongyi Adapter binary, but development Compose starts neither the Adapter nor video jobs. The
repository-local `.env.multimodal` contains the selected profile, Frux HMAC, and DashScope API key;
those values must be distributed without giving the upstream key to API or Worker.

## Goals / Non-Goals

**Goals:**

- Make newly published development videos automatically produce active-contract Fact/Projection rows.
- Reuse the existing Adapter and bounded Worker job executor without changing application contracts.
- Keep DashScope credentials confined to the Adapter process.
- Preserve a non-billable base Compose for clones without credentials while keeping this machine's
  billable overlay active through ignored local configuration.
- Retain HTTPS-only behavior for every non-local provider boundary.

**Non-Goals:**

- Historical backfill, Query Embedding, Hybrid Search, Similar Videos, or production inference.
- Passing API keys to API/Worker, embedding private/draft media, or changing retry/cost bounds.
- Training, HNSW, or automatic production activation.

## Decisions

### 1. Use a Compose overlay for the billable path

Add `docker-compose.multimodal.yml` rather than making the base Compose require a paid API key. The
overlay starts `frux-multimodal-provider`, enables Worker video jobs, injects the internal endpoint,
and adds a health dependency. Base Compose remains usable without `.env.multimodal`.

This machine receives an ignored `apps/.env` containing only Compose metadata that selects the base
and overlay files and tells Compose to use `.env.multimodal` for interpolation. It contains no model
credential. Removing that local file returns to the non-billable base composition.

Alternative considered: add the Adapter directly to base Compose. Rejected because a fresh clone
would fail or perform an unexpected paid startup probe without credentials.

### 2. Keep credentials process-scoped

Only the Adapter uses Compose `env_file: .env.multimodal`, so only it receives `DASHSCOPE_API_KEY`.
Worker receives explicit Profile, internal Endpoint, and HMAC values through the overlay. API remains
Session-only and receives neither the upstream key nor the video-job provider settings.

Alternative considered: mount `.env.multimodal` into every container. Rejected because filesystem
access would still expose the upstream key to API/Worker even if their loaders ignored it.

### 3. Permit one exact local Docker HTTP hostname

The Adapter protocol already authenticates exact bodies in both directions. Development Docker uses
the private service hostname `multimodal-provider`, which is not a loopback literal. Configuration
will allow plaintext HTTP only for that exact hostname when `allow_insecure_local=true` and the root
runtime environment is local/test. Arbitrary hostnames and all staging/production HTTP remain invalid.

Alternative considered: add internal TLS and certificate distribution. Rejected for this local-only
boundary because it adds certificate lifecycle complexity without changing the external DashScope
HTTPS connection or production rules.

### 4. Add a strict video-job environment override

`FRUX_MULTIMODAL_VIDEO_JOBS_ENABLED` follows the existing strict optional boolean behavior: missing or
blank preserves YAML, valid values override, and invalid values fail startup. The overlay sets it only
for Worker. The publication consumer then performs the existing durable handoff before committing the
Kafka source record.

### 5. Verify with the real bounded path

Validation first proves the Adapter startup probe and signed Worker readiness. A real newly published
development video or isolated acceptance fixture then proves `pending/leased/succeeded`, authoritative
Fact, current Projection, and no credential leakage. Existing retries and cost counters remain
authoritative; the run does not enable Query/Hybrid/Similar.

## Risks / Trade-offs

- **[Starting the overlay incurs cost]** → The base Compose remains non-billable; the overlay and local
  Compose metadata are explicit, documented, and removable.
- **[Adapter probe or upstream call fails]** → Adapter stays unhealthy, Worker does not start video-job
  execution, and existing publication/Feed data remains durable.
- **[Internal HTTP is broader than intended]** → Only the exact `multimodal-provider` hostname is
  accepted, only in local/test, with signed request/response bodies and no published provider port.
- **[Repeated video events create duplicate charges]** → Durable job/source-hash idempotency and
  authoritative Fact checks preserve the existing no-duplicate behavior.

## Migration Plan

1. Add the video-job override and exact local Docker endpoint validation.
2. Add and validate the Compose overlay and ignored local Compose metadata.
3. Build the image and start Adapter/Worker through the overlay; wait for health/readiness.
4. Publish one eligible video and verify Job, Fact, Projection, and cost metrics.
5. Roll back by removing local Compose metadata or starting only base Compose; existing vectors remain
   readable and v4 recommendation continues with available coverage.

## Open Questions

None. Production inference remains a separate TLS/deployment change.
