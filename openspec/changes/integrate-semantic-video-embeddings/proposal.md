## Why

After recommendation-roadmap steps 1-5 are complete, Frux needs to generate semantic vectors for
newly published videos without weakening the existing hash fallback or coupling remote inference
retries to Kafka delivery. The video Kafka migration provides the replayable publication stream;
this roadmap step adds a durable, failure-isolated semantic projection on top of it.

## What Changes

- Require recommendation-roadmap steps 1-5 and `migrate-video-workflows-to-kafka` to be completed
  and archived before implementation starts.
- Add a bounded authenticated Go client for the fixed contract provided by
  `add-semantic-embedding-service`.
- Extend the existing Kafka `frux.video.published.v1` hash-embedding intake so it persists
  `hash-ngram-v1` first, then durably upserts a PostgreSQL semantic job before committing the Kafka
  offset.
- Execute semantic jobs through bounded PostgreSQL leases and database-owned retry timing; Kafka is
  the publication source, not the semantic retry scheduler.
- Persist fixed-version 384-dimensional semantic vectors beside `hash-ngram-v1` in the existing
  video embedding store.
- Add bounded configuration, observability, Compose wiring, tests, rollout, and rollback behavior
  that isolate semantic-service outages from hash generation and unrelated workers.
- Explicitly exclude additional Kafka retry topics or compatibility headers, historical backfill, semantic user profiles,
  pgvector/ANN recall, recommendation ranking changes, and media lifecycle work.

## Capabilities

### New Capabilities

- `semantic-video-embeddings`: Defines roadmap-gated Kafka intake, hash-first durable handoff,
  PostgreSQL semantic jobs, fixed-model persistence, failure isolation, and verification for new
  video semantic embeddings.

### Modified Capabilities

None.

## Impact

- Depends on archived recommendation changes
  `persist-recommendation-training-impressions`,
  `export-recommendation-training-dataset`,
  `evaluate-recommendation-policies-offline`,
  `learn-recommendation-policy-weights`, and
  `add-semantic-embedding-service`.
- Also depends on archived `migrate-video-workflows-to-kafka` and its retained
  `frux.video.published.v1` topic, independent Feed/hash consumer groups, and durable publication
  recovery boundary.
- Affects the Go embedding application/domain code, semantic HTTP client, Kafka embedding handler,
  PostgreSQL embedding and semantic-job persistence, worker composition, configuration, metrics,
  Compose, tests, and embedding/video operational documentation.
- Uses model `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` at revision
  `e8f8c211226b894fcb81acc59f3b34ba3efd5f42`, with persistence key
  `semantic-minilm-l12-v2@e8f8c211226b894f`.
- Adds no public API or Web behavior and does not change recommendation recall, ranking, policies,
  training, or the `hash-ngram-v1` fallback.
