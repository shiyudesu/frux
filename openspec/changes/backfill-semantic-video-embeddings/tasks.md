## 1. Dependency and Contract Gate

- [ ] 1.1 Confirm both predecessor changes are implemented and expose the fixed full identity, `semantic-text-v1`, privacy, cache, validated adapter, cost/quota controls, and conditional repository.
- [ ] 1.2 Add startup validation for environment, provider/model/revision/dimension/canonicalizer, pricing revision, and exact-identity refresh confirmation.

## 2. Dry-Run Estimate and Stable Selection

- [ ] 2.1 Implement the eligible frozen horizon and stable `(published_at, id)` pages for missing, stale, and force modes without selecting hash/other identities for replacement.
- [ ] 2.2 Implement dry-run classification, unique text-hash/cache analysis, expected provider items/API calls, billable units, estimated cost, and bounded row/runtime/QPS/cost reporting with no provider call or mutation.
- [ ] 2.3 Generate and validate the deterministic estimate digest; require matching environment/identity/canonicalizer/pricing/mode/horizon/bounds before execution.
- [ ] 2.4 Add tests for equal timestamps, catalog changes, cache estimates, batching/cost math, stale/force confirmation, and estimate mismatch.

## 3. Advisory Lock and Checkpoint

- [ ] 3.1 Implement the environment-and-full-model PostgreSQL advisory lock held for the complete run while leaving live jobs independent.
- [ ] 3.2 Implement the versioned checkpoint bound to environment, full identity, canonicalizer, pricing, estimate, mode, horizon, run ID, completed tuple, and row/cost counters.
- [ ] 3.3 Implement mode-0600 sibling write, file/directory flush, atomic replacement after complete page prefixes, corruption detection, and cancellation-safe release.
- [ ] 3.4 Add lock conflict/release and checkpoint compatibility/corruption/partial-page/restart tests.

## 4. Live Priority and Resource Gates

- [ ] 4.1 Set defaults to page 128, batch at most 16, concurrency 1, maximum concurrency 2, and backfill QPS at most 20% of provider QPS.
- [ ] 4.2 Add the shared PostgreSQL capacity coordinator that reserves provider/database tokens for real-time jobs and pauses backfill when live work exists or oldest live backlog exceeds five minutes.
- [ ] 4.3 Add provider QPS/`Retry-After`, approved budget, DB p95, WAL rate, replica lag, and replica byte-backlog sampling before reads/calls/writes.
- [ ] 4.4 Implement pause/resume hysteresis (five healthy 10-second samples and 30-second cooldown), budget-reached clean stop, and metrics/tests for every gate.

## 5. Quarantine and Conditional Persistence

- [ ] 5.1 Add privacy-safe deterministic quarantine keyed by video/full identity/source version with bounded reasons and no text, credential, URL, vector, or provider response.
- [ ] 5.2 Skip unchanged quarantine entries, reevaluate changed sources, and add an authenticated operator clear/requeue command.
- [ ] 5.3 Reuse the shared adapter/cache and lock/re-read eligibility/text hash before exact-identity missing/stale/force compare-and-set persistence.
- [ ] 5.4 Add tests for invalid canonical text, source repair, provider failure not becoming quarantine, source/visibility/media changes, live-write races, identical no-op, and other-model isolation.

## 6. Cancellation Resume and Hash Invariance

- [ ] 6.1 Implement signal/runtime/operator cancellation that stops scheduling, cancels calls, waits for goroutines, and leaves the last complete-page checkpoint authoritative.
- [ ] 6.2 Add resumable stop reasons for horizon, max rows, max runtime, budget, pressure, and cancellation with at-most-one-page replay.
- [ ] 6.3 Capture pre/post `hash-ngram-v1` count and deterministic aggregate digest including vector content/timestamps; add zero hash insert/update/delete metrics.
- [ ] 6.4 Add interruption/restart matrices for missing/stale/force modes and tests proving byte/timestamp-zero hash changes.

## 7. Coverage Acceptance and Operations

- [ ] 7.1 Add coverage accounting for exact semantic facts and deterministic quarantines over the frozen/currently eligible horizon.
- [ ] 7.2 Implement acceptance requiring at least 99.5% exact-contract coverage, `covered + quarantined = 100%`, cost within approval, identical hash digest/count, and no unresolved lock/checkpoint/resource incident.
- [ ] 7.3 Add bounded metrics and exactly one final summary for estimates/actuals, calls/units/cost, cache, pages, outcomes, quarantine, checkpoints, locks, pauses, resource samples, live yielding, and coverage with redaction tests.
- [ ] 7.4 Update the operator runbook for mandatory dry-run, approval digest, bounded rollout, advisory lock, checkpoint backup, priority/pressure pauses, quarantine repair, cancellation/resume, hash verification, coverage acceptance, and rollback.

## 8. Command Container and Validation

- [ ] 8.1 Add the one-shot Go command and manual container/Compose entrypoint using PostgreSQL and provider secret/config only, with no Kafka, Redis, public port, or migrations.
- [ ] 8.2 Run targeted unit/PostgreSQL/provider-contract/cancellation/resource-gate/coverage tests and validate the manual entrypoint without real committed credentials.
- [ ] 8.3 Build all Go entrypoints, run the complete Go suite, and confirm no live Kafka, hash mutation, local model, retrieval, profile, ranking/policy, Web, or training changes.
- [ ] 8.4 Run `openspec validate --all --strict` and inspect the final artifacts for consistency with both predecessor contracts.
