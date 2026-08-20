## 1. Activation Gate and Offline Boundaries

- [ ] 1.1 Keep this change indefinitely deferred until a reviewed activation record preregisters the learning hypothesis, primary metric/MPE, sample-size calculation with alpha at most 0.05 and power at least 0.80, independent units, multiplicity, and numeric split/label/exposure/feature thresholds.
- [ ] 1.2 Require preregistered stability across non-overlapping time windows, relevant slices, and multiple seeds/resamples, plus privacy/security approval for purpose, deletion, opt-out, access, retention, and disposal.
- [ ] 1.3 Require approved compute/memory/runtime/storage budgets, owners, and abort thresholds; confirm semantic replay/golden evaluation remains independent. Do not begin sections 2-8 unless tasks 1.1-1.3 are complete.

## 2. Conditional Prerequisite Contracts and Preflight

- [ ] 2.1 After activation, verify approved impression/export/evaluator contracts and expose one strict shared path for `PolicyConfiguration` decoding, normalization, cloning, validation, and hashing.
- [ ] 2.2 Add `cmd/recommendation-policy-learn` with activation-record reference, manifest/dataset/baseline, candidate/report output, seed, bounded optimization/coverage/improvement gates, and safe overwrite.
- [ ] 2.3 Implement strict artifact integrity/version validation and exactly eight finite bounded score components; fail closed on any mismatch.

## 3. Exposure, Splits, Sampling, and Coverage

- [ ] 3.1 After preflight succeeds, derive `observational-utility/v1` only through the shared evaluator implementation, admit only validated exposed rows, exclude every delivered/engaged-unexposed row from both pair sides, and test all eligibility and bounded exclusion categories.
- [ ] 3.2 Independently validate pseudonymous-user and temporal split contracts, including request ownership, ordered boundaries, label-horizon closure, embargo reconciliation, and cross-split leakage; prove by tests that test groups remain inaccessible until final evaluation.
- [ ] 3.3 Build canonical split-local request groups and deterministic label-gap preference pairs with fixed utility strata, seed-derived SHA-256 row/pair priority, canonical tie-breaking, and all per-request/per-split caps; verify row-order invariance and exact retained identities.
- [ ] 3.4 Compute split and per-feature coverage statistics, freeze sparse or constant features at baseline, and enforce the stricter of preregistered thresholds and operational floors; production runs may not lower activation gates.

## 4. Constrained Deterministic Optimizer

- [ ] 4.1 After 3.4 passes, implement the stable weighted pairwise logistic objective with L2 and baseline-anchor regularization, fixed feature/pair order, compensated loss/gradient accumulation, and projected-gradient norm; validate against small hand-calculated fixtures.
- [ ] 4.2 Implement deterministic projection for six nonnegative benefit weights, two nonpositive penalty weights, per-weight bounds, frozen weights, and the capped total absolute-weight bound; test boundary and infeasible-gradient cases.
- [ ] 4.3 Implement baseline initialization, bounded full-batch epochs, validation-only checkpoint selection, stable tie-breaking, tolerance/patience convergence, finite-value checks, and bounded trace capture; fail closed on non-convergence or absence of a valid checkpoint.
- [ ] 4.4 Add optimizer-level tests for repeatability, convergence selection, numerical failures, frozen-feature invariance, every sign/box/L1 constraint, and proof that test data cannot influence gradients, hyperparameters, stopping, or checkpoint choice.

## 5. Candidate Assembly and Held-Out Evaluation

- [ ] 5.1 After checkpoint selection, deep-clone the normalized baseline, replace only the eight `feature_weights`, and revalidate the complete candidate; canonically prove every non-weight field is unchanged and render no policy ID, version, enabled state, activation request, or rollout instruction.
- [ ] 5.2 Evaluate baseline and candidate exactly once on identical untouched preregistered test groups, preserving baseline parity, replay scope, observational limits, exclusions, warnings, and metric definitions.
- [ ] 5.3 Require primary improvement at least the preregistered MPE with adjusted 95% lower bound above zero, every guardrail confidence bound on the safe side of zero, and stability across required windows/slices/seeds; reject any degradation tolerance.

## 6. Deterministic Reporting and Atomic Local Publication

- [ ] 6.1 After all gates pass, render a versioned canonical report containing activation-record/preregistered thresholds, hashes, versions, counts, hyperparameters, convergence, coverage/freezes, baseline/learned weights, strict-improvement/stability results, warnings, and inactive candidate-only status.
- [ ] 6.2 Implement permission-restricted sibling partial files, sync and cross-hash verification, atomic candidate/report publication, safe replacement, cleanup, and preservation of existing outputs; inject render/write/sync/rename/overwrite failures to verify fail-closed behavior.
- [ ] 6.3 Add an integration boundary test proving successful and failed runs only read local inputs and write requested local artifacts, never modify inputs or policy state, and never open database, cache, broker, HTTP, embedding, or model-server connections.

## 7. End-to-End Safety and Determinism Tests

- [ ] 7.1 Create seeded synthetic user/time-split fixtures with known signed preferences and add recovery plus golden candidate/report tests that verify expected ordering, weight direction, bounds, and exact artifact bytes.
- [ ] 7.2 Add end-to-end repeated-run and reordered-input tests covering eligibility, sampling, pairs, checkpoints, weights, metrics, and bytes, plus leakage, sparse/constant coverage, mixed versions, corrupt artifacts, strict baseline decoding, and non-weight mutation rejection.
- [ ] 7.3 Add end-to-end failure tests for inadequate evidence, non-finite optimization, non-convergence, evaluator regressions, output failures, partial cleanup, and pre-existing-output preservation, asserting that no candidate is persisted or activated.

## 8. Documentation and Final Validation

- [ ] 8.1 Document indefinite deferral, activation prerequisites, preregistration, power/label/stability/privacy/resource gates, objective/sampling, strict improvement, deterministic outputs, permissions, failure recovery, and observational limitations.
- [ ] 8.2 Verify the narrow scope: semantic evaluation has no learner dependency; only eight weights may change after activation; database writes, auto-activation, online inference, A/B systems, other fields, embeddings, vector stores, neural models, and runtime dependencies remain excluded.
- [ ] 8.3 After activation and implementation only, run focused learner/evaluator/policy tests, relevant Go suites/builds, and `openspec validate --all --strict`; before activation, validation checks planning coherence only.
