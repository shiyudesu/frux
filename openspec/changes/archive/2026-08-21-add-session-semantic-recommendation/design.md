## Context

Frux currently has five bounded Recommendation Recall Providers: `fresh`, `hot`,
`content_similarity`, `followed_author`, and `session_continuation`. The last two vector paths use the
local `hash-ngram-v1` representation: `content_similarity` loads the materialized user profile and
`session_continuation` averages `current_video_id` plus `recent_video_ids`. The Ranker already has a
hash-based `session_similarity` component, complete candidate scoring, deterministic Provider quota
merge, feedback/exposure suppression, sampled request logs, and stable Snapshot pagination.

The multimodal path is separately complete and real-provider validated. PostgreSQL owns immutable
active-contract vector facts and a rebuildable Exact projection; Exact search filters current,
published, public, media-ready, source-current rows and accepts explicit exclusions. Hybrid Search
is the only current request path that creates a query vector. Recommendation must not use that query
embedding path because session interest can be composed from existing video vectors.

The logical Recommendation context is bounded to one current video and twenty recent video IDs, but
those client-supplied IDs are not sufficient evidence of completion, preference, or dislike. Existing
PostgreSQL facts already provide server-recorded exposures, playback behavior, LIKE/FAVORITE state,
and accepted recommendation feedback. The new path must use context only to select a bounded scope,
then derive semantic weights from those trusted facts.

This is a low-data development-stage capability. It must produce a useful demonstration and an
evaluation target without requiring long-term user history, cross-user behavior, Backfill, ANN, or a
trained ranking model.

## Goals / Non-Goals

**Goals:**

- Build one bounded, deterministic active-contract session interest vector from current/recent
  Recommendation context and trusted server facts.
- Introduce an optional `semantic_session` Provider backed by existing Exact multimodal retrieval and
  an independent `semantic_similarity` score component.
- Make sparse or contradictory sessions safe through explicit confidence, bounded output, existing
  quota underfill reallocation, and healthy fallback.
- Preserve exact model-contract isolation, request deadlines, visibility checks, feedback/exposure
  suppression, Snapshot stability, and privacy-bounded evidence.
- Keep existing active policies and all default configuration behavior unchanged.
- Produce deterministic tests and Golden Set fixtures that can later feed public-dataset evaluation
  and Semantic Shadow work.

**Non-Goals:**

- No long-term multimodal user profile, Recent/Long-term/Negative persisted vector, event ledger, or
  rebuild workflow.
- No external model call, text generation, query embedding, training, fine-tuning, LTR, Bandit, or
  collaborative filtering on the recommendation request path.
- No historical video Backfill and no requirement for every public video to have a multimodal vector.
- No pgvector HNSW or other ANN index; Exact remains the quality and capacity baseline.
- No public API field change, new user-controlled semantic weight, production Shadow, or rollout of a
  new active Recommendation policy.
- No removal of the existing hash-based profile, `session_continuation`, or `session_similarity`
  baseline.

## Decisions

### 1. Add a separate semantic path instead of changing the hash path

Register `semantic_session` as a new Recall Provider and `semantic_similarity` as a new ranking
feature. Existing `session_continuation` and `session_similarity` remain byte-for-byte compatible and
continue to act as a cheap baseline/fallback.

This keeps offline comparison honest: Hash Session and Multimodal Session can be measured
independently, can coexist in one policy, and can be disabled separately. Reusing the existing names
would silently change the meaning of stored policies, request logs, metrics, and score components.

### 2. Context selects a bounded scope; PostgreSQL facts establish trust

The builder accepts at most the normalized `current_video_id` and `recent_video_ids` already present
in `RecommendationContext`. A repository performs bounded batch reads for those IDs and the
authenticated user. A seed is eligible only when recent server-issued delivery/exposure or accepted
behavior evidence confirms that the user encountered it within the registered session lookback.

Within that scope, the repository returns closed signal facts rather than raw rows:

- `current`: current readable video with recent server evidence;
- `complete`: reliable completion;
- `sustained`: progress/watch ratio above the registered threshold;
- `like` and `favorite`: current active interaction states;
- `early_skip`: reliable terminal skip below the registered ratio;
- `not_interested`: accepted explicit video feedback;
- `already_seen`: hard exclusion only;
- `reduce_author`: remains existing author suppression and does not become a content vector signal.

Duplicate progress events and retries collapse by existing event identity/order semantics. For one
video and one signal kind, only the current canonical fact contributes. Explicit `not_interested`
removes implicit current/recent positive contribution for the same video. Completion suppresses an
older early-skip classification. Client order or duplicate IDs cannot create extra signal weight.

