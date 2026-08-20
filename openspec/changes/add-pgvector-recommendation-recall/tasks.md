## 1. Provider Contracts and Enablement

- [ ] 1.1 Confirm the accepted outputs of `enable-pgvector-recommendation-index` and `project-semantic-user-interest`, then define application-owned `SemanticANNIndex`, `SemanticANNProfileSource`, and bounded `SemanticSessionVectorSource` interfaces without persistence, SQL, pgvector, or inference-client types.
- [ ] 1.2 Add the `semantic_ann` provider token, `semantic_similarity` feature token, `semantic-query-v1` weights session `0.50`/recent `0.30`/long-term `0.20`, budget `1..100`, deadline `25..500ms`, reservation bounds, exclusion limit 20, usable-vector norm `1e-6`, and semantic capacity `1..16`.
- [ ] 1.3 Add disabled-by-default `recommendation.semantic_ann.enabled` and separate semantic-capacity configuration; do not add index lifecycle, projection, reconciliation, backfill, active rollout, or shadow sampling settings.
- [ ] 1.4 Add interface/configuration contract tests proving defensive values, disabled defaults, fixed bounds, and rejection of partial or incompatible enabled composition.

## 2. Dormant Semantic ANN Provider

- [ ] 2.1 Implement fixed session/recent/long-term fusion with finite/norm checks, missing-component weight renormalization, request-local normalization, and healthy empty/hash fallback when no semantic component is usable.
- [ ] 2.2 Build the bounded session vector from persisted pretrained embeddings and extract at most 20 current/recent session video IDs into a defensive exclusion list without inference calls.
- [ ] 2.3 Implement a separate non-blocking semantic semaphore acquired before provider work so semantic capacity never consumes baseline-provider permits.
- [ ] 2.4 Implement `SemanticANNRecallProvider` to perform bounded profile/session preparation and at most one cancellable exact/ANN query using future policy budget/deadline, with no queue, retry, widening, or detached work.
- [ ] 2.5 Convert valid neighbors into candidates with exactly one `semantic_ann` reason, matching source score, and `semantic_similarity` feature from finite positive cosine clamped to `[0,1]`; map absence/failure to healthy empty or bounded degradation without partial candidates.

## 3. Provider Mixing and Ranking

- [ ] 3.1 Replace global `published_at` pre-rank truncation with duplicate merge, validated provider reservations, and deterministic round-robin fill before feature extraction/ranking.
- [ ] 3.2 Add a separate semantic pool reservation and require every future semantic policy to retain at least one baseline provider with non-zero budget/reservation.
- [ ] 3.3 Extend policy normalization to require complete semantic budget, deadline, reservation, and positive finite `semantic_similarity` weight entries; reject partial fields, out-of-range values, or semantic-only policies.
- [ ] 3.4 Add exact serialization regressions proving `InitialRecommendationPolicies` and `EnsureInitialPolicies` leave v1/v2 byte-for-byte free of `semantic_ann`, `semantic_similarity`, and semantic reservations.
- [ ] 3.5 Add service regressions for duplicate reason retention, provider underfill, semantic and baseline reservation survival, deterministic mixing, no global recency truncation, explicit semantic ranking, diversity/suppression/visibility, snapshots, attribution, and hash fallback.

## 4. Composition and Active Metrics

- [ ] 4.1 Compose and register `semantic_ann` in the API only when enablement is true and compatible profile/index implementations are available; leave worker composition unchanged.
- [ ] 4.2 Fail enabled API startup with a bounded prerequisite/configuration error when either implementation is missing or incompatible, while disabled startup requires neither prerequisite.
- [ ] 4.3 Add bounded provider result/duration, fusion-component, candidate-count, semantic-capacity, query-mode, and reservation-survival metrics using only fixed labels.
- [ ] 4.4 Add metrics/logging tests proving user/video/request IDs, vectors, candidates, model strings, SQL/index details, and raw errors never become labels or normal log fields.

## 5. Focused Verification and Documentation

- [ ] 5.1 Add provider unit tests for all session/recent/long-term availability combinations, fixed weights and renormalization, empty fallback, normalization, exclusion truncation, separate capacity, one-query behavior, cosine feature annotation, and invalid scores.
- [ ] 5.2 Add executor/service tests for omitted-policy non-execution, semantic timeout/cancellation/capacity/index failure, baseline-capacity preservation, healthy-provider continuation, empty results, and no partial merge.
- [ ] 5.3 Add composition tests for disabled mode, enabled compatible dependencies, enabled missing dependencies, and registration without policy activation.
- [ ] 5.4 Update `docs/modules/recommendation.md` and configuration references with prerequisite boundaries, fixed session/recent/long-term fusion, semantic capacity, deterministic reservations/mixing, explicit semantic ranking, baseline-provider guard, registration-only scope, shadow-first dependency, fallback, and infrastructure exclusions.
- [ ] 5.5 Run targeted recommendation domain/application/router/config/metrics tests, compile `./cmd/feed` and `./cmd/worker`, then run `cd apps/api && go test ./...`.
- [ ] 5.6 Run `openspec validate --all --strict` and confirm the change contains no semantic-video/profile deltas, pgvector lifecycle tasks, shadow execution, v1/v2 edits, created/selected semantic policy, or active gray rollout.
