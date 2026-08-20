## Why

Frux lacks a durable, compact diagnostic record of the exact ranked cards Feed actually delivered. Short-lived served-candidate evidence remains the authorization boundary, while sampled request logs cannot provide complete delivery diagnostics or a trustworthy future data foundation.

## What Changes

- Persist one compact, long-retention diagnostic impression for 100% of final hydrated/readable recommendation cards actually delivered by Feed; this fact may support future training, but this change makes no current training claim.
- Freeze the downstream identity and time contract: trusted user/request/video identity plus delivery generation, zero-based absolute position within that generation, `author_id`, `published_at`, policy/version, bounded reasons/components, degraded state/providers, trusted `served_at`, durable `recorded_at`, and explicit record/feature schema versions.
- Couple delivery evidence and a durable diagnostic-impression handoff in one trusted transaction; process the handoff with a bounded leased worker and idempotent persistence/replay.
- State explicitly that delivered is not exposed and that a delivered card without validated exposure can never be interpreted as a negative example.
- Add privacy deletion and training-opt-out boundaries, independently configurable retention cleanup, migration registration, bounded metrics, reconciliation, and failure/retry behavior.
- Add release acceptance gates for storage amplification, Feed p99 overhead, backlog age, and end-to-end reconciliation.
- Preserve existing API responses and the current served-candidate expiry and attribution rules; training impressions never authorize feedback or outcomes.
- Define a shared identity/time contract that future explicitly activated consumers may use, without adding export, training, embeddings, learned ranking, exploration, or model serving.

## Capabilities

### New Capabilities

- `recommendation-training-impressions`: Durable, compact, privacy-bounded diagnostic facts for final recommendation cards actually delivered, including frozen identity/time semantics, reliable creation, privacy handling, reconciliation, retention, and observability.

### Modified Capabilities

None.

## Impact

- Recommendation domain/application contracts for delivered-card metadata, generation identity, durable handoff, worker processing, privacy handling, reconciliation, cleanup, and metrics.
- PostgreSQL recommendation persistence and migration registration for an outbox/handoff and compact diagnostic-impression fact table.
- Feed-to-recommendation delivery recording after final card hydration/readability filtering.
- API and worker composition roots and recommendation-focused unit, persistence, migration, API-flow, replay, and cleanup tests.
- No public API schema changes, no client-controlled analytics facts, no active training/export capability, and no change to the `recommendation_served_candidate` authorization or expiry window.
