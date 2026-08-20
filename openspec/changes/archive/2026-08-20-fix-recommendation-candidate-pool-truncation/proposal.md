## Why

Policy-driven recommendation currently merges Provider candidates, globally sorts them by
`published_at DESC`, and truncates the pre-rank pool to a response-derived `limit × 8` bound before
feature scoring. A normal 10-card request therefore ranks only 80 of the up to 500 candidates
already allowed by the five selected Provider budgets, so newer content can erase stronger
similarity, relationship, session, or future semantic candidates before their ranking features are
evaluated.

## What Changes

- Remove the global `published_at` pre-rank sort and response-limit-derived truncation from the
  normal policy-driven multi-Provider recall path.
- Bound the complete pre-rank pool by the validated sum of selected Provider budgets and the existing
  absolute 500-candidate request-log/snapshot ceiling. Current bootstrap policies already select
  five 100-candidate Providers, so their complete unique merged output remains bounded by 500.
- Extend policy validation so, until a later quota-merge capability is active, the sum of selected
  Provider budgets cannot exceed the absolute pre-rank pool ceiling. Invalid policies never become
  active.
- Preserve Provider-local budget/order, duplicate reason/source-score merge, visibility
  revalidation, exposure/suppression behavior, feature loading, stable final ranking, diversity,
  Snapshot, cursor, evidence, and degraded-provider semantics.
- Keep the direct legacy/no-policy recall path bounded and deterministic without changing its
  compatibility contract.
- Add metrics and tests proving the Ranker receives every unique candidate allowed by the selected
  policy budgets and that an older high-value candidate is not discarded merely because the
  response page is small.
- Keep `recommend/v1` and `recommend/v2` byte-for-byte unchanged. Add no reservations, provider
  quotas beyond existing budgets, semantic Provider, multimodal dependency, new ranking feature, or
  recommendation rollout.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `contextual-recommendation`: Requires the normal policy-driven pre-rank pool to preserve the
  complete bounded unique merge selected by Provider budgets and forbids recency-only truncation
  before feature scoring.

## Impact

- Affects recommendation policy validation, multi-Provider recall execution, pre-rank pool metrics,
  request-service tests, ranking regressions, and recommendation documentation.
- Does not change Provider implementations, public APIs, persistence schemas, Web behavior,
  bootstrap policies, final ranking formula, Snapshot capacity, or logging/evidence payload limits.
- Establishes the prerequisite for a later `add-recommendation-provider-quota-merge` change, which
  may safely allow selected Provider budgets to exceed the global pool through explicit
  reservations and deterministic mixing.