Alternative considered: trust the submitted recent ID list and average every vector, as the current
hash provider does. Rejected because arbitrary IDs could steer the semantic profile and one accidental
card would have the same weight as a completion or favorite.

### 3. Use a closed `session-semantic-v1` policy registry

Recommendation policy gains an optional complete session-semantic block containing:

- registered builder version (`session-semantic-v1`);
- immutable active multimodal contract key;
- bounded lookback and seed limit;
- minimum positive evidence and confidence threshold;
- a closed confidence-to-output rule.

Signal weights, sustained/early-skip thresholds, decay function, negative cap, confidence formula,
and normalization epsilon belong to the code-registered builder definition selected by the version.
The API does not accept arbitrary signal names or formulas. This matches existing typed policy and
governance registries while keeping the first version reproducible.

Existing policy JSON remains valid when the block is absent. A policy cannot select
`semantic_session` or assign non-zero `semantic_similarity` without the complete block, matching
Provider budget/deadline/quota validation. The contract key must equal the configured runtime
contract before a semantic policy can execute.

Alternative considered: put every signal weight directly in policy JSON. Rejected for the first
version because it creates a large validation surface and makes Golden Set baselines harder to name
and reproduce. A later change can expose selected weights after evidence justifies tuning them.

### 4. Compose positive and negative directions without creating a long-term profile

The builder loads only current active-contract vectors for eligible seeds. It computes bounded
time-decayed positive and negative weighted sums, requires positive evidence, caps total negative
mass relative to positive mass, subtracts the capped negative direction, and L2-normalizes the final
vector. `already_seen` does not imply dislike and therefore only enters exclusions. Missing,
non-finite, dimension-mismatched, stale-source, or other-contract vectors are skipped and counted.

`session-semantic-v1` confidence is a deterministic value in `[0,1]` derived from four bounded terms:

1. compatible-vector coverage over eligible signal weight;
2. positive evidence strength up to the registered saturation point;
3. directional coherence of the positive weighted sum;
4. weighted freshness after decay.

If positive evidence is absent, the combined norm is below epsilon, or confidence is below the
registered threshold, the builder returns a typed healthy-unavailable result rather than a zero or
fabricated vector. It returns no user identity or raw vector in logs.

Alternative considered: persist the session vector. Rejected because Recommendation Snapshot already
persists the resulting order, the logical session is short-lived, and durable storage would start the
long-term profile/event-ledger problem that this change explicitly defers.

### 5. Exact recall is internal and performs no provider call

`SemanticSessionProvider` receives a narrow builder and an Exact semantic index:

```text
RecommendationContext + trusted facts
        ↓
session-semantic-v1 builder
        ↓
active-contract unit vector + confidence + exclusions
        ↓
ExactMultimodalSearch
        ↓
semantic_session candidates
```

The Provider excludes every seed video plus explicit video suppressions before Exact retrieval,
rejects non-finite/non-positive similarities, and annotates candidates with the
`semantic_session` reason. Its source score is `exact_cosine * confidence`, bounded to `[0,1]`, and
becomes the `semantic_similarity` rank component. Exact retrieval remains responsible for active
contract, source currency, readability, publication, and deterministic tie ordering; the existing
Recommendation visibility pass still revalidates the merged superset.

The Provider never uses `EmbedPublicQuery`, the HTTP multimodal Provider, Redis query cache, or the
materialized long-term profile. External provider availability therefore cannot affect request-time
session recall once video vectors exist.

### 6. Confidence bounds achieved representation without changing quota-merge semantics

The configured `semantic_session` budget and quota reservation remain maximum policy bounds. The
Provider returns at most `floor(budget * confidenceScale)` candidates after meeting the minimum
confidence gate, with a deterministic minimum of one for an eligible session. Confidence bands and
their scales are closed by `session-semantic-v1`.

When confidence is low or vector coverage is sparse, the Provider intentionally returns fewer than
its configured reservation. Existing readability-aware quota merge records underfill and releases
unused capacity to deterministic common fill. No dynamic mutation of the stored quota contract is
needed, and other Providers never lose their own reservations.

The Ranker uses the confidence-scaled semantic source score only for candidates represented by this
Provider. Other candidates retain zero `semantic_similarity`; Fresh, Hot, Following, hash similarity,
negative feedback, exposure suppression, and diversity still compete normally.

Alternative considered: rewrite quota merge around per-request reservation maps. Rejected because
existing underfill already provides the desired capacity release with less policy and replay
complexity.

### 7. Keep activation independently dormant and fail closed at composition

Add `multimodal.session_recommendation_enabled`, defaulting to `false` in every checked-in config.
When false, Router does not register the Provider and existing policies remain unaffected. When true,
configuration requires multimodal enabled, a complete active contract, Exact retrieval, and valid
session-semantic bounds; it does not require query embedding or an available upstream provider.

