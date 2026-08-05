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

### Requirement: Additive Advisory-Locked Vector Migration
Enabled schema initialization SHALL run inside the existing PostgreSQL advisory-locked migration flow and SHALL add the extension, projection table, constraints, supporting indexes, and exact-model HNSW index idempotently. It MUST NOT rewrite or delete `video_embedding`, video facts, or another model.

#### Scenario: Multiple processes initialize an empty enabled database
- **WHEN** API and worker start enabled migrations concurrently
- **THEN** the advisory lock serializes extension and vector-object creation and both processes observe the same complete schema without duplicate-object failures

#### Scenario: Enabled migration repeats
- **WHEN** schema initialization runs again against the complete vector schema
- **THEN** migration validates existing definitions and completes without destructive changes or duplicate indexes

#### Scenario: Disabled migration runs
- **WHEN** the same migration entrypoint runs with pgvector disabled
- **THEN** it skips extension catalogs and every vector-specific statement

### Requirement: Rebuildable Exact-Model Vector Projection
Frux SHALL store the derived ANN representation in `semantic_video_ann_projection` with primary key `(video_id, model)`, a foreign key to `video(id)` with delete cascade, `vector(384)` embedding, source text hash, source embedding update time, and projection time. Durable `video_embedding.embedding_json` SHALL remain the source of truth, and the projection SHALL be disposable and rebuildable.

#### Scenario: Eligible semantic source is projected
- **WHEN** an exact-model durable row has dimension 384, exactly 384 finite components, unit norm within `1e-4`, and belongs to a readable video
- **THEN** one projection row is inserted or updated with matching model, source hash, source update time, and normalized vector

#### Scenario: Source JSON is invalid
- **WHEN** the exact-model JSON has the wrong component count, a non-finite component, or invalid norm
- **THEN** no projection row is written from that source, the durable JSON remains unchanged, and invalid/missing coverage is observable

#### Scenario: Source embedding changes
- **WHEN** the durable exact-model text hash or update time changes
- **THEN** reconciliation replaces only that model's derived projection and preserves every durable embedding row

### Requirement: Explicit Cosine HNSW and Supporting Indexes
The projection SHALL have an exact-model partial HNSW index using `vector_cosine_ops`, `m=16`, and `ef_construction=64`. Supporting B-tree indexes SHALL cover model/source update reconciliation and readable-video eligibility using schema-unique names.

#### Scenario: Vector schema is inspected
- **WHEN** migration verification reads PostgreSQL catalog definitions
- **THEN** the named HNSW index is partial to `semantic-minilm-l12-v2@e8f8c211226b894f`, uses cosine operators, and records the explicit HNSW parameters

#### Scenario: Another model row exists
- **WHEN** a projection row for another model is present
- **THEN** it is outside this capability's HNSW index, reconciliation, purge, rebuild, and query scope

### Requirement: Bounded Idempotent Projection Reconciliation
An enabled worker SHALL supervise a projection reconciler with bounded interval, batch size, rows per cycle, and cycle deadline. A cycle SHALL use a projection-specific advisory lock, idempotently upsert missing or changed exact-model rows, and delete bounded exact-model rows that are stale, source-missing, private, unpublished, deleted, or media-unready.

#### Scenario: Reconciler catches up missing coverage
- **WHEN** eligible exact-model JSON rows lack current projections
- **THEN** the reconciler processes them in stable bounded batches and repeated cycles converge without duplicate rows

#### Scenario: Video becomes unreadable
- **WHEN** a projected video becomes private, unpublished, deleted, or has media status outside `legacy_ready` and `ready`
- **THEN** bounded reconciliation removes only its exact-model projection

#### Scenario: Source row disappears
- **WHEN** the exact-model durable embedding no longer exists
- **THEN** bounded reconciliation deletes the corresponding exact-model projection without changing other projection or embedding models

#### Scenario: Another reconciler holds the lock
- **WHEN** a worker or operator already owns the projection advisory lock
- **THEN** the new cycle performs no mutation, reports a healthy skipped-lock result, and retries on a later interval

#### Scenario: Cancellation interrupts a batch
- **WHEN** worker shutdown or the cycle deadline cancels reconciliation
- **THEN** in-flight SQL is canceled, completed transactions remain idempotent, and unfinished rows are eligible for a later cycle

