## Why

Frux needs higher-quality semantic text vectors without operating or training a model runtime. The
previous plan for a local Python/MiniLM service would add model images, PyTorch processes, CPU
capacity management, and model-artifact lifecycle that Frux does not need to own. This change
instead establishes a provider-agnostic adapter contract for a managed external Embedding API while
keeping provider calls out of every synchronous API and Feed path.

## What Changes

- Replace the local Python/model-serving plan with a provider-agnostic external Embedding API
  adapter exposed to Go callers through a narrow application interface.
- Pin one deployment contract consisting of provider, model, immutable revision, output dimension,
  and canonicalizer `semantic-text-v1`; requests cannot select or override those values.
- Define `semantic-text-v1` independently from provider tokenization so normalized public video
  title/description input and its SHA-256 text hash remain stable across adapter implementations.
- Send only normalized title and description for videos that are currently published and public.
  Never send user IDs, video/business IDs, request IDs, behavior data, credentials/tokens, URLs, or
  private/draft content to the provider.
- Load provider credentials only from secret/config injection. Credentials, authorization headers,
  and derived secret values are never stored in PostgreSQL, Redis, Kafka, checkpoints, logs, or
  metrics.
- Add text-hash deduplication and cache semantics scoped by the complete contract identity, plus
  strict response checks for item count/order, exact dimension, finite components, and L2
  normalization before any vector is accepted.
- Define bounded timeouts, concurrency, provider rate-limit and `Retry-After` handling, retry
  classification, a circuit/gate, and bounded cost/quota/latency metrics.
- Require a new model identity and complete semantic rebuild/backfill before any provider, model,
  revision, dimension, or canonicalizer change can become active.
- Keep `hash-ngram-v1` as the permanent fallback. This change adds no online inference, durable
  live-video job execution, historical backfill, vector retrieval, ranking, profile, or training
  behavior; those remain owned by later changes.
- Remove all requirements for a local model process, Python/PyTorch/Sentence Transformers,
  downloaded model artifacts, inference child processes, CPU worker pools, or a model container.

## Capabilities

### New Capabilities

- `semantic-embedding-service`: Defines the provider-neutral managed Embedding API adapter,
  canonical text/privacy boundary, pinned contract identity, validation, caching, resilience,
  cost/quota observability, and model-change/rebuild rules.

### Modified Capabilities

None.

## Impact

- Affects Go application ports, provider adapter/configuration, secret wiring, bounded metrics,
  tests, and semantic embedding documentation.
- Depends on the completed recommendation measurement/learning prerequisites documented by the
  roadmap; it is not part of the Kafka migration.
- Establishes reusable contracts for `integrate-semantic-video-embeddings` and
  `backfill-semantic-video-embeddings`.
- Adds no Python service, model artifact, model training, public endpoint, database job, Kafka
  consumer, Feed request-path call, Web behavior, or recommendation-policy change.
