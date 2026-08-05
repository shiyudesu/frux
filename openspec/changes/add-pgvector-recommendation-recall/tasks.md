## 1. Provider Contracts and Enablement

- [ ] 1.1 Confirm the accepted outputs of `enable-pgvector-recommendation-index` and `project-semantic-user-interest`, then define application-owned `SemanticANNIndex` and `SemanticANNProfileSource` interfaces without persistence, SQL, or pgvector types.
- [ ] 1.2 Add the `semantic_ann` recall-provider token and provider-specific constants for budget `1..100`, deadline `25..500ms`, exclusion limit 20, and usable-vector norm `1e-6` without adding a feature token.
- [ ] 1.3 Add disabled-by-default `recommendation.semantic_ann.enabled` configuration and bounded validation for provider composition only; do not add index, projection, reconciliation, backfill, or shadow settings.
- [ ] 1.4 Add interface/configuration contract tests proving defensive values, disabled defaults, fixed bounds, and rejection of partial or incompatible enabled composition.

## 2. Active Semantic ANN Provider

- [ ] 2.1 Implement recent-first, long-term-fallback profile selection with finite/norm checks and request-local normalization, returning healthy empty for absent or empty profiles.
- [ ] 2.2 Extract at most 20 current/recent session video IDs from the bounded recommendation context into a defensive ANN exclusion list.
- [ ] 2.3 Implement `SemanticANNRecallProvider` to perform one profile read and at most one cancellable index query using the policy budget and existing provider deadline context, with no retry or detached work.
- [ ] 2.4 Convert valid neighbors into candidates with exactly one `semantic_ann` recall reason and matching source score from finite positive cosine similarity clamped to `[0,1]`.
- [ ] 2.5 Map missing/empty data to healthy empty output and propagate profile, index, timeout, cancellation, and invalid-score failures through the existing bounded provider degradation behavior without partial candidates.

## 3. Policy, Merge, and Ranking Preservation

- [ ] 3.1 Extend policy normalization to allow `semantic_ann` only when matching recall budget and provider deadline entries satisfy the semantic-specific bounds.
- [ ] 3.2 Reject incomplete semantic maps, out-of-range semantic values, unknown provider tokens, and any `semantic_ann` feature-weight entry.
- [ ] 3.3 Add exact serialization regression tests proving `InitialRecommendationPolicies` and `EnsureInitialPolicies` leave bootstrap `recommend/v1` and `recommend/v2` byte-for-byte free of `semantic_ann`.
- [ ] 3.4 Add service regressions proving duplicate merge retains semantic and existing reasons while ranking features/weights, diversity, suppression, visibility checks, snapshots, attribution, and hash fallback remain unchanged.

## 4. Composition and Active Metrics

- [ ] 4.1 Compose and register `semantic_ann` in the API only when enablement is true and compatible profile/index implementations are available; leave worker composition unchanged.
- [ ] 4.2 Fail enabled API startup with a bounded prerequisite/configuration error when either implementation is missing or incompatible, while disabled startup requires neither prerequisite.
- [ ] 4.3 Add bounded active-provider attempt, duration, result, candidate-count, and profile-source metrics using only the specified fixed result/source labels.
- [ ] 4.4 Add metrics/logging tests proving user/video/request IDs, vectors, candidates, model strings, SQL/index details, and raw errors never become labels or normal log fields.

## 5. Focused Verification and Documentation

- [ ] 5.1 Add provider unit tests for recent selection, long-term fallback, empty profile, normalization, exclusion truncation, one-query behavior, budget propagation, cosine annotations, and invalid neighbor scores.
- [ ] 5.2 Add executor/service tests for omitted-policy non-execution, active timeout/cancellation/capacity/index failure, healthy-provider continuation, empty ANN results, and no partial merge.
- [ ] 5.3 Add composition tests for disabled mode, enabled compatible dependencies, enabled missing dependencies, and registration without policy activation.
- [ ] 5.4 Update `docs/modules/recommendation.md` and existing configuration references touched by enablement with prerequisite boundaries, enablement plus policy opt-in, recent-then-long selection, active metrics, degradation, rollout, rollback, and explicit infrastructure/shadow exclusions.
- [ ] 5.5 Run targeted recommendation domain/application/router/config/metrics tests, compile `./cmd/feed` and `./cmd/worker`, then run `cd apps/api && go test ./...`.
- [ ] 5.6 Run `openspec validate --all --strict` and confirm the change contains no semantic-video/profile deltas, pgvector infrastructure tasks, reconciliation/backfill, query-plan/performance acceptance, shadow execution, or ranking-weight changes.
