## Context

Frux executes bounded `RecallProvider` implementations through policy budgets/deadlines, duplicate merge, visibility revalidation, ranking, and degraded-provider recording. Bootstrap `recommend/v1` and `recommend/v2` contain only existing providers. The current multi-provider path may globally order the merged pool by `published_at` and truncate before ranking, which can erase a provider's candidates before its ranking signal is evaluated.

This change registers a dormant `semantic_ann` provider and fixes the future candidate-mixing and ranking contract. `enable-pgvector-recommendation-index` owns exact/HNSW search infrastructure; `project-semantic-user-interest` owns immutable per-user recent/long-term profiles. Provider registration must not make either prerequisite a Feed correctness dependency, and no active semantic policy or gray rollout is introduced here. `shadow-semantic-ann-recall` must precede any later activation.

## Goals / Non-Goals

**Goals:**

- Add the `semantic_ann` provider token and narrow application interfaces.
- Build one query vector by fixed explicit fusion of available session, recent, and long-term pretrained semantic vectors.
- Bound semantic execution with a separate no-queue capacity slot, policy budget/deadline, and bounded exclusions.
- Replace premature global recency truncation with deterministic provider reservations and mixing before ranking.
- Expose cosine as both recall metadata and an explicit `semantic_similarity` ranking component.
- Validate future semantic policies while preserving at least one baseline provider and keeping bootstrap v1/v2 unchanged.
- Compose and register the provider disabled by default, then stop before active policy rollout.

**Non-Goals:**

- Installing/configuring pgvector, adding migrations, or owning projection/index lifecycle.
- Changing semantic embedding/profile persistence rules.
- Training individual, cohort, or population models.
- Creating, selecting, or canarying an active semantic policy.
- Changing bootstrap `recommend/v1` or `recommend/v2`.
- Implementing shadow evaluation; that is the mandatory next change.

## Decisions

### 1. Depend on narrow prerequisite interfaces

The recommendation application owns:

- `SemanticANNProfileSource`, returning compatible recent, long-term, and negative vectors;
- `SemanticSessionVectorSource`, building a bounded request-session vector from persisted exact-model embeddings for current/recent session videos;
- `SemanticANNIndex`, accepting a normalized query vector, budget, and exclusions and returning readable candidate facts with cosine similarity.

The interfaces expose no SQL, pgvector, projection, persistence, provider/model metadata, or embedding-service client. Enabled composition fails closed when implementations are absent or incompatible; missing user data is a healthy runtime absence.

### 2. Fuse session, recent, and long-term interest explicitly

`semantic-query-v1` uses fixed weights: session `0.50`, recent `0.30`, long-term `0.20`. A component is usable only when finite, compatible, and norm at least `1e-6`. Missing components contribute zero and remaining configured weights are renormalized to one. Thus usable recent interest never completely replaces usable long-term interest.

The weighted sum is normalized on a defensive request-local copy. If no component is usable, semantic recall returns healthy empty and existing hash/non-vector providers remain the fallback. The provider does not call an embedding API, mutate profile data, or train a model. Negative interest remains available for a future explicitly specified rule and is not silently mixed into `semantic-query-v1`.

### 3. Give semantic recall separate bounded capacity

Semantic calls use a dedicated process-local non-blocking semaphore, default 2 and bounded `1..16`, acquired before profile/session/index work. It never consumes the baseline provider permit pool. The provider still uses a future policy budget `1..100`, deadline `25..500ms`, at most 20 session exclusions, one profile/session read phase, and at most one index query. It never queues, retries, scans the corpus itself, or continues detached after cancellation.

### 4. Make semantic similarity a ranking component

Each valid neighbor contributes exactly one `semantic_ann` recall reason and matching source score. The finite positive cosine is clamped to `[0,1]` and exposed to candidate feature extraction as `semantic_similarity`.

