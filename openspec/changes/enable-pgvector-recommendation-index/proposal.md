## Why

Frux will have durable fixed-model semantic video embeddings, but JSONB is unsuitable for bounded approximate-nearest-neighbor retrieval. A separately deployable pgvector projection is needed so later recommendation providers can query semantic neighbors without making pgvector mandatory for ordinary PostgreSQL environments or replacing the durable JSON source of truth.

## What Changes

- Add disabled-by-default pgvector configuration so ordinary deployments retain the existing PostgreSQL image, startup, and migrations without extension checks.
- For enabled deployments, use a supported pgvector PostgreSQL 17 image and validate database extension/version plus the exact semantic model and 384-dimensional normalized-vector prerequisites.
- Add an additive, advisory-locked migration for a rebuildable exact-model semantic video vector projection, cosine HNSW index with explicit parameters, and supporting eligibility/reconciliation indexes.
- Reconcile the projection from durable semantic JSON rows through bounded, idempotent backfill and stale-row deletion, excluding private, deleted, unpublished, and media-unready videos.
- Add coverage metrics and guarded dry-run, rebuild, purge, rollback, and model-isolated operator procedures.
- Expose only an infrastructure-owned narrow ANN query interface with validated normalized input, top-K at most 100, at most 20 exclusions, exact-model isolation, readable-video filtering, positive cosine scores, deterministic tie-breaking, and context deadline/cancellation.
- Add real PostgreSQL migration, query-plan, recall-quality, reconciliation, and modest performance-gate tests.
- Explicitly exclude the application `RecallProvider`, policy token, semantic user-profile loading, shadow mode, ranking changes, training, and external vector databases.

## Capabilities

### New Capabilities

- `pgvector-recommendation-index`: Defines optional pgvector deployment prerequisites, rebuildable exact-model semantic video projection and HNSW lifecycle, bounded reconciliation and operator controls, observability, a narrow infrastructure ANN query contract, and PostgreSQL acceptance tests.

### Modified Capabilities

None. Existing PostgreSQL and recommendation requirements remain unchanged when this optional capability is disabled.

## Impact

- Depends explicitly on `integrate-semantic-video-embeddings` for the fixed semantic model identity, canonical durable JSON vector facts, normalization, dimension, and readable-video semantics, and on `backfill-semantic-video-embeddings` for bounded historical semantic coverage before index acceptance.
- Affects PostgreSQL Compose/Kubernetes image selection for enabled environments, configuration, additive migration and projection persistence, infrastructure repositories, metrics, operator tooling/runbooks, and real-PostgreSQL tests.
- JSONB `video_embedding` rows remain authoritative; the pgvector projection is derived, model-isolated, disposable, and rebuildable.
- `add-pgvector-recommendation-recall` consumes the narrow query interface later. `shadow-semantic-ann-recall` follows provider integration and is not part of this change.
- Adds no public API, Web behavior, application recall provider, recommendation policy/ranking behavior, semantic profile loading, model training, or external vector service.