Router creates/reuses the PostgreSQL multimodal repository before Recommendation assembly and wires
the Provider only after dependency validation. Startup fails if the feature flag requests a partial
runtime. At request time, missing signals/vectors are healthy empty results; database errors or Exact
deadline failures mark only `semantic_session` degraded and leave healthy Providers active.

No checked-in bootstrap policy selects the Provider. Enabling the runtime flag alone cannot alter
ranking; a later explicit policy must also select `semantic_session`, its deadline/budget/quota, the
registered semantic block, and a positive `semantic_similarity` weight.

### 8. Preserve Snapshot ordering and emit privacy-bounded evidence

Session interest is built only on the first Recommendation page. The resulting ranked candidates and
score components enter the existing Snapshot, so later pages never recompute a different session
vector. Snapshot fallback retains the current degraded semantics.

Sampled request logs gain an optional bounded semantic summary:

- builder version and contract key;
- closed result/degradation code;
- confidence and confidence band;
- eligible, positive, negative, compatible, and excluded counts;
- bounded canonical input digest, but no raw vector or raw event payload.

Candidate logs already retain Provider reasons and registered score components, so
`semantic_session` and `semantic_similarity` become replay-visible. Metrics use fixed result,
confidence-band, and closed-reason labels plus histograms for confidence, coverage, signal counts,
candidate counts, and duration. They never label user/request/session/video IDs, contract keys,
vectors, query text, or raw errors.

### 9. Use deterministic Golden Set fixtures before Shadow

Add versioned fixture cases covering interest direction and suppression rather than claiming online
lift. Each case contains synthetic unit vectors, closed server signal facts, an expected confidence
band, expected included/excluded candidates, and ordering assertions. Required baselines include:

- current only;
- completion plus favorite reinforcement;
- early skip opposing a positive direction;
- `not_interested` overriding implicit positive context;
- `already_seen` exclusion without negative direction;
- mixed compatible/missing vectors;
- contradictory signals below confidence threshold;
- active-contract mismatch;
- semantic timeout with hash/non-vector fallback.

These fixtures validate orchestration and deterministic direction. MicroLens/KuaiRec metrics and
human relevance labels remain the next independent evaluation change.

## Risks / Trade-offs

- [Sparse multimodal coverage produces frequent empty results] → Treat absence as healthy underfill,
  preserve all existing Providers, record coverage/confidence, and do not enable a policy before
  Golden Set coverage is acceptable.
- [One accidental current card dominates interest] → Require server evidence, cap each signal kind,
  require positive evidence strength, use coherence/confidence, and exclude explicit negative facts.
- [Negative-vector subtraction can create unstable directions] → Require a positive base, cap negative
  mass, reject near-zero norms, and cover contradictory cases with deterministic fixtures.
- [Client-provided IDs attempt to steer recommendation] → Use context only as a bounded selector and
  require server-issued encounter/behavior evidence before any vector contributes.
- [Exact search becomes slow as coverage grows] → Reuse policy deadlines, global Recall slots, context
  cancellation, fixed budgets, and metrics; propose HNSW only after measured Exact capacity gates.
- [Contract rotation makes a session non-reproducible] → Pin the immutable contract key in policy and
  evidence, compare only exact identities, and return healthy unavailable on mismatch.
- [Request logs expose a behavioral fingerprint] → Store bounded counts and a digest only under the
  existing sampling/retention controls; never store raw vectors or event bodies.
- [Adding another Provider increases policy complexity] → Keep the first builder code-registered,
  require a complete optional policy block, leave bootstrap policies untouched, and document one
  dormant example for development evaluation only.

## Migration Plan

1. Add registries, configuration fields, domain validation, builder/provider abstractions, and tests
   while all checked-in feature flags remain false and active policies remain unchanged.
2. Add PostgreSQL batch readers over existing tables and optional request-log JSON fields; no schema
   migration or data Backfill is required.
3. Wire the Provider behind complete runtime validation, add metrics/docs, and verify the disabled
   default through complete tests and Compose validation.
4. In development only, enable the runtime and create a separate non-bootstrap policy version that
   selects `semantic_session`; run deterministic and PostgreSQL Golden Set acceptance.
5. Roll back by deactivating that policy or disabling the runtime flag. Existing policies, Snapshots,
   hash vectors, and non-semantic Providers continue without data repair.

## Open Questions

No implementation-blocking question remains. Exact signal weights, confidence thresholds, and band
scales are intentionally fixed under `session-semantic-v1`; later tuning requires Golden Set evidence
and a new registered version rather than editing the meaning of v1.
