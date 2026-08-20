> **Superseded implementation plan (2026-08-20):** Do not apply this change. Its durable handoff
> principles are incorporated into `add-multimodal-video-discovery`; the old managed text-only
> provider prerequisite is no longer part of the active implementation path.

## Why

Frux needs semantic embeddings for newly published public videos, but a managed Embedding API is a
remote, rate-limited, billable dependency. Publication, Feed, Kafka progress, and
`hash-ngram-v1` must remain available when that provider is slow, unavailable, over quota, or
misconfigured. The integration therefore needs a durable handoff and independently retryable job
lifecycle rather than any online provider call.

## What Changes

- Depend on the provider-agnostic adapter, fixed
  `(provider, model, revision, dimension, semantic-text-v1)` contract, validation, privacy, cache,
  cost/quota, and circuit behavior from `add-semantic-embedding-service`.
- Keep publication/hash intake limited to idempotently creating or refreshing a PostgreSQL semantic
  job after the existing hash fact is safe; no provider API call occurs in the Kafka handler.
- Allow the Kafka source record to commit after durable semantic handoff. Provider API failure
  occurs later and never blocks publish, Feed, hash generation, or Kafka source progress.
- Define explicit `pending`, `leased`, `retry`, `succeeded`, and `terminal` states, stable claims,
  lease fencing, heartbeat/reclaim, bounded backoff plus `Retry-After`, manual requeue, and cleanup.
- Define source/retry/DLQ publication and commit boundaries so poison records and transient durable
  handoff failures remain replay-safe without coupling remote API retries to Kafka.
- Persist the complete provider, model, revision, dimension, canonicalizer, and canonical text hash
  on both jobs and semantic vector facts; credentials and raw canonical text are never persisted.
- Re-read and revalidate published/public source text immediately before provider access, then
  conditionally persist only the matching text hash and fenced lease generation.
- Keep `hash-ngram-v1` as the permanent fallback and preserve current recommendation behavior.
- Add a new-public-video availability SLA and rollout coverage gate with bounded backlog, terminal,
  provider cost/quota, and semantic coverage metrics.

## Capabilities

### New Capabilities

- `semantic-video-embeddings`: Defines hash-safe Kafka handoff, durable semantic job execution,
  provider-contract persistence, failure isolation, lease/retry/requeue/cleanup operations, and
  live semantic SLA/coverage acceptance for new public videos.

### Modified Capabilities

None.

## Impact

- Depends on archived recommendation prerequisites,
  `add-semantic-embedding-service`, and `migrate-video-workflows-to-kafka`.
- Affects Go embedding application/domain code, Kafka hash/publication handling, PostgreSQL job and
  vector persistence, worker composition, configuration, metrics, tests, and operational docs.
- Adds no public API, Web behavior, online Feed inference, model runtime/training, vector search,
  semantic profile, ranking/policy consumption, or historical scan.
- Provider outage changes semantic freshness only. Hash rows and existing product behavior remain
  independent.
