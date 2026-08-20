## 1. Planning Reconciliation and Contract Baseline

- [x] 1.1 Reconcile or retire `add-semantic-embedding-service` and
  `integrate-semantic-video-embeddings` so implementation has one active embedding contract and no
  external-text-only prerequisite.
- [x] 1.2 Update `docs/recommendation-roadmap.md` to place multimodal discovery before Session
  recommendation, remove development-history backfill from the first implementation path, and keep
  HNSW behind measured Exact capacity gates.
- [x] 1.3 Define typed configuration for enablement, active contract aliases, dimension, text/frame/
  preprocessing/fusion policy IDs, provider deadlines/admission, image limits, query cache, exact
  limits, hybrid version, and lexical fallback with disabled-safe defaults.
- [x] 1.4 Add configuration validation proving partial contracts, unknown policies, weak bounds,
  incompatible dimensions, and semantic enablement without required dependencies fail startup while
  disabled mode requires no provider.

## 2. Multimodal Provider and Input Contracts

- [x] 2.1 Add application-owned video-content and query-text embedding interfaces and typed
  request/result contracts without provider SDK, hardware, process, language, URL, or storage types.
- [x] 2.2 Implement canonical contract identity and equality helpers covering provider/model/
  revision/dimension/canonicalizer/frame/preprocessing/fusion policies, source input hash, and vector
  digest.
- [x] 2.3 Implement strict vector validation for exact identity, dimension, finite components,
  normalization tolerance, input hash, and digest with bounded closed failure codes.
- [x] 2.4 Implement bounded normalized public-video text and public-query canonicalization without
  persisting raw canonical text or attaching identity/session/request metadata.
- [x] 2.5 Add a media preparation port and Infrastructure adapter that selects and prepares bounded
  cover/keyframe inputs under the configured frame/preprocessing policy with deterministic hashes
  and cleanup.
- [x] 2.6 Add provider adapter test doubles and contract tests proving video/query vectors share one
  model space, provider implementations are replaceable, and ineligible/private inputs never cross
  the application boundary.

## 3. Persistence and Additive Migrations

- [x] 3.1 Add PostgreSQL models and additive migrations for multimodal jobs, authoritative
  model-isolated vector facts, job operation receipts where needed, and rebuildable exact-retrieval
  projection rows.
- [x] 3.2 Add constraints and indexes for exact contract identity, source hash, one active fact per
  video/contract, lease scans, retry scans, terminal inspection, and projection eligibility without
  creating HNSW/IVFFlat indexes.
- [x] 3.3 Implement repositories for idempotent handoff, database-time claim, heartbeat, fenced
  retry/success/terminal transitions, manual requeue, cleanup, authoritative fact persistence, and
  equality-checked projection reconciliation.
- [x] 3.4 Add real PostgreSQL tests for concurrent handoff, lease reclaim, stale-token fencing,
  source-change conflicts, duplicate success, terminal requeue, active-contract isolation, and
  stale projection removal.
- [x] 3.5 Prove existing readable videos without multimodal rows remain valid and that migrations
  create no automatic historical jobs or full-catalog scan.

## 4. Newly Published Video Job Handoff

- [x] 4.1 Extend the existing video-publication embedding consumer to create/reuse an exact-contract
  multimodal job only after `hash-ngram-v1` is durably safe and the video is published, public, and
  media-ready.
- [x] 4.2 Keep provider execution outside the Kafka handler and allow source offset commit only after
  durable multimodal handoff or a registered terminal/no-op decision.
- [x] 4.3 Define source/retry/DLQ commit boundaries and idempotency tests for duplicate publication,
  transient PostgreSQL failure, poison input, and replay without coupling provider retries to Kafka.
- [x] 4.4 Add integration tests proving publication, Feed fanout, hash embedding, and Kafka source
  progress remain available when multimodal handoff is a no-op, delayed, retried, or terminal.

## 5. Multimodal Job Execution

- [x] 5.1 Implement a bounded Worker loop with non-blocking provider admission, database-time claim,
  heartbeat, cancellation, reclaim, retry classification/backoff, terminal classification, and
  graceful shutdown.
- [x] 5.2 Re-read and revalidate video publication, visibility, media readiness, selected content,
  source hash, and contract immediately before media preparation and provider access.
- [x] 5.3 Execute one bounded video-content provider call per attempt, validate the complete result,
  and conditionally persist only while the lease and source/contract remain current.
- [x] 5.4 Discard stale results when content or lifecycle changes during inference and schedule the
  appropriate no-op, retry, or refreshed exact-contract job without overwriting newer facts.
- [x] 5.5 Add focused unit/concurrency tests for provider timeout, admission exhaustion, invalid
  vectors, cancellation-ignoring calls, heartbeat loss, stale source, retry-after, duplicate
  completion, terminal failure, manual requeue, and shutdown.

## 6. Projection and Exact Retrieval

