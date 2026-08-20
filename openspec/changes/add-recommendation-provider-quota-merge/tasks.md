## 1. Policy Contract

- [x] 1.1 Add omission-compatible `pre_rank_pool_limit`, ordered Provider list, and Provider
  reservation fields plus defensive clone/JSON persistence support to recommendation policy types.
- [x] 1.2 Normalize Provider tokens and validate complete-field presence, exact selected-Provider
  coverage, no duplicates, pool limit 50–500, reservation 0..budget, and reservation sum within the
  pool.
- [x] 1.3 Preserve prerequisite complete-pool validation when every quota field is omitted and allow
  selected budget sum above 500 only for a complete valid quota configuration.
- [x] 1.4 Add domain table tests for valid/invalid orders, partial fields, unknown/missing/duplicate
  Providers, negative/over-budget reservations, over-sum reservations, bounds, and defensive copies.
- [x] 1.5 Add exact seed/serialization/persistence regressions proving `recommend/v1` and
  `recommend/v2` remain byte-for-byte free of quota fields and continue using complete-pool mode.

## 2. Provider-local Normalization

- [ ] 2.1 Implement a pure Provider-local normalization helper that filters invalid/nil candidates,
  deduplicates IDs, canonicalizes that Provider's evidence, stably orders by finite source score/
  publication/video ID, and applies the Provider budget.
- [ ] 2.2 Preserve all cross-Provider reasons/source scores in a separate global duplicate merge
  without comparing Provider score scales.
- [ ] 2.3 Add tests for raw slice permutation, duplicate IDs, invalid scores, missing evidence,
  stable ties, budget truncation, and defensive candidate cloning.

## 3. Readable Superset Preparation

- [ ] 3.1 Build one bounded unique superset from every healthy normalized Provider and perform one
  visibility batch before quota accounting.
- [ ] 3.2 Filter each Provider-local sequence to readable IDs while updating current author,
  publication, and hot facts on the global merged candidate.
- [ ] 3.3 Preserve existing visibility failure, all-providers-failed, timeout/error/capacity
  degradation, and no-policy compatibility behavior.
- [ ] 3.4 Add tests for leading unreadable candidates, mixed lifecycle states, large overlap,
  visibility lookup failure, Provider underfill after filtering, and bounded superset size.

## 4. Reservation and Fill Mixer

- [ ] 4.1 Implement a pure reservation phase over normalized readable Provider sequences in explicit
  policy order, tracking one global slot per video and represented counts from merged reasons.
- [ ] 4.2 Implement Provider underfill/exhaustion handling so all usable candidates survive and
  unused capacity returns to the common fill phase.
- [ ] 4.3 Implement deterministic round-robin fill from retained Provider cursors until pool limit or
  global exhaustion, merging duplicates without consuming another slot.
- [ ] 4.4 Integrate quota merge only when a policy supplies the complete quota contract; retain the
  prerequisite complete-pool path for policies that omit it.
- [ ] 4.5 Add exhaustive table/property-style tests for completion-order independence, policy-order
  priority, overlaps representing multiple Providers, duplicate-only turns, underfill, one-provider
  remainder, exact pool fill, empty providers, and pool bounds.
- [ ] 4.6 Add service regressions proving every mixed unique candidate reaches unified feature
  scoring and existing ranking/diversity/Snapshot/cursor/evidence behavior remains unchanged.

## 5. Observability and Diagnostics

- [ ] 5.1 Add fixed-label metrics for Provider returned, local unique, readable, reserved,
  fill-selected, final represented, overlap, exhausted, and underfill counts plus merge duration and
  selected pool size.
- [ ] 5.2 Add sampled bounded diagnostic reason/phase summaries to existing recommendation evidence
  or logs only where current schemas allow, without creating a raw candidate-pool persistence table.
- [ ] 5.3 Add tests proving video/user/request/session IDs, candidate lists, raw source scores, map
  payloads, and Provider raw errors never become labels or normal log payloads.
- [ ] 5.4 Update recommendation module, optimization, monitoring, and roadmap documentation with
  policy fields, deterministic algorithm, compatibility behavior, underfill, and rollback.

## 6. Verification

- [ ] 6.1 Add a disabled development policy fixture with selected budgets above its pool limit and
  verify normalization/persistence/mixing without selecting it for production requests.
- [ ] 6.2 Run targeted policy domain/persistence, recall, ranker, snapshot, request-log, evidence,
  metrics, composition, and concurrency tests.
- [ ] 6.3 Run `cd apps/api && go test ./...` and compile `./cmd/feed` and `./cmd/worker`.
- [ ] 6.4 Run `openspec validate --all --strict` and confirm the change adds no Provider, semantic/
  multimodal feature, public API, persistence table, learned quota, bootstrap policy edit, or active
  rollout.
