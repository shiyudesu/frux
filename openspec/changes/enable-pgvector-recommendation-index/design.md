## Context

`integrate-semantic-video-embeddings` defines the immutable semantic persistence key `semantic-minilm-l12-v2@e8f8c211226b894f`, 384-dimensional finite L2-normalized vectors, canonical source hashes, and durable JSONB rows in `video_embedding`. `backfill-semantic-video-embeddings` owns safe historical production of those rows. JSONB remains the durable fact format but cannot provide a practical ANN access path.

Frux currently uses ordinary PostgreSQL 17 images and runs schema initialization from both API and worker under a shared PostgreSQL advisory transaction lock. pgvector must therefore be opt-in: disabled environments must not need the extension, a pgvector image, vector DDL, or startup probes. Enabled environments need a disposable projection that can lag or be rebuilt without changing the source embeddings or current recommendation behavior. Because the confirmed route assumes a small initial catalog, exact cosine is the safe default; HNSW is a later physical optimization gated by configured size and measured database headroom.

This change is an infrastructure prerequisite. `add-pgvector-recommendation-recall` later adapts its narrow query repository into an application `RecallProvider`; `shadow-semantic-ann-recall` follows provider integration.

## Goals / Non-Goals

**Goals:**

- Keep standard PostgreSQL behavior byte-for-byte available when pgvector is disabled.
- Validate a pinned PostgreSQL 17 pgvector runtime and the exact semantic model/dimension contract before vector migration or reconciliation.
- Build an additive, exact-model, rebuildable `vector(384)` projection from authoritative versioned JSONB embedding rows.
- Maintain the projection through bounded idempotent upserts and deletion of stale or unreadable rows.
- Use exact cosine for small catalogs and create HNSW only after row-count, capacity, filtered-fill, and recall gates pass.
- Provide safe dry-run, reconcile, purge, rebuild, rollback, metrics, and verification procedures.
- Expose a cancellable, deadline-bounded, plain-Go ANN query repository with strict input and output bounds.
- Establish real-PostgreSQL plan, recall, and modest performance acceptance gates.

**Non-Goals:**

- Changing semantic embedding generation, backfill semantics, or JSONB source-of-truth ownership.
- Adding the application `RecallProvider`, policy token, user-profile reads, shadow traffic, ranking features/weights, or policy rollout.
- Training or changing models, supporting dynamic dimensions/models, or adding an external vector database.
- Making pgvector mandatory for API, worker, tests, Compose, or Kubernetes environments that leave it disabled.

## Decisions

### 1. Gate every pgvector dependency behind disabled-by-default infrastructure configuration

Add `postgres.pgvector.enabled`, default `false`. When false:

- existing `postgres:17.5-alpine` Compose and deployment images remain valid;
- startup does not query `pg_available_extensions` or `pg_extension`;
- migration does not execute `CREATE EXTENSION`, vector DDL, projection reconciliation, or ANN repository construction;
- API and worker behavior and prerequisites remain unchanged.

Enabled Compose uses the pinned supported image `pgvector/pgvector:0.8.1-pg17`; deployment manifests use the same version and pin the approved digest during implementation. Startup requires PostgreSQL major version 17 and pgvector extension version at least `0.8.0`. Configuration also binds the exact model key and dimension to the compile-time semantic descriptor and rejects any mismatch.

Image selection is explicit through the deployment environment/overlay rather than silently changing all PostgreSQL installations. Enabled API and worker instances fail startup with a bounded prerequisite error when the image, server version, extension availability/version, model key, or dimension is wrong. Disabled instances perform none of those checks.

Alternative considered: always install the extension while conditionally using it. Rejected because ordinary environments would still gain an extension/image/privilege prerequisite.

### 2. Keep startup migration additive and bounded

The shared migration transaction lock remains the outer serialization boundary. With pgvector enabled, migration:

1. verifies PostgreSQL 17 and extension availability;
2. runs `CREATE EXTENSION IF NOT EXISTS vector`;
3. verifies installed `extversion >= 0.8.0`;
4. creates the additive projection table, constraints, and small supporting B-tree indexes;
5. records the migration marker only after all validation succeeds.