- [x] 6.1 Implement bounded projection reconciliation from authoritative active-contract facts with
  published/public/media-ready/source-equality checks and deterministic stale deletion.
- [x] 6.2 Implement database-side exact cosine query with active-contract filtering, bounded top-K,
  exclusions, context cancellation/deadline, positive finite similarity, and deterministic
  similarity/`published_at`/video-ID ordering.
- [x] 6.3 Add exact-query repository tests for empty coverage, ties, exclusions, mixed contracts,
  unreadable videos, stale projections, source mismatch, limit bounds, and cancellation.
- [x] 6.4 Add a modest reproducible benchmark recording eligible rows, Exact P50/P95/P99, database
  CPU/query plan, and result quality without defining or creating an ANN index.

## 7. Hybrid Public Video Search

- [x] 7.1 Add bounded semantic query-vector caching keyed by normalized query plus active contract,
  with TTL, validation, no raw-query persistence beyond existing search handling, and safe invalidation
  on contract change.
- [x] 7.2 Implement one no-retry synchronous query embedding attempt behind deadline and
  non-blocking admission, with lexical-only first-page fallback for disabled, saturated, failed,
  timed-out, or invalid semantic results.
- [x] 7.3 Implement a versioned deterministic lexical/semantic candidate merge with explicit
  reservations, video-ID deduplication, retained internal reasons, stable hybrid scoring, bounded
  pool size, and final readability revalidation.
- [x] 7.4 Version video-search cursors with normalized query, result category, lexical/hybrid mode,
  merge version, model contract where applicable, complete ordering tuple, and expiry; reject legacy
  or rebound cursors.
- [x] 7.5 Preserve hybrid mode across continuation pages and return a typed retryable error when a
  compatible query vector cannot be reproduced instead of silently switching to lexical ordering.
- [x] 7.6 Add API-flow and Web regressions for lexical-only fallback, hybrid exact/title matches,
  semantic-only matches, duplicates, stale results, independent user search, cursor pagination,
  stale requests, empty/error/loading states, and encoded query navigation.

## 8. Similar-Video Discovery

- [x] 8.1 Add a typed bounded similar-video application service using the readable source video's
  active-contract vector, source exclusion, exact retrieval, final readability checks, and stable
  source/model-bound cursor semantics.
- [x] 8.2 Add the public similar-video HTTP route, DTOs, validation, typed semantic-unavailable/empty
  behavior, error mapping, and router composition without exposing raw model or vector data.
- [x] 8.3 Add the minimal Web integration at an existing video destination or development surface,
  preserving current playback/navigation state and truthful unavailable/empty/loading/error states.
- [x] 8.4 Add domain, repository, API-flow, and Web tests for readable/unreadable sources, missing
  vectors, exact order, self exclusion, stale neighbors, cursor rebinding, pagination, and identity
  changes during pending requests.

## 9. Observability, Operations, and Evaluation Evidence

- [x] 9.1 Add fixed-label metrics for job state/backlog/oldest age, provider result/duration/admission,
  active-contract coverage, projection drift, exact-query latency/result count, query-cache outcome,
  hybrid mode/degradation/overlap/contribution, and similar-video empty/filter outcomes.
- [ ] 9.2 Add bounded operator inspection and manual requeue for multimodal jobs with permission,
  audit, cursor, and closed-detail contracts that expose no images, vectors, raw queries, credentials,
  signed URLs, or arbitrary provider errors.
- [ ] 9.3 Create a small versioned human golden set and deterministic evaluation command comparing
  lexical-only, available component baselines, and multimodal hybrid relevance with denominators,
  model/merge versions, Recall/NDCG-style metrics, overlap, and latency.
- [x] 9.4 Add privacy/logging tests proving raw images, vectors, query text, credentials, user/video/
  request IDs, model strings, and raw errors never become metric labels or normal log payloads.
- [ ] 9.5 Document enablement, contract changes, development fixture generation, disabled behavior,
  query fallback, exact-capacity evidence, operator inspection, rollback, and the explicit absence of
  historical backfill/HNSW/recommendation activation.

## 10. Integration Verification

- [ ] 10.1 Update `docs/modules/embedding.md`, `docs/modules/search.md`, `docs/product.md`, relevant
  architecture/engineering sections, and roadmap status without presenting future Session
  recommendation or ANN work as implemented.
- [ ] 10.2 Run targeted embedding, publication-consumer, persistence, search, similar-video, router,
  metrics, and Web tests plus real PostgreSQL integration tests when `FRUX_POSTGRES_TEST_DSN` is
  available.
- [ ] 10.3 Run `cd apps/api && go test ./...`, compile `./cmd/feed` and `./cmd/worker`, then run Web
  lint/test/build and relevant Compose configuration validation.
- [ ] 10.4 Run `openspec validate --all --strict` and confirm the implementation adds no historical
  scan/backfill, HNSW/ANN index, semantic user profile, recommendation Provider/policy activation,
  training export, learned weights, or model-training runtime requirement.
