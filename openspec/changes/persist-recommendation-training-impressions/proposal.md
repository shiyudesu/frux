## Why

Frux retains durable request-linked outcomes but lacks a long-lived, compact record of the exact ranked cards that users were actually sent. The existing served-candidate evidence is intentionally short-lived for security attribution, while the sampled full-pool request log is too large and sparse to serve as a complete training impression source.

## What Changes

- Persist one compact, long-retention training impression per final hydrated/readable recommendation card delivered by Feed.
- Record trusted user/request/video identity, absolute rank position, scene, policy version, bounded recall reasons and score components, served time, and explicit record/schema version metadata.
- Couple delivery evidence and a durable training-impression handoff in one trusted transaction; process the handoff with a bounded leased worker and idempotent persistence/replay.
- Add independently configurable retention cleanup, privacy bounds, migration registration, operational metrics, and failure/retry behavior.
- Preserve existing API responses and the current served-candidate expiry and attribution rules; training impressions never authorize feedback or outcomes.
- Define the durable fact that a later dataset-export change may consume, without adding export, evaluation, embeddings, learned ranking, exploration, or model serving.

## Capabilities

### New Capabilities

- `recommendation-training-impressions`: Durable, compact, privacy-bounded facts for final recommendation cards actually delivered, including reliable creation, idempotent replay, retention, and observability.

### Modified Capabilities

None.

## Impact

- Recommendation domain/application contracts for delivered-card metadata, durable handoff, worker processing, cleanup, and metrics.
- PostgreSQL recommendation persistence and migration registration for an outbox/handoff and training-impression fact table.
- Feed-to-recommendation delivery recording after final card hydration/readability filtering.
- API and worker composition roots and recommendation-focused unit, persistence, migration, API-flow, replay, and cleanup tests.
- No public API schema changes, no client-controlled analytics facts, and no change to the `recommendation_served_candidate` authorization or expiry window.
