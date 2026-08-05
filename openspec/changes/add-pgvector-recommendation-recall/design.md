## Context

Frux already executes bounded application-level `RecallProvider` implementations through policy budgets, per-provider deadlines, a shared process-wide concurrency limit, duplicate merge, visibility revalidation, unchanged ranking, and degraded-provider recording. Bootstrap `recommend/v1` and `recommend/v2` contain only the existing providers.

This change adds only the active `semantic_ann` provider and policy opt-in behavior. It assumes two separately delivered prerequisites:

- `enable-pgvector-recommendation-index` owns pgvector deployment, extension/schema/projection/index lifecycle, reconciliation/backfill, query validation, exclusions inside the query, and query-plan/performance acceptance. It exposes a validated bounded semantic ANN query interface without pgvector types.
- `project-semantic-user-interest` owns semantic profile persistence/projection and exposes compatible recent and long-term profile reads without requiring this provider to understand storage.

The provider must not make either prerequisite a Feed correctness dependency. Disabled composition and policies that omit `semantic_ann` preserve existing behavior.

## Goals / Non-Goals

**Goals:**

- Add the `semantic_ann` provider token and application-level interfaces.
- Select recent semantic interest first and long-term interest only as fallback.
- Bound execution by policy budget, executor deadline, shared concurrency, and bounded session exclusions.
- Annotate candidates with finite cosine recall reason/source scores.
- Preserve normal merge, visibility filtering, ranking, snapshots, attribution, and degradation.
- Allow only explicit future policies to activate the provider while keeping bootstrap v1/v2 unchanged.
- Compose behind disabled-by-default enablement and expose bounded active-provider metrics.
- Document policy-based rollout and rollback.

**Non-Goals:**

- Installing or configuring pgvector or PostgreSQL.
- Adding migrations, projection/index schema, reconciliation, backfill, rebuild, purge, or model lifecycle operations.
- Defining ANN SQL, HNSW parameters, query plans, retrieval-quality thresholds, or performance acceptance.
- Adding shadow execution, sampling, overlap metrics, or shadow acceptance gates; those belong to future `shadow-semantic-ann-recall`.
- Changing semantic video embedding or semantic user-interest normative requirements.
- Adding ranking features or weights, changing final scoring, training models, or changing bootstrap policies.

## Decisions

### 1. Depend on narrow prerequisite interfaces

The recommendation application owns two interfaces:

- `SemanticANNProfileSource`, which loads one compatible profile containing recent and long-term vectors for a user.
- `SemanticANNIndex`, which accepts a normalized query vector, budget, and bounded excluded video IDs and returns readable candidate facts with cosine similarity.

The interfaces use application/domain values only. They do not expose SQL, pgvector values, projection rows, index settings, or profile persistence models. The index prerequisite remains responsible for validating its descriptor, bounding the physical query, honoring cancellation, applying exclusions, and returning no more than the requested budget.

Composition supplies both implementations for the same supported semantic contract. When semantic ANN is enabled but either prerequisite is unavailable or incompatible, API startup fails with a bounded configuration/prerequisite error rather than silently registering a partial provider. Missing data for an individual user or video remains a runtime empty result.

Alternative considered: let the provider query PostgreSQL/profile repositories directly. Rejected because it would recouple this change to the infrastructure scope deliberately moved into the prerequisites.

### 2. Keep profile selection inside the active provider

For each request, the provider loads the user's compatible semantic profile and:

1. uses a finite recent vector whose norm is at least `1e-6`;
2. otherwise uses a finite long-term vector whose norm is at least `1e-6`;
3. otherwise returns a successful empty candidate list.

The provider normalizes a defensive request-local copy before querying the index. It does not mutate the profile, combine recent and long-term vectors, use the negative vector, fall back to the hash profile internally, or call an embedding service. A missing profile or two empty positive vectors is healthy absence. An error or incompatible payload from the profile source is provider degradation.

Alternative considered: blend recent and long-term interests. Rejected because that would introduce a new preference formula outside the accepted profile contract.

### 3. Use existing provider execution bounds

`semantic_ann` implements the existing `RecallProvider` contract. Its budget comes from the selected policy and is restricted to `1..100`. Its context is already capped by the selected policy deadline, restricted to `25..500ms`, and admitted through the existing shared provider-concurrency limit.

The provider copies at most 20 current/recent session video IDs from the bounded recommendation context and passes them as exclusions to `SemanticANNIndex`. It performs one profile read and at most one ANN query. It never retries, widens the budget, scans candidates itself, or starts detached work after the provider context ends.

