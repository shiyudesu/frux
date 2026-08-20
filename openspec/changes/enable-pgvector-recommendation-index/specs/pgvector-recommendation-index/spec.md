## ADDED Requirements

### Requirement: Explicit Semantic Embedding Dependencies
The pgvector recommendation index capability SHALL depend on completed, strictly validated `integrate-semantic-video-embeddings` and `backfill-semantic-video-embeddings` changes. It SHALL consume only the fixed model key `semantic-minilm-l12-v2@e8f8c211226b894f`, dimension 384, finite L2-normalized durable JSON vectors, canonical source hashes, and readable-video lifecycle semantics defined by those changes.

#### Scenario: Dependency contracts are available
- **WHEN** pgvector infrastructure is enabled
- **THEN** startup validates the exact fixed model, dimension, durable source schema, and normalization contract before vector migration, reconciliation, or query composition

#### Scenario: Dependency contract differs
- **WHEN** the configured or persisted semantic descriptor differs in model key or dimension from the dependency contracts
- **THEN** enabled startup fails with a bounded prerequisite error before projection mutation or ANN queries

### Requirement: Disabled Environments Retain Standard PostgreSQL
pgvector infrastructure SHALL be disabled by default. A disabled API, worker, migration, test, Compose deployment, or Kubernetes deployment MUST NOT require a pgvector image, query extension catalogs, create or validate the vector extension, create vector objects, run projection reconciliation, or compose the ANN repository.

#### Scenario: Ordinary PostgreSQL starts with pgvector disabled
- **WHEN** Frux starts against the existing supported standard PostgreSQL 17 image with pgvector disabled
- **THEN** schema initialization and runtime behavior complete without any vector extension availability or privilege requirement

#### Scenario: Extension is absent while disabled
- **WHEN** the database has no vector extension and pgvector is disabled
- **THEN** API and worker startup, migrations, and existing recommendation behavior remain unchanged

### Requirement: Enabled PostgreSQL and Extension Prerequisites
An enabled deployment SHALL use a supported pgvector PostgreSQL 17 image, SHALL require PostgreSQL major version 17 and pgvector extension version at least `0.8.0`, and SHALL validate extension availability before additive migration. Compose SHALL use the pinned `pgvector/pgvector:0.8.1-pg17` image for enabled development; deployment manifests SHALL use the same version with an approved digest.

#### Scenario: Enabled prerequisites are valid
- **WHEN** PostgreSQL 17 exposes a supported vector extension and the exact semantic descriptor
- **THEN** enabled initialization may install or validate the extension and continue to projection migration

#### Scenario: Extension is unavailable or old
- **WHEN** the vector extension is unavailable or its installed version is lower than `0.8.0`
- **THEN** enabled initialization fails before projection reconciliation or ANN query composition

#### Scenario: PostgreSQL major version is wrong
- **WHEN** pgvector is enabled against a PostgreSQL server whose major version is not 17
- **THEN** startup fails with a bounded prerequisite error and performs no projection work

### Requirement: Additive Bounded Vector Migration Without HNSW Rebuild
Enabled schema initialization SHALL run inside the existing PostgreSQL advisory-locked migration flow and SHALL add only the extension, projection table, constraints, and small supporting indexes idempotently under bounded lock and statement timeouts. Startup migration MUST NOT create, rebuild, drop, or repair HNSW and MUST NOT rewrite or delete `video_embedding`, video facts, or another model.

#### Scenario: Multiple processes initialize an empty enabled database
- **WHEN** API and worker start enabled migrations concurrently
- **THEN** the advisory lock serializes extension and vector-object creation and both processes observe the same complete schema without duplicate-object failures

#### Scenario: Enabled migration repeats
- **WHEN** schema initialization runs again against the complete vector schema
- **THEN** migration validates existing definitions and completes without destructive changes or duplicate indexes

#### Scenario: Large projection already exists
- **WHEN** enabled startup sees a large populated projection with no HNSW index or a stale HNSW index
- **THEN** migration completes without starting an automatic index build/rebuild inside the shared migration lock

#### Scenario: Disabled migration runs
- **WHEN** the same migration entrypoint runs with pgvector disabled
- **THEN** it skips extension catalogs and every vector-specific statement

### Requirement: Rebuildable Exact-Model Vector Projection
Frux SHALL store the derived search representation in `semantic_video_ann_projection` with primary key `(video_id, model)`, a foreign key to `video(id)` with delete cascade, embedding provider/model/revision, `vector(384)` embedding, source text hash, source vector digest, source embedding update time, and projection time. Durable versioned `video_embedding.embedding_json` SHALL remain the source of truth, and the projection SHALL be disposable and rebuildable.

