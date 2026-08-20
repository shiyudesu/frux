## 1. Dependency, Configuration, and Deployment Gates

- [ ] 1.1 Confirm the completed contracts from `integrate-semantic-video-embeddings` and `backfill-semantic-video-embeddings`, then centralize the exact semantic model key, dimension 384, normalization tolerance, and readable-video predicates consumed by infrastructure.
- [ ] 1.2 Add disabled-by-default pgvector configuration with validation for enablement, PostgreSQL/extension prerequisites, reconcile interval, batch size, per-cycle row limit, query/reconcile lock and statement timeouts, exact-first `hnsw_min_rows`, maintenance concurrency, and capacity gates; reject model/dimension overrides and unbounded values.
- [ ] 1.3 Add explicit enabled deployment selection for pinned `pgvector/pgvector:0.8.1-pg17` in Compose and the PostgreSQL deployment manifest, including an approved production digest, while retaining `postgres:17.5-alpine` for disabled environments.
- [ ] 1.4 Add configuration/startup regression tests proving disabled API and worker paths never inspect extension catalogs, require vector privileges, run vector DDL, start reconciliation, or construct the ANN repository.

## 2. Extension and Additive Projection Migration

- [ ] 2.1 Implement enabled-only PostgreSQL 17, extension availability, installed pgvector `>=0.8.0`, fixed-model, dimension, and durable source-schema prerequisite validation with bounded safe errors.
- [ ] 2.2 Add the `semantic_video_ann_projection` persistence model and additive DDL for `(video_id, model)` identity, video delete cascade, provider/model/revision, `vector(384)`, source text hash/vector digest/update metadata, projection time, and schema-unique constraints.
- [ ] 2.3 Add only the projection, source-embedding, and readable-video supporting B-tree indexes to startup DDL; define the exact-model partial cosine HNSW catalog contract with `m=16` and `ef_construction=64` for operator maintenance.
- [ ] 2.4 Integrate extension/table/small-index creation into the existing advisory-locked migration only when enabled, with bounded lock/statement timeouts, idempotent catalog validation, a success marker, and an explicit prohibition on startup HNSW build/rebuild.
- [ ] 2.5 Add real PostgreSQL tests for clean and concurrent enabled migration, repeat migration, large populated projection with absent/stale HNSW, disabled migration on ordinary PostgreSQL, timeout failure, missing/old extension, wrong server major version, wrong model/dimension, exact catalog definitions, no automatic HNSW work, and preservation of durable source rows.

## 3. Projection Conversion, Reconciliation, and Metrics

- [ ] 3.1 Implement infrastructure-local JSON-to-vector conversion that defensively parses exactly 384 finite values, enforces norm tolerance, normalizes once, and keeps pgvector driver/value types inside infrastructure.
- [ ] 3.2 Implement stable bounded queries for missing/changed eligible sources and stale/ineligible projection rows, exact provider/model/revision/text-hash/vector-digest/update-time equality checks, conditional upsert, and model-isolated deletion for source-missing, private, unpublished, deleted, or media-unready videos.
- [ ] 3.3 Implement the bounded reconciler with a dedicated low-concurrency connection pool, projection advisory lock, per-batch transactions, lock/statement timeouts, configured interval/batch/row/deadline limits, idempotent replay, cancellation, invalid-source isolation, and skipped-lock behavior.
- [ ] 3.4 Compose and supervise reconciliation only in the enabled worker, with clean shutdown and no Redis, Kafka, semantic-service, API, or existing worker-consumer behavior changes.
- [ ] 3.5 Add bounded coverage, reconciliation outcome, cycle result, and duration metrics plus safe logging tests that reject model/video/vector/SQL/raw-error label or field leakage.

## 4. Guarded Operator Projection Management

- [ ] 4.1 Add `cmd/manage-semantic-video-ann` as a PostgreSQL-only one-shot command with bounded reconcile, page, maximum-row, maximum-runtime, deadline, and dry-run options and no unlimited mode.
- [ ] 4.2 Implement dry-run and normal reconcile through the shared validation, advisory lock, selection, conversion, mutation, metrics, and safe summary paths, proving dry-run performs no database mutation.
- [ ] 4.3 Implement exact-model purge/projection rebuild plus capacity-gated concurrent HNSW build/reindex on an independent connection with exact confirmations, row threshold, free-disk/WAL/CPU/connection gates, one maintenance permit, bounded lock/statement timeouts, cancellation, invalid-index cleanup, and no automatic HNSW build after projection rebuild.
- [ ] 4.4 Add command tests for option bounds, safe summaries, reconcile/maintenance lock contention, dry-run parity, missing/partial/wildcard confirmation rejection, every capacity-gate failure, bounded interruption, exact-model purge/rebuild, HNSW build/reindex confirmation, invalid-index cleanup, and unchanged source/other-model facts.

## 5. Narrow Bounded ANN Query Repository

- [ ] 5.1 Define infrastructure-owned plain-Go ANN query, neighbor, and repository types bound to the fixed model, and add dependency/package checks proving pgvector values, SQL, projection models, and extension metadata do not escape infrastructure.
- [ ] 5.2 Implement defensive input validation for exactly 384 finite normalized components, top-K `1..100`, at most 20 unique positive exclusions, required caller deadline, copied inputs, and a 500-millisecond effective deadline cap.
- [ ] 5.3 Implement the read-only query transaction with local lock/statement timeouts, exact cosine below threshold or without accepted HNSW, HNSW settings above threshold, authoritative embedding equality, exact-model/readable-video/exclusion/positive-cosine filters, and deterministic distance then video-ID ordering.
- [ ] 5.4 Add repository tests for every input rejection, top-K/exclusion bounds, exact-model isolation, positive finite scores, deterministic ties, live privacy/lifecycle/media filtering, smaller healthy result sets, caller cancellation, deadline timeout, no partial output, and result-order validation.

## 6. Real PostgreSQL Acceptance and Documentation

- [ ] 6.1 Add real PostgreSQL projection lifecycle tests for valid/invalid JSON, source updates, concurrent embedding/backfill writes, eligibility transitions, hard deletes, stale cleanup, advisory-lock serialization, replay, purge/rebuild, and model isolation.
- [ ] 6.2 Add a below-threshold fixture proving exact cosine correctness and an above-threshold seeded fixture with `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` requiring the accepted named HNSW index and rejecting a sequential projection scan for the production filtered query shape.
- [ ] 6.3 Add exact eligible-cosine ground-truth comparison for deterministic queries, enforce aggregate recall@20 of at least `0.90`, and report filtered requested/returned K, pool survival, exclusions, and readability cases before HNSW activation.
- [ ] 6.4 Add documented capacity and opt-in warm performance gates recording free disk, estimated index growth, WAL budget, CPU/memory/connection headroom, versions, fixture, HNSW settings, warm-up, latency, filtered fill, and zero deadline failures.
- [ ] 6.5 Update PostgreSQL/deployment, semantic embedding, recommendation prerequisite, metrics, and operator runbooks with authoritative JSON/version identity, exact-first behavior, prerequisites, bounded migration, reconciliation equality, independent connections, dry-run, capacity-gated concurrent HNSW build/rebuild/purge/reindex, filtered-fill/recall acceptance, coverage verification, rollout, and rollback.
- [ ] 6.6 Run targeted config/migration/projection/query/command tests, compile `./cmd/feed`, `./cmd/worker`, and the operator binary, run `cd apps/api && go test ./...`, validate Compose, then run `openspec validate --all --strict` and confirm no application RecallProvider, policy/profile/shadow/ranking/training/external-vector-DB code or main-spec edits were added.
