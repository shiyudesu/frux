## Why

Frux's live semantic projection intentionally does not force historical reconstruction. Most low-volume deployments can let per-user semantic profiles form naturally from new behavior, while operators that need older coverage require a bounded, resumable, explicitly invoked reconstruction workflow that matches live materialization without racing newer writes.

## What Changes

- Add an optional operator-only command that reconstructs long-term, recent, and negative semantic user-interest vectors for one exact pretrained model from durable eligible facts in stable bounded user/event pages; normal API/worker startup and deployments do not require it.
- Reuse the live semantic event schema, fixed signal weights, canonical `(occurred_at, source_kind_rank, source_event_id)` order, event-time decay, one-final-clamp reducer, profile schema, and exact event-time embedding identity defined by `project-semantic-user-interest`.
- Resolve and record the same embedding provider/model/revision, semantic text hash, and vector digest that the event represented when it occurred. Never rebuild an old event from a video's newer content embedding.
- Capture a deterministic source high-water mark, reconstruct through that fence, and perform bounded catch-up under the shared `(user_id, model)` serialization contract so finalization never overwrites a newer live profile version.
- Report and defer facts whose exact event-time embedding identity is absent, ambiguous, changed, or invalid; never record those facts as applied or publish a profile that silently omits them as complete.
- Support dry-run, bounded maximum users/events/runtime, cancellation, restart, atomically advanced checkpoints, one-user transactional replace/upsert, idempotent replay, and guarded force rebuilding scoped to the confirmed exact model.
- Add bounded progress, coverage, missing-vector, checkpoint, and outcome metrics plus safe periodic/final summaries.
- Add unit and PostgreSQL integration coverage for delayed and out-of-order facts, decay equivalence with live projection, missing vectors, interruption/restart, live-write races, idempotency, force guards, and model isolation.
- Explicitly allow small-user deployments to skip rebuild and gain semantic profiles from subsequent live events.
- Explicitly exclude changes to live projection behavior, pgvector or ANN storage/querying, recommendation recall/ranking/policy, individual or group model training, and author-affinity projection.

## Capabilities

### New Capabilities

- `semantic-user-interest-rebuild`: Defines optional deterministic historical selection, event-time-identity-preserving profile reconstruction, live-race safety, missing-vector/identity deferral, bounded operator controls, atomic resumability, observability, and verification.

### Modified Capabilities

None. The narrowed `semantic-user-interest` capability remains the source of live projection semantics and its existing future-rebuild boundary is consumed without changing live behavior.

## Impact

- Adds a Go operator command, rebuild orchestration, historical fact scans, run/checkpoint persistence, transactional profile replacement/catch-up support, metrics, tests, container entrypoint, and recommendation operations documentation.
- Depends explicitly on `integrate-semantic-video-embeddings` for versioned pretrained semantic rows and dimensional validation, optionally uses `backfill-semantic-video-embeddings` when historical event-time identities can be reproduced, and uses `project-semantic-user-interest` for event schema, fixed weights, canonical reduction, profile schema, and shared per-user/model serialization.
- Reads PostgreSQL source-of-truth facts and exact-model video embeddings; it adds no public API, Web behavior, semantic service calls, Redis or Kafka workflow, online profile consumer, main-spec edit, or behavior change to current recommendation delivery.