#### Scenario: Eligible semantic source is projected
- **WHEN** an exact-model durable row has dimension 384, exactly 384 finite components, unit norm within `1e-4`, and belongs to a readable video
- **THEN** one projection row is inserted or updated with matching model, source hash, source update time, and normalized vector

#### Scenario: Source JSON is invalid
- **WHEN** the exact-model JSON has the wrong component count, a non-finite component, or invalid norm
- **THEN** no projection row is written from that source, the durable JSON remains unchanged, and invalid/missing coverage is observable

#### Scenario: Source embedding changes
- **WHEN** the durable exact-model text hash or update time changes
- **THEN** reconciliation replaces only that model's derived projection and preserves every durable embedding row

### Requirement: Exact-First Search and Capacity-Gated Cosine HNSW
The repository SHALL use exact cosine search while eligible exact-model projection rows are below a configured positive `hnsw_min_rows` threshold or no accepted HNSW index exists. A model-specific partial HNSW index using `vector_cosine_ops`, `m=16`, and `ef_construction=64` MAY be created only by guarded operator maintenance after row-count, free-disk, WAL-budget, CPU-headroom, connection, concurrency, lock-timeout, and statement-timeout gates pass. Supporting B-tree indexes SHALL cover exact source-identity reconciliation and readable-video eligibility using schema-unique names.

#### Scenario: Vector schema is inspected
- **WHEN** operator-built HNSW catalog definitions are inspected
- **THEN** the named index is partial to `semantic-minilm-l12-v2@e8f8c211226b894f`, uses cosine operators, and records the explicit HNSW parameters

#### Scenario: Another model row exists
- **WHEN** a projection row for another model is present
- **THEN** it is outside this capability's HNSW index, reconciliation, purge, rebuild, and query scope

#### Scenario: Catalog is below threshold
- **WHEN** current eligible projection rows are fewer than `hnsw_min_rows`
- **THEN** exact cosine remains active and no automatic HNSW creation is attempted

#### Scenario: Capacity gate fails
- **WHEN** disk, WAL, CPU, maintenance connection, concurrency, or timeout headroom is below its configured bound
- **THEN** concurrent HNSW build/reindex does not start and exact cosine remains the healthy query mode

#### Scenario: Concurrent index build fails
- **WHEN** cancellation, timeout, or database failure leaves an invalid index
- **THEN** the operator reports a bounded failure, cleans or marks the invalid object safely, and queries continue in exact mode

### Requirement: Bounded Idempotent Projection Reconciliation
An enabled worker SHALL supervise a projection reconciler with bounded interval, batch size, rows per cycle, cycle deadline, dedicated low-concurrency connection pool, and transaction-local lock/statement timeouts. A cycle SHALL use a projection-specific advisory lock, idempotently upsert rows only when provider/model/revision/text-hash/vector-digest/update-time metadata differs from the authoritative source, and delete bounded exact-model rows that are stale, source-missing, private, unpublished, deleted, or media-unready.

#### Scenario: Reconciler catches up missing coverage
- **WHEN** eligible exact-model JSON rows lack current projections
- **THEN** the reconciler processes them in stable bounded batches and repeated cycles converge without duplicate rows

#### Scenario: Video becomes unreadable
- **WHEN** a projected video becomes private, unpublished, deleted, or has media status outside `legacy_ready` and `ready`
- **THEN** bounded reconciliation removes only its exact-model projection

#### Scenario: Source row disappears
- **WHEN** the exact-model durable embedding no longer exists
- **THEN** bounded reconciliation deletes the corresponding exact-model projection without changing other projection or embedding models

#### Scenario: Projection metadata is stale
- **WHEN** any provider, model, revision, text hash, vector digest, or source update time differs from the authoritative versioned embedding row
- **THEN** reconciliation replaces or removes the projection before it can be treated as current

#### Scenario: Another reconciler holds the lock
- **WHEN** a worker or operator already owns the projection advisory lock
- **THEN** the new cycle performs no mutation, reports a healthy skipped-lock result, and retries on a later interval

#### Scenario: Cancellation interrupts a batch
- **WHEN** worker shutdown or the cycle deadline cancels reconciliation
- **THEN** in-flight SQL is canceled, completed transactions remain idempotent, and unfinished rows are eligible for a later cycle

### Requirement: Guarded Operator Projection Management
Frux SHALL provide a PostgreSQL-only one-shot operator command for bounded reconcile, dry-run, exact-model purge, exact-model projection rebuild, capacity-gated concurrent HNSW build, and named HNSW reindex. Every destructive mode MUST require exact model confirmation, HNSW maintenance MUST additionally require exact index confirmation, and all page, row, runtime, lock-timeout, statement-timeout, and maintenance-concurrency limits MUST reject zero or unlimited values.

