## Context

Frux already consumes first-publication facts in an independent embedding Kafka Group and
conditionally persists deterministic `hash-ngram-v1` rows. Public video search is lexical-only,
and content-similar recommendation uses the local hash-vector abstraction. The repository also has
active plans for managed text embeddings, historical backfill, semantic user profiles, pgvector
ANN recall, and shadow evaluation, but none of their implementation tasks have started.

The direction has changed in three material ways. First, content understanding should include
prepared visual frames as well as title/description text. Second, the model runtime and deployment
topology must remain an interchangeable provider detail rather than an external-API requirement.
Third, development-era historical data does not need migration: the first useful slice should cover
newly published videos, exact discovery, and visible search/similarity behavior before profiles,
recommendation activation, backfill, or ANN indexing.

This change is therefore a fresh vertical slice rather than a rename of the existing text-only
changes. It must coexist safely with the current product until those older plans are explicitly
reconciled or retired.

## Goals / Non-Goals

**Goals:**

- Define an environment-neutral multimodal provider and immutable compatibility contract.
- Generate validated multimodal vectors asynchronously for newly published public, media-ready
  videos without delaying publication, Kafka source progress after durable handoff, or Feed.
- Preserve authoritative versioned vector facts and a rebuildable exact-retrieval projection.
- Add exact cosine semantic retrieval, hybrid public video search, and similar-video discovery.
- Preserve lexical search and every existing Feed/recommendation path when semantic coverage or the
  provider is unavailable.
- Establish bounded privacy, admission, cursor, observability, golden-set, and rollback contracts.

**Non-Goals:**

- Selecting a programming language, model family, hardware, process boundary, external vendor, or
  deployment topology for the provider.
- Training or fine-tuning a model.
- Scanning or backfilling videos that predate the feature.
- Building semantic session or long-term user profiles.
- Adding a semantic recommendation RecallProvider or changing `recommend/v1`/`recommend/v2`.
- Persisting training datasets or learning ranking weights.
- Creating HNSW/IVFFlat or any approximate-nearest-neighbor index.
- Claiming online CTR, completion, retention, or causal lift.

## Decisions

### 1. Use one application-owned provider with separate video and query operations

The embedding application owns a `MultimodalEmbeddingProvider` abstraction with two operations:

- video-content embedding accepts normalized public title/description plus prepared, bounded
  cover/keyframe inputs and runs only from a durable background job;
- query-text embedding accepts one normalized public video-search query and may run synchronously
  only behind a cache, deadline, non-blocking admission bound, and lexical fallback.

Both return vectors in the same immutable model space. Provider SDK types, URLs, process models,
hardware identifiers, and language-specific objects remain in Infrastructure.

Alternative: require an external API or a local model service. Rejected because either choice would
turn a deployment decision into a product contract and repeat the limitation of the text-only plan.

Alternative: forbid all synchronous provider access. Rejected for this vertical slice because an
arbitrary natural-language query cannot be compared with video vectors without a compatible text
encoder. The narrower rule is that video publication and recommendation Feed never wait for model
inference; public search may attempt one bounded query operation and fall back on its first page.

### 2. Frux prepares bounded visual inputs; the provider owns model-specific encoding and fusion

Frux selects media under a versioned frame policy, decodes orientation correctly, applies safe
pixel/count/byte limits, and supplies prepared content. The provider contract owns model-specific
image/text preprocessing and returns one final normalized video vector under a versioned fusion
policy.

The first contract may select a small fixed number of representative frames, but the exact count,
sampling positions, resolution, and fusion weights belong to immutable policy identifiers rather
than hard-coded product requirements.

Alternative: persist independent online text and image vectors and fuse them in every query.
Deferred because it multiplies storage, compatibility, ranking, and migration states before the
project has evidence that runtime-selectable fusion is useful. Evaluation tools may retain
component vectors outside the online contract.

### 3. Reuse first-publication intake but split durable handoff from provider execution

The existing embedding publication consumer continues to make `hash-ngram-v1` safe first. It then
idempotently creates or refreshes one exact-contract multimodal job and may commit the source Kafka
record after that PostgreSQL handoff. Provider execution occurs in a separate leased Worker path.

Jobs use database time, stable claim tokens, lease fencing, heartbeat/reclaim, bounded attempts and
backoff, explicit retry/terminal classes, manual requeue, and conditional cleanup. A process-local
non-blocking admission bound prevents a slow or cancellation-ignoring provider from creating an
unbounded queue.

Alternative: invoke the provider inside the Kafka handler. Rejected because provider latency,
quota, or failure would block the source Group and couple semantic freshness to publication
progress.

### 4. Treat old and missing vectors as normal during development

This change does not scan existing videos. Only newly published videos and explicit development
fixtures enter the job path. A readable video without an active-contract vector remains available
to lexical search and all existing Feed providers but is absent from semantic retrieval.

Model-contract changes create isolated new rows. Development environments may regenerate fixtures
or recreate disposable data, but no production-grade checkpointed backfill is required here.

Alternative: require complete historical coverage before enabling discovery. Rejected because the
repository has no production migration obligation and the complexity would delay the first
measurable content-understanding slice.

### 5. Keep authoritative facts separate from retrieval projection

Authoritative rows store the exact contract, source hash, validated normalized vector, digest, and
timestamps. A separate projection contains only current, published, public, media-ready rows for
the active contract. Projection reconciliation performs equality-checked upserts and stale-row
removal from authoritative video and embedding facts.

The concrete vector representation may use a pgvector column for exact search, but this change does
not create an ANN index. Authoritative data remains sufficient to rebuild the projection.