Startup migration MUST NOT create, rebuild, drop, or repair HNSW. It sets bounded `lock_timeout` and `statement_timeout` before vector DDL and fails without an automatic retry loop that could hold the shared migration lock around a large table operation. HNSW lifecycle is owned by the guarded operator path on an independent connection.

The projection table is `semantic_video_ann_projection`:

- `video_id BIGINT NOT NULL REFERENCES video(id) ON DELETE CASCADE`;
- `model VARCHAR(64) NOT NULL`;
- `embedding_provider VARCHAR(64) NOT NULL`;
- `embedding_revision VARCHAR(64) NOT NULL`;
- `embedding vector(384) NOT NULL`;
- `source_text_hash VARCHAR(64) NOT NULL`;
- `source_vector_digest VARCHAR(64) NOT NULL`;
- `source_embedding_updated_at TIMESTAMPTZ NOT NULL`;
- `projected_at TIMESTAMPTZ NOT NULL`;
- primary key `(video_id, model)`.

The optional HNSW index, when the operator creates it, is partial and exact-model isolated:

```sql
CREATE INDEX idx_semantic_video_ann_minilm_cosine_hnsw
ON semantic_video_ann_projection
USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64)
WHERE model = 'semantic-minilm-l12-v2@e8f8c211226b894f';
```

Supporting indexes cover exact source-identity reconciliation and readable-video predicates. Names are schema-unique. No existing column/table is rewritten and no source embedding is deleted.

Alternative considered: add a vector column to `video_embedding`. Rejected because it would mix authoritative and derived representations, complicate disabled deployments, and make rollback less isolated.

### 3. Treat JSONB as authoritative and the vector table as an eventually consistent projection

Only the exact fixed semantic row with `dimension=384` is eligible as a source. The projector parses the JSON array into plain Go floats, requires exactly 384 finite values and norm within `1e-4` of one, normalizes a defensive copy once more, and writes the vector through an infrastructure-local pgvector encoder. pgvector driver/value types do not cross the infrastructure package.

A projection row is current only when provider, model, revision, source text hash, vector digest, and source embedding `updated_at` equal the authoritative durable row and the video is currently:

- published (`status=2`);
- public (`visibility='public'`);
- media-ready (`media_status IN ('legacy_ready','ready')`);
- backed by the exact semantic source row.

Queries always rejoin both the live video row and authoritative versioned embedding row and require exact metadata equality, so projection lag cannot expose unreadable media or stale semantic content. The reconciler deletes projection rows whose source disappeared, any identity field differs, or video became private, unpublished, deleted, or media-unready. Hard-deleted videos are also removed by the foreign-key cascade.

Alternative considered: database triggers on embedding/video changes. Rejected because JSON validation, lifecycle fan-out, extension-optional DDL, observability, bounded retries, and operator rebuilds are clearer in an explicit reconciler.

### 4. Run a single bounded infrastructure reconciler with idempotent batches

When enabled, the worker supervises an infrastructure projection reconciler. Defaults and accepted bounds are:

- interval: 1 minute, range 10 seconds–1 hour;
- batch size: 500, range 1–1,000;
- maximum examined rows per cycle: 5,000, range 1–100,000;
- cycle deadline: 30 seconds, range 1 second–10 minutes.

Each cycle takes a projection-specific PostgreSQL advisory lock with `pg_try_advisory_lock`; another process holding it causes a healthy skipped cycle. It first selects bounded source rows that are missing or differ by provider/model/revision/text hash/vector digest/source update time, ordered by `(video_embedding.updated_at, video_id)`, then validates and upserts them with `ON CONFLICT (video_id, model) DO UPDATE` only when authoritative identity metadata differs. It next selects bounded stale/ineligible projection IDs ordered by `(source_embedding_updated_at, video_id)` and deletes only those exact model rows.

