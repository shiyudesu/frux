## 1. Policy Bounds

- [x] 1.1 Add a single domain constant for the absolute policy pre-rank pool maximum of 500 and
  reuse the existing request-log/Snapshot ceiling where ownership allows without circular imports.
- [x] 1.2 Extend policy normalization to sum every selected valid Provider budget and reject zero,
  overflow, or totals above 500 until quota merge is supported.
- [x] 1.3 Add domain tests for exact-bound acceptance, over-bound rejection, unknown/disabled
  Provider handling, defensive map copies, and unchanged existing field validation.
- [x] 1.4 Add exact serialization/seed regressions proving bootstrap `recommend/v1` and
  `recommend/v2` remain byte-for-byte unchanged and each selected budget sum remains 500.

## 2. Complete Policy Recall Pool

- [x] 2.1 Separate policy-driven pre-rank pool sizing from the response-derived
  `candidatePoolLimit` used by legacy repository/no-policy fallback paths.
- [x] 2.2 Remove global `published_at`/video-ID sorting and response-size truncation from the
  policy-driven merged recall set.
- [x] 2.3 Preserve duplicate merge, canonical Recall Reasons/Source Scores, Provider-local budgets,
  Provider degradation, healthy-provider counting, and visibility revalidation for the complete
  bounded set.
- [x] 2.4 Ensure the policy Ranker receives the entire visibility-filtered unique set independent of
  Provider completion order and Go map iteration order.
- [x] 2.5 Preserve the direct no-policy caller's deterministic total limit and recent-exposure
  compatibility behavior.

## 3. Ranking and Resource Regressions

- [x] 3.1 Add service tests proving a 10-card request with five 100-budget Providers sends all 500
  unique readable candidates to feature loading and ranking before response slicing.
- [x] 3.2 Add a regression where an older candidate is outside the former 80-item recency prefix but
  ranks first after content/session or another existing feature is evaluated.
- [x] 3.3 Add shuffled Provider completion and candidate-map insertion tests proving the final ranked
  output is identical for equivalent candidate facts.
- [x] 3.4 Add duplicate-heavy, visibility-filtered, partially failed, capacity-degraded, all-failed,
  and empty-provider regressions proving bounds and existing error semantics.
- [x] 3.5 Prove feature batch loading, suppression, diversity, Snapshot creation, full-pool request
  logging, served evidence, cursor fallback, and response assembly remain within 500 candidates.
- [x] 3.6 Extend the bounded-pool benchmark to exercise the full 500-candidate policy path and record
  feature/ranking allocations and latency without setting an unsupported production SLO.

## 4. Metrics and Documentation

- [x] 4.1 Add fixed-label observations for Provider-returned, unique-merged,
  visibility-filtered, and Ranker-input candidate counts plus rejected over-cap policies.
- [x] 4.2 Add metrics tests proving candidate/user/request IDs, Provider raw errors, and high-cardinality
  pool contents do not become labels or normal logs.
- [x] 4.3 Update `docs/modules/recommendation.md`, `docs/optimization.md`, and the recommendation
  roadmap to describe complete bounded pre-rank scoring and the separate quota-merge follow-up.

## 5. Verification

- [ ] 5.1 Run targeted recommendation domain, recall, ranker, snapshot, request-log, evidence,
  policy-persistence, metrics, and router tests.
- [ ] 5.2 Run `cd apps/api && go test ./...` and compile `./cmd/feed` and `./cmd/worker`.
- [ ] 5.3 Run `openspec validate --all --strict` and confirm the change adds no Provider Reservation,
  semantic/multimodal dependency, new feature weight, persistence migration, policy rollout, or
  public API change.