Alternative considered: add a semantic-specific executor or concurrency pool. Rejected because the existing executor already supplies the required deadline, admission, cancellation, and degradation semantics.

### 4. Treat cosine similarity as recall metadata only

Each returned neighbor becomes one candidate with:

- exactly one `RecallReason{Provider: "semantic_ann", Score: cosine}`;
- `SourceScores["semantic_ann"] = cosine`.

The score must be finite and is defensively clamped to `[0,1]`; non-positive or invalid scores are omitted. No vector, distance, profile source, model metadata, or index metadata is attached to the candidate or exposed in responses.

Normal duplicate merge retains semantic and existing-provider reasons on one candidate. The unchanged ranker may consume only its existing feature set; this change adds no semantic feature or weight.

### 5. Make activation require both enablement and policy opt-in

Configuration adds `recommendation.semantic_ann.enabled`, default `false`.

- Disabled: do not construct or register the provider and do not require either prerequisite at API startup.
- Enabled: validate and compose both prerequisite interfaces, then register `semantic_ann`.
- Active: the selected policy must also contain both `recall_budgets.semantic_ann` and `provider_deadlines_ms.semantic_ann`.

Policy normalization recognizes `semantic_ann` only as a recall provider. Budget must be `1..100`; deadline must be `25..500ms`; both entries must be present together. There is no `semantic_ann` feature-weight key.

`InitialRecommendationPolicies` and `EnsureInitialPolicies` remain byte-for-byte free of `semantic_ann`. Provider registration alone therefore cannot change production ordering. A selected policy that references an unavailable provider follows the existing missing/failing-provider degradation path; rollout procedures prevent this state by enabling composition before policy activation.

### 6. Preserve merge and degradation semantics

Healthy semantic candidates enter the existing candidate pool, duplicate merge, final readability check, suppression, ranking, snapshot, evaluation, and attribution flows. Empty profile or empty neighbors is a healthy empty result.

Profile-source errors, ANN errors, deadline expiry, cancellation, or shared-capacity rejection produce no partial semantic candidates and use the existing bounded provider degradation reason. Healthy providers continue. Existing hash and non-vector fallbacks are unchanged.

### 7. Observe only active provider behavior

Metrics cover active provider attempts, duration, result, candidate count, and selected profile source. Labels use fixed enums only:

- result: `success`, `empty`, `no_profile`, `timeout`, `capacity`, `invalid_profile`, `index_error`;
- profile source: `recent`, `long_term`, `none`.

Metrics and normal logs exclude user/video/request IDs, vectors, candidate lists, model strings, SQL/index details, and raw errors. Shadow, overlap, projection, reconciliation, backfill, HNSW, and query-plan metrics belong to other changes.

### 8. Roll out and roll back through policy selection

Deployment first enables and verifies provider composition while the selected policy remains v1/v2 or another policy without `semantic_ann`. Operators then create a new validated policy version with explicit semantic budget/deadline and use existing rollout percentage controls.

Rollback selects a policy without `semantic_ann`; new requests stop executing it without a redeploy. Configuration may be disabled after no selected policy references the provider. Rollback does not alter prerequisite data or index state.

## Risks / Trade-offs

- [The prerequisite interfaces may evolve independently] → Keep contracts narrow, require compatible composition, and cover adapters with contract tests.
- [A malformed profile or invalid score could reach ranking] → Validate selection inputs and defensively reject non-finite/non-positive output before candidate annotation.
- [Semantic ANN can consume shared provider capacity] → Reuse policy deadlines and the existing global admission bound; do not add retries or background work.
- [Enabling composition without policy activation can be mistaken for rollout] → Keep bootstrap policies unchanged and document that both enablement and explicit policy selection are required.
- [An active policy can outlive provider enablement] → Roll back policy first and retain existing degraded-provider behavior as a safety net.

## Migration Plan

1. Complete and validate `enable-pgvector-recommendation-index` and `project-semantic-user-interest`.
2. Add the application interfaces, provider token, provider, policy validation, metrics, and composition switch with semantic ANN disabled.
3. Verify bootstrap v1/v2 serialization and existing recommendation behavior are unchanged.
4. Enable provider composition in a prepared environment while the selected policy omits `semantic_ann`.
5. Create and gradually roll out a new policy version with explicit semantic budget/deadline.
6. Roll back by selecting a policy without `semantic_ann`; disable composition only after active references are removed.

## Open Questions

None. Infrastructure acceptance is owned by `enable-pgvector-recommendation-index`, and shadow evaluation is deferred to future `shadow-semantic-ann-recall`.
