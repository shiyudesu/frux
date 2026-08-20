## Context

`recommendFreshPage` derives a pre-rank limit from the response limit using an eight-times
multiplier, bounded to 50–500. `recallCandidates` executes every selected Provider under its own
policy budget/deadline, merges duplicate IDs and reasons, globally sorts the unique pool by
`published_at DESC, video_id DESC`, revalidates visibility, and truncates to that response-derived
limit before `rankCandidates` loads features and applies the policy.

For a normal 10-card request the Ranker sees at most 80 candidates even though bootstrap
`recommend/v1` and `recommend/v2` each select five Providers with budgets of 100, a total hard bound
of 500. The request log and Redis Snapshot already support 500 ranked candidates. Therefore the
current recency truncation is not required for the accepted bootstrap capacity and can remove
candidates before content/session similarity, hotness, author affinity, following, negative,
exposure, suppression, and diversity rules are evaluated.

## Goals / Non-Goals

**Goals:**

- Make the normal policy-driven Ranker receive the complete unique pool allowed by selected
  Provider budgets.
- Preserve the existing absolute 500-candidate bound and reject policies that exceed it until quota
  merge is available.
- Remove Provider completion, map iteration, global recency, video-ID, and response-page-size bias
  from pre-rank candidate survival.
- Preserve current Provider, feature, ranking, diversity, Snapshot, cursor, evidence, logging, and
  degradation contracts.

**Non-Goals:**

- Adding Provider Reservations, ordered quota mixing, or allowing budget sums above 500.
- Adding a semantic/multimodal Provider or ranking feature.
- Changing `recommend/v1`, `recommend/v2`, their serialized JSON, or rollout cohorts.
- Changing final ranking, diversity, suppression, response page size, Snapshot size, or public APIs.
- Changing the legacy no-policy recall compatibility path.

## Decisions

### 1. Derive the policy pre-rank bound from selected budgets, not response size

For policy-driven recall, compute the selected Provider budget sum after policy normalization. The
accepted value must be positive and at most the existing absolute pool maximum of 500. Each
Provider remains independently bounded by its own budget, so the merged unique pool cannot exceed
the sum.

The response `limit × 8` helper remains available only to legacy repository fallback/no-policy
paths where it is part of the existing compatibility and cost contract.

Alternative: simply increase the multiplier. Rejected because any response-derived value still
creates an unrelated survival bias and can become wrong as Provider count or budgets change.

### 2. Do not establish a global order before feature ranking

After duplicate merge, policy-driven candidates form an unordered bounded set. Visibility
revalidation updates current author, publication, and hot facts; unreadable rows are removed. The
Ranker then computes features for the whole set and establishes the first global order through the
existing stable `(rank_score, published_at, video_id)` comparison.

Provider-local ordering remains relevant only to each Provider enforcing its own budget. Duplicate
reason/source-score ordering stays canonical through the existing merge helper.

Alternative: sort by the best Provider source score before truncation. Rejected because source
scores are Provider-specific and not comparable until policy ranking; this would replace recency
bias with an implicit cross-Provider scale assumption.

### 3. Reject over-cap policies until quota merge exists

Policy normalization sums budgets for every selected valid Provider and rejects a total above 500.
This makes complete-pool preservation a configuration invariant rather than a best-effort runtime
property. The later quota-merge change will introduce a versioned pre-rank pool limit and
Reservations before relaxing this restriction.

Bootstrap v1/v2 each total exactly 500 and remain byte-for-byte unchanged. Missing or disabled
Providers are not silently reallocated in this change; healthy Providers return up to their own
budgets as today.

### 4. Keep feature and evidence resource bounds unchanged

`RankingFeatureSource` already loads facts for an already bounded pool, request logs accept 500
candidates, and Snapshot storage accepts 500. This change adds regressions and metrics but no larger
payload or storage ceiling.

Metrics record fixed-label pre-rank counts such as Provider-returned, unique-merged,
visibility-filtered, and Ranker-input counts. They do not label candidate IDs, users, requests, or
Provider raw errors.

### 5. Preserve degraded and legacy behavior

Provider timeout/error/capacity handling, all-providers-failed behavior, visibility failure,
legacy exposure filtering, repository fallback, and deterministic cold-start paths remain as
implemented. The complete-pool rule applies only when a versioned policy selected at least one
healthy Provider.

## Risks / Trade-offs

- [Ranking work increases from about 50–80 to as many as 500 candidates for small pages] → Keep the
  existing absolute bound, batch feature loads, add the existing bounded-pool benchmark at 500, and
  observe rank/feature latency before rollout.
- [Map iteration no longer has a deterministic pre-rank order] → Treat the pool as a set; require
  the Ranker to establish the only global order and add shuffle/order-independence tests.
- [A future Provider pushes selected budgets above 500] → Reject the policy until the separate
  quota-merge change defines explicit Reservations and mixing.
- [Legacy callers accidentally change behavior] → Branch explicitly on the presence of a selected
  policy and retain direct no-policy tests.
- [Visibility lookup becomes larger] → Keep one bounded batch of at most 500 IDs and measure it in
  focused tests/benchmarks.

## Migration Plan

1. Add the budget-sum validation and exact bootstrap serialization regressions.
2. Separate policy-driven pre-rank bounding from legacy response-derived pool sizing.
3. Remove the global recency sort/truncate from the policy path and add complete-pool metrics.
4. Prove feature loading, ranking, Snapshot, request logging, cursor, and response assembly remain
   bounded at 500.
5. Deploy without changing active policy rows or rollout percentages.

Rollback restores the prior policy recall bound and recency truncation. No migration, persisted
policy rewrite, Snapshot format change, or data rollback is required.

## Open Questions

None. Provider-specific Reservations and over-cap mixing belong to the accepted follow-up change,
not this correctness fix.