#### Scenario: Dry-run is requested
- **WHEN** an operator runs a bounded dry-run
- **THEN** the command reports proposed insert, update, delete, unchanged, and invalid counts without changing projection, source embeddings, or indexes

#### Scenario: Exact-model rebuild is confirmed
- **WHEN** `--rebuild-model` is accompanied by the exact fixed model confirmation
- **THEN** the command purges and repopulates only that model's projection under the advisory lock and within configured bounds without automatically rebuilding HNSW

#### Scenario: Destructive confirmation is unsafe
- **WHEN** model or index confirmation is missing, partial, wildcarded, or names another object
- **THEN** the command fails before acquiring mutation work or changing database objects

#### Scenario: Model purge is performed for rollback
- **WHEN** an operator confirms exact-model purge
- **THEN** only derived rows for that model are deleted and durable JSON embeddings, other models, the table, and the extension remain intact

#### Scenario: HNSW maintenance is accepted
- **WHEN** the eligible row threshold and every documented capacity gate pass with exact model/index confirmation
- **THEN** the command uses an independent connection and one maintenance permit to run concurrent index creation/reindex outside migration and reconciliation transactions

### Requirement: Projection Coverage and Operation Metrics
Frux SHALL expose bounded-cardinality metrics for eligible/current/missing/stale/ineligible projection coverage, reconciliation row outcomes, cycle results and duration, ANN query results and duration, and returned result counts. Metrics and normal logs MUST NOT contain model strings, video IDs, vectors, exclusions, SQL, operator confirmations, or raw infrastructure errors.

#### Scenario: Coverage sampling succeeds
- **WHEN** the bounded aggregate coverage query completes
- **THEN** gauges report only the fixed projection states using fixed labels

#### Scenario: Coverage sampling fails
- **WHEN** the aggregate query is canceled or fails
- **THEN** prior gauge values are preserved and a bounded failed-cycle result is recorded

#### Scenario: Projection work completes
- **WHEN** a row is inserted, updated, deleted, unchanged, or rejected as invalid
- **THEN** exactly one fixed reconciliation outcome is observed without row-specific labels

### Requirement: Narrow Infrastructure ANN Query Interface
The capability SHALL expose only an infrastructure-owned ANN repository using plain Go query and result values. The input SHALL contain a vector, top-K, and excluded video IDs; the output SHALL contain only positive video IDs and cosine scores. SQL, pgvector values/types, distance operators, projection models, and extension metadata MUST NOT escape infrastructure.

#### Scenario: Later provider consumes the repository
- **WHEN** `add-pgvector-recommendation-recall` adapts the ANN repository
- **THEN** it can pass plain numeric values and receive video IDs with cosine scores without importing pgvector or projection persistence types

#### Scenario: Infrastructure is disabled
- **WHEN** pgvector is disabled
- **THEN** the ANN repository is not constructed or required by existing application composition

### Requirement: Validated Bounded ANN Input
The ANN repository SHALL require exactly 384 finite input components with L2 norm within `1e-4` of one, top-K from 1 through 100, at most 20 unique positive excluded video IDs, and a context carrying a deadline. It SHALL defensively copy input and reject invalid values before executing SQL.

#### Scenario: Valid bounded query is submitted
- **WHEN** a normalized 384-dimensional vector, top-K at most 100, at most 20 valid exclusions, and a deadline are supplied
- **THEN** the repository executes at most one bounded ANN statement and returns no more than top-K rows

#### Scenario: Vector is invalid
- **WHEN** the vector has the wrong dimension, a non-finite component, or non-unit norm
- **THEN** the repository returns an invalid-input error before opening a query transaction

#### Scenario: Query bounds are invalid
- **WHEN** top-K is zero or greater than 100, exclusions exceed 20, an exclusion is non-positive or duplicated, or the context lacks a deadline
- **THEN** the repository rejects the query before SQL

### Requirement: Exact-Model Current Readable Cosine Query Semantics
Both exact and HNSW SQL SHALL bind provider/model/revision internally, apply exclusions in SQL, join current video facts and the authoritative versioned embedding row, require exact projection equality on text hash, vector digest, and source update time, and return only published, public, media-ready videos with finite positive cosine similarity. Both modes SHALL order by cosine distance ascending and `video_id ASC` as a deterministic tie-break and SHALL never return more than requested top-K.

#### Scenario: Eligible neighbors exist
- **WHEN** the index contains matching readable exact-model projections
- **THEN** results contain only those videos, positive cosine scores in `(0,1]`, and deterministic order

#### Scenario: Excluded or unreadable neighbor is close
- **WHEN** a nearest row is excluded, private, unpublished, deleted, or media-unready
- **THEN** it is absent from results and the bounded iterative scan may continue for another eligible row

