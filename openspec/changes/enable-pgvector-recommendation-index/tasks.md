## 1. Dependency, Configuration, and Deployment Gates

- [ ] 1.1 Confirm the completed contracts from `integrate-semantic-video-embeddings` and `backfill-semantic-video-embeddings`, then centralize the exact semantic model key, dimension 384, normalization tolerance, and readable-video predicates consumed by infrastructure.
- [ ] 1.2 Add disabled-by-default pgvector configuration with validation for enablement, PostgreSQL/extension prerequisites, reconcile interval, batch size, per-cycle row limit, and deadline; reject model/dimension overrides and unbounded values.
- [ ] 1.3 Add explicit enabled deployment selection for pinned `pgvector/pgvector:0.8.1-pg17` in Compose and the PostgreSQL deployment manifest, including an approved production digest, while retaining `postgres:17.5-alpine` for disabled environments.
- [ ] 1.4 Add configuration/startup regression tests proving disabled API and worker paths never inspect extension catalogs, require vector privileges, run vector DDL, start reconciliation, or construct the ANN repository.

## 2. Extension and Additive Projection Migration

- [ ] 2.1 Implement enabled-only PostgreSQL 17, extension availability, installed pgvector `>=0.8.0`, fixed-model, dimension, and durable source-schema prerequisite validation with bounded safe errors.
- [ ] 2.2 Add the `semantic_video_ann_projection` persistence model and additive DDL for `(video_id, model)` identity, video delete cascade, `vector(384)`, source hash/update metadata, projection time, and schema-unique constraints.
- [ ] 2.3 Add the exact-model partial cosine HNSW index with `m=16` and `ef_construction=64`, plus the projection, source-embedding, and readable-video supporting B-tree indexes defined by the design.
- [ ] 2.4 Integrate extension/table/index creation into the existing advisory-locked migration only when enabled, including idempotent catalog-definition validation and a migration marker that is written only after complete success.
- [ ] 2.5 Add real PostgreSQL tests for clean and concurrent enabled migration, repeat migration, disabled migration on ordinary PostgreSQL, missing/old extension, wrong server major version, wrong model/dimension, exact catalog definitions, and preservation of durable source rows.

## 3. Projection Conversion, Reconciliation, and Metrics

- [ ] 3.1 Implement infrastructure-local JSON-to-vector conversion that defensively parses exactly 384 finite values, enforces norm tolerance, normalizes once, and keeps pgvector driver/value types inside infrastructure.
- [ ] 3.2 Implement stable bounded queries for missing/changed eligible sources and stale/ineligible projection rows, conditional exact-model upsert, and model-isolated deletion for source-missing, private, unpublished, deleted, or media-unready videos.
- [ ] 3.3 Implement the bounded reconciler with projection-specific advisory locking, per-batch transactions, configured interval/batch/row/deadline limits, idempotent replay, cancellation, invalid-source isolation, and skipped-lock behavior.
- [ ] 3.4 Compose and supervise reconciliation only in the enabled worker, with clean shutdown and no Redis, RabbitMQ, semantic-service, API, or existing worker-consumer behavior changes.
- [ ] 3.5 Add bounded coverage, reconciliation outcome, cycle result, and duration metrics plus safe logging tests that reject model/video/vector/SQL/raw-error label or field leakage.

## 4. Guarded Operator Projection Management

- [ ] 4.1 Add `cmd/manage-semantic-video-ann` as a PostgreSQL-only one-shot command with bounded reconcile, page, maximum-row, maximum-runtime, deadline, and dry-run options and no unlimited mode.
- [ ] 4.2 Implement dry-run and normal reconcile through the shared validation, advisory lock, selection, conversion, mutation, metrics, and safe summary paths, proving dry-run performs no database mutation.
- [ ] 4.3 Implement exact-model purge/rebuild and named concurrent HNSW reindex with exact confirmation guards, model/index isolation, cancellation, and rollback-safe behavior that never deletes durable embeddings, other models, the table, or the extension.
- [ ] 4.4 Add command tests for option bounds, safe summaries, lock contention, dry-run parity, missing/partial/wildcard confirmation rejection, bounded interruption, exact-model purge/rebuild, reindex confirmation, and unchanged source/other-model facts.

## 5. Narrow Bounded ANN Query Repository

- [ ] 5.1 Define infrastructure-owned plain-Go ANN query, neighbor, and repository types bound to the fixed model, and add dependency/package checks proving pgvector values, SQL, projection models, and extension metadata do not escape infrastructure.
- [ ] 5.2 Implement defensive input validation for exactly 384 finite normalized components, top-K `1..100`, at most 20 unique positive exclusions, required caller deadline, copied inputs, and a 500-millisecond effective deadline cap.
- [ ] 5.3 Implement the read-only ANN transaction with local `statement_timeout`, `hnsw.ef_search=100`, strict iterative scan, `hnsw.max_scan_tuples=10000`, exact-model/readable-video/exclusion/positive-cosine filters, and deterministic distance then video-ID ordering.
- [ ] 5.4 Add repository tests for every input rejection, top-K/exclusion bounds, exact-model isolation, positive finite scores, deterministic ties, live privacy/lifecycle/media filtering, smaller healthy result sets, caller cancellation, deadline timeout, no partial output, and result-order validation.

## 6. Real PostgreSQL Acceptance and Documentation

- [ ] 6.1 Add real PostgreSQL projection lifecycle tests for valid/invalid JSON, source updates, concurrent embedding/backfill writes, eligibility transitions, hard deletes, stale cleanup, advisory-lock serialization, replay, purge/rebuild, and model isolation.
- [ ] 6.2 Add a seeded fixture of at least 10,000 projections and an `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` acceptance test requiring the named HNSW index and rejecting a sequential projection scan for the production filtered query shape.
- [ ] 6.3 Add exact-cosine ground-truth comparison for at least 100 deterministic queries and enforce aggregate recall@20 of at least `0.90`, including exclusion and readability-filter cases.
- [ ] 6.4 Add the documented opt-in warm performance gate for 100 top-20 repository queries over at least 10,000 rows, requiring p95 at most 150 milliseconds and zero deadline failures while recording versions, resources, fixture, HNSW settings, warm-up, and latency summary.
- [ ] 6.5 Update PostgreSQL/deployment, semantic embedding, recommendation prerequisite, metrics, and operator runbooks with disabled behavior, prerequisites, migration, reconciliation, dry-run, guarded rebuild/purge/reindex, coverage verification, rollout, and rollback.
- [ ] 6.6 Run targeted config/migration/projection/query/command tests, compile `./cmd/feed`, `./cmd/worker`, and the operator binary, run `cd apps/api && go test ./...`, validate Compose, then run `openspec validate --all --strict` and confirm no application RecallProvider, policy/profile/shadow/ranking/training/external-vector-DB code or main-spec edits were added.
