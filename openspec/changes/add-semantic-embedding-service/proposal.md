## Why

After Frux completes the trusted-impression, dataset-export, offline-evaluation, and linear-weight
learning stages, it needs a reproducible semantic representation of Chinese video titles and
descriptions before later recommendation work can safely depend on learned text embeddings. This
roadmap step isolates the Python/model runtime and fixes a standalone bounded contract without
coupling it to the Go worker, persistence, Kafka, RabbitMQ, or recommendation policy.

## What Changes

- Add a separately deployable, CPU-oriented Python semantic embedding service under its own app/module with locked dependencies and an immutable, revision-pinned multilingual sentence model.
- Define authenticated internal endpoints for liveness/readiness, fixed model metadata, and bounded batch embedding only.
- Define strict normalized title/description input limits, deterministic item ordering and batching, L2-normalized finite-vector output, safe errors, request timeouts, concurrency limits, and overload backpressure.
- Preload and validate the fixed model before readiness; prohibit request-time downloads, arbitrary model selection, mutable model switching, browser exposure, and raw token or URL storage.
- Add a container image, Compose/configuration wiring, healthcheck, resource guidance, tests, and service documentation.
- Explicitly defer all Go recommendation/worker integration, queues, databases, persisted video embeddings, backfills, vector indexes, recommendation policy changes, and model training to later changes, including `integrate-semantic-video-embeddings`.
- Require `learn-recommendation-policy-weights` to be completed and archived before this change is
  implemented, preserving the strict sequence in `docs/recommendation-roadmap.md`.

## Capabilities

### New Capabilities

- `semantic-embedding-service`: Defines the fixed multilingual text-embedding contract, bounded authenticated API, deterministic inference behavior, operational lifecycle, deployment, and verification requirements.

### Modified Capabilities

None.

## Impact

- Adds a new standalone app directory, Python lock/environment files, tests, and container build context.
- Extends local Compose and environment documentation with one internal-only service and healthcheck.
- Introduces a fixed initial model contract: `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` at revision `e8f8c211226b894fcb81acc59f3b34ba3efd5f42`, producing 384-dimensional vectors.
- Depends on the completed recommendation measurement/learning chain through
  `learn-recommendation-policy-weights`; it is not part of the Kafka message migration.
- Does not change existing Go APIs, workers, databases, queues, Web behavior, main OpenSpec specifications, or current recommendation requirements.
