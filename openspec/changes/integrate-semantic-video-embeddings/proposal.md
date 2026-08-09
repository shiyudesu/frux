## Why

The active `add-semantic-embedding-service` change defines a reproducible semantic model service but intentionally leaves Frux without a caller or durable semantic video vectors for newly published content. This change integrates that contract into the live video-published path while preserving the local hash n-gram vector as the always-available fallback and deferring recommendation use and historical coverage.

## What Changes

- Add a bounded authenticated Go client for the internal semantic embedding service, including startup metadata validation, strict deadlines and connection limits, exact response contract checks, and safe error classification.
- Extend worker configuration and composition so hash embeddings are always generated and semantic generation is independently enabled for the fixed service model/revision.
- Make Kafka video-publication intake persist hash coverage first and durably hand semantic generation to a PostgreSQL job keyed by `(video_id, model)`.
- Persist finite L2-normalized semantic vectors beside `hash-ngram-v1` in the existing `video_embedding` table under a fixed revision-bearing model key.
- Add bounded-cardinality metrics for semantic requests, live-event vector outcomes, semantic-job retries and suspension, semantic coverage, and PostgreSQL backlog.
- Add configuration, Compose dependency wiring, migration assessment, failure-mode documentation, and unit, contract, persistence, worker, and Compose integration tests.
- Explicitly defer all historical/resumable scanning, command/job, cursor/checkpoint, dry-run, and re-embedding behavior to a future separate change named `backfill-semantic-video-embeddings`.
- Keep recommendation recall and ranking unchanged; `add-pgvector-recommendation-recall` may consume these stored vectors later.

## Capabilities

### New Capabilities

- `semantic-video-embeddings`: Defines safe live-event generation, retry, persistence, configuration, observability, and verification of fixed-version semantic embeddings for newly published videos.

### Modified Capabilities

None. The change consumes the planned `semantic-embedding-service` contract without extending it and does not change `contextual-recommendation` behavior.

## Impact

- Depends on `migrate-video-workflows-to-kafka` and its retained publication topic, independent embedding group, and durable semantic-job handoff.
- Affects the Go embedding domain/application packages, Kafka publication intake, PostgreSQL embedding and semantic-job repositories, worker composition, configuration, metrics, Compose wiring, tests, and embedding/video operational documentation.
- Depends explicitly on the active `add-semantic-embedding-service` change and its fixed authenticated metadata/embedding API for `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` revision `e8f8c211226b894fcb81acc59f3b34ba3efd5f42` with 384-dimensional normalized output.
- Uses the existing `video_embedding(video_id, model)` identity and JSONB vector storage; no schema change, pgvector column, or ANN index is expected.
- The future `backfill-semantic-video-embeddings` change will depend on this change's fixed model identity, canonicalization, validated client, conditional persistence, and coverage metrics, but will own all historical selection, resumability, operator controls, and backfill-specific retry behavior. This change neither depends on that future consumer nor backfills existing historical videos.
- Adds no public API or Web behavior and does not add recall providers, ranking inputs, policy changes, online inference in request paths, training, or removal of the hash fallback.
