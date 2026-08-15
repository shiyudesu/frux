## Why

Frux's live semantic projection intentionally cannot recover eligible historical facts, disabled intervals, or prolonged missing-vector gaps, so existing users may have incomplete model-versioned semantic profiles. Operators need a bounded, resumable reconstruction workflow that produces the same profile state as live projection without racing newer live writes or broadening recommendation behavior.

## What Changes

- Add an operator-only command that reconstructs long-term, recent, and negative semantic user-interest vectors for one exact supported model from durable eligible facts in stable bounded user/event pages.
- Reuse the event eligibility, signal destinations and weights, deterministic ordering, event-time decay, profile schema, vector dimension validation, and exact-model embedding reads defined by the narrowed `project-semantic-user-interest` change.
- Capture a deterministic source high-water mark, reconstruct through that fence, and perform bounded catch-up under the shared `(user_id, model)` serialization contract so finalization never overwrites a newer live profile version.
- Report and defer facts whose exact-model semantic video embedding is missing or invalid; never record those facts as applied or publish a profile that silently omits them as complete.
- Support dry-run, bounded maximum users/events/runtime, cancellation, restart, atomically advanced checkpoints, one-user transactional replace/upsert, idempotent replay, and guarded force rebuilding scoped to the confirmed exact model.
- Add bounded progress, coverage, missing-vector, checkpoint, and outcome metrics plus safe periodic/final summaries.
- Add unit and PostgreSQL integration coverage for delayed and out-of-order facts, decay equivalence with live projection, missing vectors, interruption/restart, live-write races, idempotency, force guards, and model isolation.
- Explicitly exclude changes to live projection behavior, pgvector or ANN storage/querying, recommendation recall/ranking/policy, model training, and author-affinity projection.

## Capabilities

### New Capabilities

- `semantic-user-interest-rebuild`: Defines deterministic historical selection, exact-model semantic profile reconstruction, live-race safety, missing-vector deferral, bounded operator controls, atomic resumability, observability, and verification.

### Modified Capabilities

None. The narrowed `semantic-user-interest` capability remains the source of live projection semantics and its existing future-rebuild boundary is consumed without changing live behavior.

## Impact

- Adds a Go operator command, rebuild orchestration, historical fact scans, run/checkpoint persistence, transactional profile replacement/catch-up support, metrics, tests, container entrypoint, and recommendation operations documentation.
- Depends explicitly on `integrate-semantic-video-embeddings` for exact revision-bearing semantic video rows and dimensional validation, `backfill-semantic-video-embeddings` for historical vector coverage and its missing-vector operational boundary, and the narrowed `project-semantic-user-interest` for profile schema, event semantics, decay, applied-event identity, and shared per-user/model serialization.
- Reads PostgreSQL source-of-truth facts and exact-model video embeddings; it adds no public API, Web behavior, semantic service calls, Redis or Kafka workflow, online profile consumer, main-spec edit, or behavior change to current recommendation delivery.
