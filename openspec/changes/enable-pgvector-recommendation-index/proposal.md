## Why

Frux will have durable versioned semantic video embeddings whose JSON representation remains authoritative. The confirmed low-data route should use exact cosine search for a small eligible catalog and add HNSW only after configurable row-count and database-capacity gates are satisfied, without making pgvector mandatory for ordinary PostgreSQL environments.

## What Changes

- Add disabled-by-default pgvector configuration so ordinary deployments retain the existing PostgreSQL image, startup, and migrations without extension checks.
- For enabled deployments, use a supported pgvector PostgreSQL 17 image and validate database extension/version plus the exact semantic model and 384-dimensional normalized-vector prerequisites.
- Add an additive, advisory-locked migration only for the extension, rebuildable exact-model vector projection, and small supporting indexes; never build or rebuild a large HNSW index inside the startup migration lock.
- Reconcile the projection from authoritative versioned semantic JSON rows through bounded, idempotent equality-checked upserts and stale-row deletion, excluding private, deleted, unpublished, and media-unready videos.
- Query exact cosine while eligible projection size is below the configured HNSW threshold or no accepted HNSW index exists.
- Permit guarded concurrent HNSW creation/reindex only when row-count, free disk, WAL budget, CPU headroom, lock/statement-timeout, and maintenance-concurrency gates pass.
- Add coverage metrics and guarded dry-run, rebuild, purge, rollback, and model-isolated operator procedures.
- Expose only an infrastructure-owned narrow semantic-neighbor query interface with validated normalized input, top-K at most 100, at most 20 exclusions, exact-model isolation, authoritative-projection equality checks, published/public/media-ready filtering, positive cosine scores, deterministic tie-breaking, and context deadline/cancellation.
- Add real PostgreSQL migration, exact-query, HNSW-plan, filtered-fill, recall-quality, reconciliation, and modest performance/capacity-gate tests.
- Explicitly exclude the application `RecallProvider`, policy token, semantic user-profile loading, shadow mode, ranking changes, training, and external vector databases.

## Capabilities

### New Capabilities

- `pgvector-recommendation-index`: Defines optional pgvector deployment prerequisites, authoritative versioned-embedding projection, exact-first query behavior, capacity-gated HNSW lifecycle, bounded reconciliation and operator controls, observability, a narrow infrastructure query contract, and PostgreSQL acceptance tests.

### Modified Capabilities

None. Existing PostgreSQL and recommendation requirements remain unchanged when this optional capability is disabled.

## Impact

- Depends explicitly on `integrate-semantic-video-embeddings` for the fixed semantic model identity, canonical durable JSON vector facts, normalization, dimension, and readable-video semantics, and on `backfill-semantic-video-embeddings` for bounded historical semantic coverage before index acceptance.
- Affects PostgreSQL Compose/Kubernetes image selection for enabled environments, configuration, additive migration and projection persistence, infrastructure repositories, metrics, operator tooling/runbooks, and real-PostgreSQL tests.
- JSONB `video_embedding` rows remain authoritative; the pgvector projection is derived, model-isolated, disposable, and rebuildable.
- `add-pgvector-recommendation-recall` consumes the narrow query interface later. `shadow-semantic-ann-recall` follows provider integration and is not part of this change.
- Adds no public API, Web behavior, application recall provider, recommendation policy/ranking behavior, semantic profile loading, model training, or external vector service.