### Requirement: Guarded Operator Projection Management
Frux SHALL provide a PostgreSQL-only one-shot operator command for bounded reconcile, dry-run, exact-model purge, exact-model rebuild, and named HNSW reindex. Every destructive mode MUST require exact model confirmation, reindex MUST additionally require exact index confirmation, and all page, row, and runtime limits MUST reject zero or unlimited values.

#### Scenario: Dry-run is requested
- **WHEN** an operator runs a bounded dry-run
- **THEN** the command reports proposed insert, update, delete, unchanged, and invalid counts without changing projection, source embeddings, or indexes

#### Scenario: Exact-model rebuild is confirmed
- **WHEN** `--rebuild-model` is accompanied by the exact fixed model confirmation
- **THEN** the command purges and repopulates only that model's projection under the advisory lock and within configured bounds

#### Scenario: Destructive confirmation is unsafe
- **WHEN** model or index confirmation is missing, partial, wildcarded, or names another object
- **THEN** the command fails before acquiring mutation work or changing database objects

#### Scenario: Model purge is performed for rollback
- **WHEN** an operator confirms exact-model purge
- **THEN** only derived rows for that model are deleted and durable JSON embeddings, other models, the table, and the extension remain intact

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

### Requirement: Exact-Model Readable Cosine Query Semantics
The ANN SQL SHALL bind the fixed model internally, apply exclusions in SQL, join current video facts, and return only published, public, media-ready videos with finite positive cosine similarity. It SHALL order by cosine distance ascending and `video_id ASC` as a deterministic tie-break and SHALL never return more than requested top-K.

#### Scenario: Eligible neighbors exist
- **WHEN** the index contains matching readable exact-model projections
- **THEN** results contain only those videos, positive cosine scores in `(0,1]`, and deterministic order

#### Scenario: Excluded or unreadable neighbor is close
- **WHEN** a nearest row is excluded, private, unpublished, deleted, or media-unready
- **THEN** it is absent from results and the bounded iterative scan may continue for another eligible row

#### Scenario: Another model is closer
- **WHEN** another model has a projection with a smaller cosine distance
- **THEN** it cannot participate in or affect the exact-model result

#### Scenario: Cosine distances tie
- **WHEN** two eligible rows have equal cosine distance
- **THEN** the lower video ID appears first

### Requirement: Bounded ANN Execution and Cancellation
Each ANN query SHALL cap its child deadline at 500 milliseconds, set transaction-local `statement_timeout`, use `hnsw.ef_search=100`, `hnsw.iterative_scan='strict_order'`, and `hnsw.max_scan_tuples=10000`, and honor caller cancellation. A timeout or cancellation MUST return no partial neighbor set.

#### Scenario: Caller deadline is shorter than the cap
- **WHEN** the caller supplies a deadline under 500 milliseconds
- **THEN** the database statement uses the shorter remaining duration and stops when that context ends

#### Scenario: Query exceeds its deadline
- **WHEN** PostgreSQL does not finish before the effective deadline
- **THEN** the driver cancels the statement, the read transaction closes, and the repository returns a bounded timeout without partial results

#### Scenario: Caller cancels explicitly
- **WHEN** the context is canceled during ANN execution
- **THEN** database work is canceled promptly and no detached query continues

### Requirement: Real PostgreSQL Plan, Recall, and Performance Acceptance
Implementation verification SHALL use real PostgreSQL 17 with pgvector at least `0.8.0`. Tests SHALL cover disabled and enabled migration, projection lifecycle, query semantics, query cancellation, and exact schema definitions. A seeded fixture of at least 10,000 vectors SHALL prove HNSW plan use, recall@20 of at least `0.90` across at least 100 deterministic queries, and a documented modest warm performance gate.

#### Scenario: Query plan acceptance runs
- **WHEN** the seeded projection is analyzed and `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` runs for the production query shape
- **THEN** the plan uses the named HNSW index and does not sequentially scan the projection table

#### Scenario: Recall-quality acceptance runs
- **WHEN** ANN top-20 results are compared with exact cosine ground truth for at least 100 seeded queries
- **THEN** aggregate recall@20 is at least `0.90` while exclusions and readability filters remain enforced

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