Alternative: query JSON vectors directly from application memory. Rejected as the primary design
because it transfers the entire eligible corpus for every query and obscures database-side
filtering, cancellation, and query-cost evidence. A bounded in-memory implementation may exist only
as a test double.

### 6. Use exact cosine before approximate search

Every semantic query filters to the active contract and readable/current projection, compares all
eligible rows, and orders by cosine similarity followed by deterministic `published_at DESC,
video_id DESC` tie-breaking. Query limit is bounded, and similar-video queries exclude the source
ID.

Exact results provide the quality baseline and avoid index lifecycle, approximate recall loss, and
filtered-ANN fill behavior. HNSW requires a separate future change justified by measured eligible
row count, P95/P99 latency, database CPU, and concurrency.

### 7. Hybrid video search keeps lexical retrieval as the stable fallback

The existing video endpoint remains the public entry point. First-page processing validates the
query, runs lexical retrieval, then attempts a cached or single bounded query embedding. When the
vector is available, exact semantic candidates are merged with lexical candidates using a
versioned deterministic rule with explicit reservations and deduplication. When it is unavailable,
the response is lexical-only and semantic degradation is observed internally.

Hybrid cursor payloads bind normalized query, result category, retrieval mode, hybrid version,
model contract, last complete ordering tuple, and expiry. A hybrid continuation cannot silently
become lexical-only; if the compatible query vector cannot be reproduced, the API returns a typed
retryable error. Lexical cursors continue independently.

Alternative: return separate lexical and semantic tabs. Rejected for the first slice because it
does not improve the normal search experience and moves merge responsibility to the client.

### 8. Add similar-video discovery as a separate bounded resource

The initial API shape is a typed similar collection under the source video, for example
`GET /api/videos/{videoId}/similar?limit=&cursor=`. It validates source readability and active
contract coverage before exact search. The cursor binds source ID, model contract, ordering tuple,
and expiry.

The Web may initially expose similar discovery from the existing video destination or a development
surface; this change does not require a Feed scene or recommendation-policy integration.

### 9. Bound content, query, logging, and metrics at the boundary

Video provider input contains only normalized public content and prepared image bytes. Query input
contains only the normalized public search text. Neither carries identity, behavior, session,
request metadata, credentials, signed URLs, comments, or arbitrary metadata.

Configuration bounds image count, pixels, bytes, MIME types, query length, provider concurrency,
deadline, cache TTL, result count, and retry policy. Logs and metrics use fixed outcome, mode,
reason, and contract aliases; raw query text, images, vectors, credentials, raw errors, and IDs do
not become normal log fields or metric labels.

### 10. Gate semantic enablement on deterministic evidence, not production uplift

Tests cover provider validation, job fencing, source races, exact retrieval, hybrid merge, cursors,
degradation, and visibility. A small human golden set compares lexical, text-only when available,
image-only when available, and final multimodal relevance, and records model/merge version,
denominators, ranking metrics, and latency.

The golden set is a regression and development enablement gate. It does not establish causal online
lift. Public datasets and session recommendation are owned by later changes so this slice stays
bounded.

## Risks / Trade-offs

- [The selected pretrained model performs poorly on the project's language or video domains] →
  Require a versioned golden-set spike before enabling hybrid search and keep lexical fallback.
- [Frame selection misses the relevant visual moment] → Version the bounded frame policy, retain
  source/fusion identity, and evaluate representative failure cases before changing the contract.
- [Synchronous query embedding adds search latency] → Cache by normalized query and contract, allow
  one bounded attempt with no HTTP retry loop, cap admission, and fall back on the first page.
- [Hybrid pagination becomes inconsistent when semantic service availability changes] → Bind the
  cursor to retrieval mode and model contract; never change modes inside one cursor sequence.
- [Provider returns plausible but incompatible vectors] → Validate complete identity, dimension,
  components, norm, input hash, and digest before persistence or query use.
- [Projection drifts from video lifecycle facts] → Revalidate at query time, expose stale-row
  metrics, and reconcile from authoritative facts.
- [Exact cosine becomes slow as the catalog grows] → Record eligible-row and latency evidence;
  introduce HNSW only in a separate measured capacity change.
- [Existing text-semantic changes conflict with this contract] → Do not implement both; reconcile
  or retire the text-only changes before applying this change.

## Migration Plan

1. Reconcile the recommendation roadmap and mark the existing text-only embedding/live-integration
   changes as superseded or future reference before implementation begins.
2. Add disabled-by-default multimodal provider, contract, storage, job, and projection
   configuration plus additive migrations.
3. Implement provider validation and development fixtures, then prove one exact contract with a
   small golden set.
4. Enable newly published video job creation while hybrid search remains disabled; observe job and
   coverage metrics.
5. Enable exact similar-video discovery for compatible fixtures/new videos.
6. Enable hybrid public video search in development after lexical fallback, cursor, quality, and
   latency tests pass.
7. Keep all existing recommendation policies and Feed providers unchanged.

Rollback disables query embedding, hybrid search, similar discovery, and new multimodal job
creation. Lexical search, `hash-ngram-v1`, publication, and Feed remain available. Additive job,
fact, and projection rows may remain for investigation or be purged by a later explicit operator
procedure; rollback does not require historical reconstruction.

## Open Questions

- Which concrete first model contract and output dimension pass the project golden set? This is an
  implementation spike, not an architecture constraint.
- Should similar-video absence return `200` with an empty typed page or a dedicated semantic
  coverage status? The API convention should be chosen before DTO implementation.
- What exact deterministic hybrid formula and lexical/semantic reservations should version `v1`
  use? The golden set must compare at least lexical-only and simple reciprocal/rank-based options.
- Should prepared frame bytes be transient in memory or use a bounded temporary-file adapter? Both
  must preserve the same privacy and cleanup contract.
