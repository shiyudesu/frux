## Why

Frux needs a safe operator workflow to fill fixed-version semantic embeddings for historical videos that predate live semantic generation or were missed during outages. The backfill must be resumable and tightly bounded so catalog repair cannot overwrite unrelated models, process videos that are no longer publicly readable, or create an unbounded load on PostgreSQL or the semantic service.

## What Changes

- Add an operator-only command that scans videos which are published, public, media-ready, and missing the exact fixed semantic model in stable bounded pages.
- Reuse the semantic model identity, title/description canonicalization, bounded authenticated client, vector validation, and versioned embedding repository contract from `integrate-semantic-video-embeddings`.
- Batch semantic requests with bounded concurrency and batch size, then conditionally persist exact-model rows idempotently without changing hash embeddings or any other model.
- Add dry-run, maximum-row and maximum-runtime limits, context cancellation, deterministic ordering, progress summaries, and an opaque checkpoint cursor that is atomically replaced only after durable page progress.
- Skip existing exact-model rows by default. Permit refresh only with a guarded confirmation flag naming the exact semantic model, including stale-source and explicitly forced rows, while never deleting or rewriting other models.
- Validate the canonical title/description source hash before persistence and re-check lifecycle, visibility, and media readiness so videos changed during a run are safely skipped rather than written with stale or ineligible content.
- Add bounded metrics, safe error classes, operational/runbook documentation, unit and PostgreSQL/service integration tests, and a container-accessible command entrypoint.
- Explicitly exclude live event-processing changes, pgvector or ANN indexes, profile rebuilds, recommendation providers or policy changes, and model training.

## Capabilities

### New Capabilities

- `semantic-embedding-backfill`: Defines historical candidate selection, deterministic bounded execution, exact-model refresh safeguards, atomic checkpointing, freshness and eligibility revalidation, observability, operator controls, and verification.

### Modified Capabilities

None. The existing `semantic-video-embeddings` change already defines the reusable producer-side contracts and the one-way future backfill boundary; this change consumes that narrowed contract without changing live-event requirements.

## Impact

- Adds a Go operator command/composition entrypoint, backfill application orchestration, persistence scan/checkpoint support, configuration, metrics, tests, container wiring, and operational documentation.
- Depends explicitly on `add-semantic-embedding-service` for the fixed authenticated batch API and on the narrowed `integrate-semantic-video-embeddings` change for model identity `semantic-minilm-l12-v2@e8f8c211226b894f`, canonical source hashing, validated client behavior, conditional `(video_id, model)` persistence, and coverage interfaces.
- Reads historical video and embedding facts from PostgreSQL and calls the existing internal semantic service; it adds no public API, Web behavior, Kafka consumer, Redis state, schema for vector search, recommendation consumption, or main-spec edits.