A future policy containing `semantic_ann` is valid only when it assigns a positive finite weight to `semantic_similarity`. This prevents semantic candidates from being recalled and then ordered only by hash/recency features. v1/v2 omit both provider and feature and remain byte-for-byte unchanged.

### 5. Reserve provider contributions before global ranking

Provider-local outputs remain bounded and stably ordered. Before ranking, the service:

1. merges duplicate IDs while retaining all reasons/scores;
2. satisfies each selected provider's validated unique-candidate reservation, including a separate semantic reservation and at least one baseline reservation;
3. fills remaining pool capacity by deterministic round-robin over fixed provider order and provider-local order;
4. only then computes features and ranks the bounded pool.

The service does not globally sort all provider outputs by `published_at` and truncate before this step. Duplicates occupy one global slot; if a provider returns fewer unique candidates than reserved, unused capacity returns to deterministic fill.

### 6. Validate future policy opt-in without activating it

Configuration adds `recommendation.semantic_ann.enabled`, default `false`. Disabled composition constructs nothing. Enabled composition validates the three interfaces and registers the provider.

A future policy may reference semantic ANN only when budget `1..100`, deadline `25..500ms`, reservation `1..budget`, and positive `semantic_similarity` weight are present together. It must also retain at least one baseline provider from Fresh, Hot, content similarity, followed author, or session continuation with non-zero budget and reservation.

`InitialRecommendationPolicies` and `EnsureInitialPolicies` remain free of `semantic_ann`, `semantic_similarity`, and semantic reservations. This change creates, selects, or rolls out no semantic policy.

### 7. Preserve failure isolation and hash fallback

Missing semantic profile/session data and empty search results are healthy empty outcomes. Profile, session-vector, index, timeout, cancellation, invalid-result, or semantic-capacity failures produce no partial semantic set and do not reduce baseline-provider capacity. Existing hash/non-vector fallback, final readability checks, suppression, snapshots, evidence, attribution, and response contracts remain available.

### 8. Observe bounded dormant-provider behavior

Metrics cover provider attempts, duration, result, candidate count, fusion-component availability, semantic capacity, reservation survival, and selected physical query mode using fixed enums. User/video/request IDs, vectors, model strings, candidate lists, SQL/index details, and raw errors never appear in labels or normal logs.

### 9. Stop at registration and hand off to shadow

Deployment may enable and verify provider composition while selected policies remain v1/v2 or otherwise omit `semantic_ann`. `shadow-semantic-ann-recall` then invokes the registered provider outside production merge and evaluates safety/usefulness. Any active gray rollout requires shadow completion and a separate accepted change.

Rollback disables provider composition. Because this change activates no policy, no semantic request traffic or data rollback is required.

## Risks / Trade-offs

- [Semantic calls consume baseline capacity] → Use a separate bounded no-queue semantic semaphore.
- [Global recency truncation hides semantic contribution] → Reserve and deterministically mix provider outputs before ranking.
- [Semantic candidates are ranked only by hash features] → Require explicit positive `semantic_similarity` policy weight.
- [Fusion rules drift] → Version fixed weights as `semantic-query-v1` and test missing-component renormalization.
- [Registration is mistaken for rollout] → Keep v1/v2 unchanged and require shadow plus a separate activation change.

## Migration Plan

1. Complete and validate `enable-pgvector-recommendation-index` and `project-semantic-user-interest`.
2. Add application interfaces, fixed fusion, provider token, separate semantic admission, deterministic pool mixing, semantic feature extraction, future-policy validation, metrics, and disabled composition.
3. Prove bootstrap v1/v2 serialization and existing recommendation behavior remain unchanged.
4. Enable provider registration in a prepared environment while every selected policy omits `semantic_ann`.
5. Implement and run `shadow-semantic-ann-recall`.
6. Do not create, select, or roll out an active semantic policy in this change.

## Open Questions

None. Infrastructure is owned by `enable-pgvector-recommendation-index`; this change ends at safe provider registration, and shadow must precede a separate active-rollout proposal.