Transactions are per batch, context-bound, and short. Reconciliation uses a dedicated, low-concurrency PostgreSQL connection pool separate from request traffic, sets bounded transaction-local `lock_timeout` and `statement_timeout`, and permits at most one mutating batch per process. Repeated cycles, concurrent live embedding writes, concurrent historical backfill writes, crashes, and duplicate selection are safe. Invalid source JSON is never projected; it is counted as invalid and remains visible as missing coverage for remediation by the source-owning changes.

Alternative considered: complete the entire catalog in one startup migration. Rejected because catalog conversion and HNSW maintenance can be slow, block deployments, and cannot satisfy bounded retry/cancellation requirements.

### 5. Gate HNSW lifecycle by catalog size and database headroom

Exact cosine remains the production query mode until both conditions hold:

- current eligible exact-model projection rows meet a configured `hnsw_min_rows` threshold; and
- an operator capacity check records sufficient free disk for table plus index growth, bounded WAL budget, CPU headroom, maintenance connection availability, and no conflicting migration/reconcile/reindex work.

Configuration bounds the threshold and never treats zero as “always build.” Capacity checks use documented conservative ratios and operator-supplied maintenance windows rather than pretending PostgreSQL can reliably infer production headroom. HNSW creation uses `CREATE INDEX CONCURRENTLY` from a dedicated connection, with bounded `lock_timeout`, bounded `statement_timeout`, one process-wide maintenance permit, cancellation, progress observation, and cleanup of a failed invalid index. It never runs inside the startup migration transaction.

Before HNSW becomes query-eligible, acceptance compares its filtered query against exact eligible cosine ground truth. It requires recall@20 at least `0.90`, documented result-fill behavior under public/published/media-ready filters and exclusions, and no material regression in filtered top-K survival. Failing acceptance leaves exact mode active.

Alternative considered: always create HNSW at schema migration. Rejected because small catalogs do not need it and large startup builds can hold locks, consume WAL/disk/CPU, and make every deploy risky.

### 6. Add a guarded one-shot operator command for reconciliation and index maintenance

Add `cmd/manage-semantic-video-ann` using PostgreSQL only. It shares validation and batch logic with the reconciler and offers:

- default bounded `reconcile`;
- `--dry-run`, which reports insert/update/delete/invalid counts without mutation;
- `--purge-model`, which deletes only the exact projection model;
- `--rebuild-model`, which purges that exact projection then performs bounded reconciliation without automatically building HNSW;
- `--build-hnsw` or `--reindex`, which first runs row-count/capacity gates and then performs guarded concurrent creation/rebuild of the named model-specific HNSW index.

All mutating destructive modes require `--confirm-model=semantic-minilm-l12-v2@e8f8c211226b894f`. Reindex additionally requires `--confirm-index=idx_semantic_video_ann_minilm_cosine_hnsw`. Page size, maximum rows, and maximum runtime are positive and bounded; zero/unlimited values are rejected. Projection mutation uses the same advisory lock so it cannot overlap a worker cycle or another operator mutation. HNSW maintenance uses a distinct maintenance lock and independent connection so it never holds the projection transaction or startup migration lock.

The command never deletes `video_embedding`, another model, the vector extension, or an unrelated index. Rebuild may temporarily reduce projection coverage, which metrics and the later provider degradation path make visible.

Alternative considered: generic SQL instructions for operators. Rejected because exact-model confirmation, bounds, dry-run parity, safe metrics, and lock ownership need executable enforcement.

### 7. Expose an exact-first fixed-model infrastructure repository with plain values

Create an infrastructure-owned repository such as:

```go
type ANNQuery struct {
    Vector           []float32
    TopK             int
    ExcludedVideoIDs []int64
}

type ANNNeighbor struct {
    VideoID    int64
    CosineScore float64
}

type ANNRepository interface {
    Nearest(context.Context, ANNQuery) ([]ANNNeighbor, error)
}
```