#### Scenario: Another model is closer
- **WHEN** another model has a projection with a smaller cosine distance
- **THEN** it cannot participate in or affect the exact-model result

#### Scenario: Projection is stale
- **WHEN** a projected row no longer equals the authoritative provider/model/revision/text-hash/vector-digest metadata
- **THEN** neither exact nor HNSW query returns that video

#### Scenario: Cosine distances tie
- **WHEN** two eligible rows have equal cosine distance
- **THEN** the lower video ID appears first

### Requirement: Bounded ANN Execution and Cancellation
Each query SHALL cap its child deadline at 500 milliseconds, set transaction-local `lock_timeout` and `statement_timeout`, and honor caller cancellation. Exact mode SHALL run only below the configured size gate. HNSW mode SHALL use `hnsw.ef_search=100`, `hnsw.iterative_scan='strict_order'`, and `hnsw.max_scan_tuples=10000`. A timeout or cancellation MUST return no partial neighbor set.

#### Scenario: Caller deadline is shorter than the cap
- **WHEN** the caller supplies a deadline under 500 milliseconds
- **THEN** the database statement uses the shorter remaining duration and stops when that context ends

#### Scenario: Query exceeds its deadline
- **WHEN** PostgreSQL does not finish before the effective deadline
- **THEN** the driver cancels the statement, the read transaction closes, and the repository returns a bounded timeout without partial results

#### Scenario: Caller cancels explicitly
- **WHEN** the context is canceled during ANN execution
- **THEN** database work is canceled promptly and no detached query continues

### Requirement: Real PostgreSQL Exact, Filtered-Fill, Recall, and Capacity Acceptance
Implementation verification SHALL use real PostgreSQL 17 with pgvector at least `0.8.0`. Tests SHALL cover disabled and enabled migration, no startup HNSW rebuild, projection equality lifecycle, exact-mode semantics, HNSW semantics, query cancellation, maintenance timeouts/concurrency, and exact schema definitions. A below-threshold fixture SHALL prove exact cosine correctness. An above-threshold fixture SHALL prove accepted HNSW plan use, recall@20 of at least `0.90` against exact eligible ground truth, documented filtered-fill/survival under readability predicates and exclusions, and a modest warm performance gate.

#### Scenario: Query plan acceptance runs
- **WHEN** the seeded projection is analyzed and `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` runs for the production query shape
- **THEN** the plan uses the named HNSW index and does not sequentially scan the projection table

#### Scenario: Small catalog acceptance runs
- **WHEN** eligible rows remain below the configured HNSW threshold
- **THEN** exact cosine returns deterministic current readable results without requiring the HNSW index

#### Scenario: Recall-quality acceptance runs
- **WHEN** ANN top-20 results are compared with exact cosine ground truth for at least 100 seeded queries
- **THEN** aggregate recall@20 is at least `0.90` while exclusions and readability filters remain enforced

#### Scenario: Filtered fill acceptance runs
- **WHEN** public/published/media-ready predicates and exclusions remove near neighbors
- **THEN** the report records requested and returned K plus pre/post-filter survival and rejects HNSW activation when fill or recall materially trails exact eligible search

#### Scenario: Modest performance gate runs
- **WHEN** 100 warmed top-20 repository queries run over at least 10,000 rows in the documented local or CI PostgreSQL container
- **THEN** p95 latency is at most 150 milliseconds, no query exceeds its deadline, and the report records database/extension versions, fixture size, resources, HNSW settings, warm-up, and latency summary

### Requirement: Downstream and Scope Boundaries
This capability SHALL stop at optional pgvector infrastructure, derived projection lifecycle, operator controls, metrics, and the narrow ANN query repository. It SHALL NOT add an application `RecallProvider`, recommendation policy token, semantic user-profile loading, shadow execution, overlap metrics, ranking behavior, training, or an external vector database.

#### Scenario: Index capability is deployed
- **WHEN** migration, reconciliation, and ANN query verification are complete
- **THEN** current Feed recall, ranking, policies, APIs, workers unrelated to projection, and Web behavior remain unchanged

#### Scenario: Active ANN recall is implemented later
- **WHEN** `add-pgvector-recommendation-recall` is implemented
- **THEN** it consumes this capability's narrow repository instead of redefining pgvector deployment, projection, reconciliation, or query-plan acceptance

#### Scenario: Shadow evaluation is planned
- **WHEN** semantic ANN shadow execution is proposed
- **THEN** `shadow-semantic-ann-recall` follows provider integration and does not broaden this infrastructure change

#### Scenario: Strict validation runs
- **WHEN** all proposal artifacts are complete
- **THEN** `openspec validate --all --strict` succeeds without modifying application code or main specifications
