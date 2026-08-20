## Why

Frux currently understands video content primarily through normalized title/description text and
the deterministic `hash-ngram-v1` vector. In the project's low-data development stage, a
pretrained multimodal representation can make newly published videos discoverable by visual and
textual meaning without training on Frux users, while preserving the existing search and Feed paths
when vectors are missing or the provider is unavailable.

## What Changes

- Add an environment-neutral `MultimodalEmbeddingProvider` contract with separate bounded
  operations for public-video text plus prepared cover/keyframe inputs and for normalized public
  search-query text. Both operations return validated, normalized vectors in the same model space.
  The contract does not prescribe Python, Go, ONNX, a local service, a remote service, hardware, or
  deployment topology.
- Define an immutable multimodal contract identity covering provider, model, revision, dimension,
  text canonicalizer, frame-sampling policy, image preprocessing policy, fusion policy, source
  content hash, and vector digest. Different contract identities are never mixed silently.
- Extend the existing first-publication embedding intake so it durably hands off a multimodal job
  only after the current `hash-ngram-v1` fact is safe. Video-content provider work remains outside
  the Kafka handler and every synchronous publication and Feed path. Public search may request a
  query-text vector only behind a bounded cache, deadline, admission limit, and lexical fallback.
- Add bounded PostgreSQL job states, leases, fencing, heartbeat/reclaim, retry, terminal failure,
  manual requeue, and conditional result persistence for newly published public, media-ready
  videos.
- Persist authoritative multimodal vector facts separately from rebuildable retrieval projection
  state. Missing vectors remain a normal condition and never make an otherwise readable video
  unavailable.
- Add exact cosine retrieval for the active multimodal contract. This change creates no HNSW or
  other approximate-nearest-neighbor index and requires no historical catalog backfill.
- Extend public video search to use deterministic lexical + semantic hybrid retrieval when a query
  vector is available, while keeping the existing lexical result and cursor contract as the
  fallback when semantic retrieval is unavailable.
- Add a bounded similar-video API that uses the source video's active multimodal vector, excludes
  the source itself, revalidates public/media-ready state, and returns a stable exact-similarity
  page.
- Add fixed-label metrics and operator diagnostics for job backlog, provider result, model
  coverage, exact-query latency, hybrid-search degradation, and similar-video retrieval without
  logging raw vectors, images, arbitrary queries, credentials, or high-cardinality identifiers.
- Keep `hash-ngram-v1`, current recommendation policies, and all existing Feed providers unchanged.
  Explicitly exclude user/session profiles, semantic recommendation recall/ranking, impression
  training exports, learned policy weights, historical backfill, HNSW, shadow rollout, and model
  training or fine-tuning.

## Capabilities

### New Capabilities

- `multimodal-video-embeddings`: Defines the environment-neutral multimodal provider contract,
  public-content/privacy boundary, immutable model identity, newly published video job lifecycle,
  conditional vector persistence, fallback, and observability.
- `multimodal-video-discovery`: Defines exact semantic retrieval, deterministic hybrid public-video
  search, similar-video discovery, visibility revalidation, cursor stability, degradation, and
  quality/latency evidence.

### Modified Capabilities

- `global-search`: Extends public video search from lexical-only relevance to a deterministic
  lexical + multimodal semantic hybrid when the active vector contract is available, without
  changing user search or weakening query/cursor safety.

## Impact

- Affects embedding domain/application code, publication-event handoff, PostgreSQL migrations and
  persistence, media frame preparation adapters, provider configuration, Worker composition,
  exact vector retrieval, public video search, a new similar-video HTTP/Web integration point,
  metrics, tests, and embedding/search documentation.
- Replaces the intended first implementation role of the text-only
  `add-semantic-embedding-service` and `integrate-semantic-video-embeddings` plans; those existing
  changes must be reconciled or retired before implementation so two incompatible active contracts
  are not built in parallel.
- Adds no requirement to preserve or scan development-era historical videos. Test, demo, and
  golden-set videos obtain vectors through the new-publication path or explicit development
  fixtures.
- Adds no recommendation-policy activation or user-visible claim of personalized uplift.
