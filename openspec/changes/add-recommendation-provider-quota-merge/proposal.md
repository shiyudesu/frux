## Why

After `fix-recommendation-candidate-pool-truncation`, current policies can rank every unique
candidate because their selected Provider budgets total the absolute 500-candidate pool bound.
Future Providers or wider recall budgets will legitimately exceed that bound, but Frux has no
explicit contract for deciding which Provider candidates survive. Runtime truncation by recency,
completion order, map order, or incomparable source scores would reintroduce hidden bias.

## What Changes

- Add a versioned policy contract for `pre_rank_pool_limit`, ordered selected Providers, and a
  per-Provider `reservation` bounded by that Provider's existing recall budget.
- Allow selected Provider budget sums to exceed the pre-rank pool only when the policy supplies a
  complete valid quota-merge configuration. Policies without quota fields retain the complete-pool
  requirement from the prerequisite change and remain invalid above 500.
- Normalize every Provider result to its existing bounded stable local order, merge duplicate video
  IDs while retaining all reasons/source scores, satisfy configured Provider reservations through
  deterministic ordered rounds, and fill remaining slots through deterministic round-robin.
- Count each globally selected video once. A duplicate can retain and represent multiple Provider
  reasons; providers that return insufficient usable candidates simply underfill, and unused
  capacity returns to the common fill phase.
- Preserve visibility revalidation before final pool admission or immediately after a bounded
  provisional mix, then deterministically refill from remaining Provider-local candidates so stale
  or unreadable rows do not silently waste the pool.
- Add fixed-label metrics and diagnostic evidence for Provider returned/unique/reserved/fill/
  selected counts, overlap, underfill, and survival without persisting candidate IDs or adding
  high-cardinality labels.
- Keep bootstrap `recommend/v1` and `recommend/v2` byte-for-byte unchanged. Their omitted quota
  fields continue using the complete 500-budget pool and do not activate reservation logic.
- Add no new RecallProvider, semantic/multimodal dependency, ranking feature, public API,
  persistence schema, user-visible policy rollout, or change to final scoring/diversity/Snapshot.

## Capabilities

### New Capabilities

- `recommendation-provider-quota-merge`: Defines versioned Provider order, pool limit,
  reservations, duplicate handling, underfill, deterministic round-robin fill, readability refill,
  observability, and compatibility with policies that omit quota configuration.

### Modified Capabilities

- `contextual-recommendation`: Allows a bounded policy-selected budget sum above the global
  pre-rank pool only through an explicit deterministic quota-merge contract and requires every
  selected candidate to reach unified feature scoring after that mix.

## Impact

- Depends on `fix-recommendation-candidate-pool-truncation` and affects recommendation policy
  domain validation/serialization, recall execution, candidate merge diagnostics, metrics,
  request-service tests, policy persistence tests, and recommendation documentation.
- Establishes the Provider mixing seam required by later Session Semantic Recall without naming or
  activating a semantic Provider in this change.
- Requires no data migration because quota fields are optional and omitted by existing stored and
  bootstrap policies.