The concrete repository is constructed only for the fixed semantic descriptor. It accepts exactly 384 finite components, requires an input norm within `1e-4` of one, copies the input, requires `TopK` in `1..100`, and accepts at most 20 unique positive exclusions. Invalid input fails before SQL. The context must carry a deadline; the repository derives a child deadline capped at 500 milliseconds and uses driver cancellation plus transaction-local `lock_timeout` and `statement_timeout`.

The SQL fixes provider/model/revision internally, rejoins the authoritative embedding row and requires exact text-hash/vector-digest/update-time equality, filters current published/public/media-ready videos, applies exclusions in SQL, requires cosine similarity `> 0`, orders by cosine distance ascending then `video_id ASC`, and returns at most `TopK`. Output is rejected if an ID is non-positive, a score is non-finite/non-positive/outside `[0,1]`, results exceed the limit, or order is inconsistent.

When no accepted HNSW exists or eligible rows are below threshold, the repository executes bounded exact cosine over the small eligible set. When an accepted HNSW is present above threshold, it sets `hnsw.ef_search=100`, `hnsw.iterative_scan='strict_order'`, and `hnsw.max_scan_tuples=10000`. Both modes have identical filtering, ordering, score, and cancellation semantics; callers do not select the physical mode. pgvector `>=0.8.0` is required for bounded iterative filtered HNSW scans. No vector type, SQL fragment, distance value, extension metadata, or projection model escapes infrastructure.

Alternative considered: expose GORM models or pgvector values to the application provider. Rejected because the later provider needs only stable video IDs and cosine scores.

### 8. Make coverage and operations observable with bounded labels

Register:

- `frux_semantic_video_ann_projection_rows{state}`;
- `frux_semantic_video_ann_reconcile_rows_total{outcome}`;
- `frux_semantic_video_ann_reconcile_cycles_total{result}`;
- `frux_semantic_video_ann_reconcile_duration_seconds{result}`;
- `frux_semantic_video_ann_query_total{result}`;
- `frux_semantic_video_ann_query_duration_seconds{result}`;
- `frux_semantic_video_ann_query_results`.
- `frux_semantic_video_ann_query_mode_total{mode,result}`;
- `frux_semantic_video_ann_maintenance_gate{gate,state}`.

Projection states are `eligible`, `current`, `missing`, `stale`, and `ineligible`; row outcomes are `inserted`, `updated`, `deleted`, `unchanged`, and `invalid`; fixed result enums cover success, skipped lock, canceled, timeout, invalid input, prerequisite, and database failure. Model names, video IDs, vectors, exclusions, SQL, raw errors, operator flags, and query values are never labels or normal logs.

Coverage sampling uses bounded aggregate SQL and preserves the prior gauge on failure while recording a bounded failed-cycle result.

### 9. Verify exact-first correctness and HNSW usefulness against real PostgreSQL

Tests use a real PostgreSQL 17 instance with pgvector `>=0.8.0`, not SQLite or only mocks. They cover:

- disabled startup/migration against ordinary `postgres:17.5-alpine` with no extension query or vector object;
- enabled clean/concurrent/idempotent migration, extension/version rejection, schema/index definitions, and model/dimension prerequisite rejection;
- valid JSON conversion, invalid length/non-finite/norm cases, source updates, concurrent writes, eligibility transitions, stale deletion, replay, advisory locking, dry-run, purge, rebuild, and model isolation;
- query validation, exclusions, live readability filtering, exact model, positive scores, deterministic ties, top-K bounds, cancellation, deadline expiry, and no pgvector types outside infrastructure;
- exact-cosine query correctness and bounded plans below `hnsw_min_rows`, with no HNSW prerequisite;
- `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` above threshold after accepted concurrent index build, requiring the named HNSW index and rejecting a sequential scan of the projection;
- HNSW recall against exact eligible cosine ground truth, requiring recall@20 of at least `0.90` across deterministic queries;
- filtered-fill and survival tests with public/published/media-ready predicates and exclusions, recording requested versus returned K and rejecting unacceptable HNSW loss relative to exact mode;
- failed disk/WAL/CPU/connection/lock/statement-timeout gates, interrupted concurrent builds, invalid-index cleanup, and proof that startup migration never initiates a rebuild;
- a documented opt-in modest performance gate on a warm local/CI PostgreSQL container: 100 top-20 queries over at least 10,000 rows, p95 repository latency at or below 150 milliseconds, zero deadline failures, and bounded result counts.

The performance result records fixture size, PostgreSQL/pgvector versions, HNSW parameters, `ef_search`, CPU/memory limits, warm-up count, and latency summary so it is reproducible without claiming production capacity.

Alternative considered: assert only that the query returns plausible rows. Rejected because an ANN prerequisite must prove the intended index is used and that approximation remains acceptably close to exact cosine ordering.

### 10. Roll out and roll back without changing recommendation behavior

Implementation and deployment order is:

1. complete and strictly validate `integrate-semantic-video-embeddings`;
2. complete and strictly validate `backfill-semantic-video-embeddings`;
3. deploy the supported pgvector PostgreSQL 17 image while keeping the feature disabled;
4. enable bounded migration and verify extension/projection prerequisites without HNSW creation;
5. run dry-run, then bounded reconciliation until exact-query coverage is acceptable;
6. keep exact mode for a small catalog; only after threshold and capacity gates pass, build HNSW concurrently and run plan, filtered-fill, recall, and modest performance acceptance;
7. keep the later recommendation provider disabled until its separate change is implemented.

Rollback first disables future ANN consumers, then disables pgvector reconciliation/query composition. Standard application behavior continues because JSON embeddings and existing recommendation paths remain intact. Operators may retain the inert projection or purge only the confirmed fixed model. Automatic rollback never drops the extension/table/index because other future models may depend on them; schema removal requires a separate accepted change.

## Risks / Trade-offs

- [The pgvector image/tag or extension version is unavailable in an environment] → Fail only enabled deployments with a bounded prerequisite error; disabled deployments retain ordinary PostgreSQL.
- [Projection lag leaves missing neighbors] → Reconcile frequently, expose coverage, recheck live readability in every query, and let later providers degrade safely.
- [Filtered HNSW queries return too few rows] → Require pgvector iterative scans, bound scan tuples, test exclusions/readability filters, and return a smaller healthy result rather than widening bounds.
- [HNSW build or rebuild consumes database resources] → Keep exact mode below threshold; require disk/WAL/CPU/connection headroom, independent maintenance connections, bounded timeouts, one maintenance permit, concurrent DDL, and documented windows.
- [Invalid durable JSON prevents projection] → Reject only the invalid row, expose invalid/missing coverage, and leave correction to the source-owning embedding changes.
- [A purge/rebuild causes temporary coverage loss] → Require exact confirmations and dry-run support, serialize with advisory locking, and preserve JSON source facts for deterministic recovery.
- [Performance gates vary by hardware] → Use a modest pinned fixture/container gate, record resources and versions, and treat it as regression protection rather than a production SLO.

## Migration Plan

1. Add disabled configuration and tests proving no pgvector checks or DDL occur.
2. Add supported enabled deployment image/overlay and prerequisite validation.
3. Add advisory-locked extension/projection migration and real PostgreSQL migration tests; prove no startup HNSW build occurs.
4. Add projection conversion/reconciliation, operator command, metrics, and lifecycle tests.
5. Add the fixed-model ANN repository and validation/cancellation tests.
6. Run exact-mode acceptance first, then capacity-gated concurrent HNSW build plus filtered-fill, query-plan, recall-quality, and modest performance gates only above threshold.
7. Update PostgreSQL, embedding, recommendation-prerequisite, deployment, and operator documentation.
8. Strictly validate all OpenSpec artifacts without editing main specs.

Rollback disables composition and reconciliation, then optionally purges the confirmed model projection. Durable JSON embeddings, videos, existing PostgreSQL behavior, and recommendation behavior remain unchanged.

## Open Questions

None. Dependencies, runtime versions, model/dimension, projection ownership, HNSW parameters, reconciliation bounds, query contract, operator guards, quality/performance gates, downstream order, and exclusions are fixed by this proposal.
