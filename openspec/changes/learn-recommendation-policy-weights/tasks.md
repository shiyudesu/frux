## 1. Prerequisite Contracts and Offline Boundaries

- [ ] 1.1 Verify that the implemented impression, export, and evaluator prerequisites provide the required versioned fields and reusable integrity, label, replay, metric, and bootstrap APIs; add focused compatibility tests that fail closed when any contract is absent or unsupported.
- [ ] 1.2 Expose one shared strict path for `PolicyConfiguration` decoding, normalization, cloning, validation, and canonical hashing, and verify production, evaluator, and learner callers produce identical normalized results.
- [ ] 1.3 After 1.1-1.2, create the learner packages and fixed eight-feature/compatibility/sampler/optimizer/report registries; enforce with dependency tests that they cannot initialize or import database, cache, broker, HTTP, embedding, or model-serving infrastructure.

## 2. Command and Preflight Validation

- [ ] 2.1 After 1.3, add `cmd/recommendation-policy-learn` with manifest, dataset, baseline, candidate/report output, seed, bounded optimization/coverage/evaluator-gate, and safe-overwrite options; test required flags, relationships, finite bounds, path separation, and existing-output rejection.
- [ ] 2.2 Implement strict baseline loading plus export manifest/dataset integrity validation, including input and normalized hashes, filename/size/SHA-256 checks, gzip/JSONL completeness, canonical ordering, row limits, duplicate detection, and count reconciliation; validate with valid and corrupted fixtures.
- [ ] 2.3 Enforce exact supported contract versions and exactly the eight finite bounded score components, rejecting missing, duplicate, unknown, mixed-version, non-finite, or out-of-range input without coercion, imputation, or skipped structural errors.

## 3. Exposure, Splits, Sampling, and Coverage

- [ ] 3.1 After preflight succeeds, derive `observational-utility/v1` only through the shared evaluator implementation, admit only validated exposed rows, exclude every delivered/engaged-unexposed row from both pair sides, and test all eligibility and bounded exclusion categories.
- [ ] 3.2 Independently validate pseudonymous-user and temporal split contracts, including request ownership, ordered boundaries, label-horizon closure, embargo reconciliation, and cross-split leakage; prove by tests that test groups remain inaccessible until final evaluation.
- [ ] 3.3 Build canonical split-local request groups and deterministic label-gap preference pairs with fixed utility strata, seed-derived SHA-256 row/pair priority, canonical tie-breaking, and all per-request/per-split caps; verify row-order invariance and exact retained identities.
- [ ] 3.4 Compute split and per-feature coverage statistics, freeze sparse or constant features exactly at normalized baseline values, and enforce exposed-row/user/request/pair/trainable-feature gates before optimization; cover each freeze reason and failed aggregate gate with tests.

## 4. Constrained Deterministic Optimizer

- [ ] 4.1 After 3.4 passes, implement the stable weighted pairwise logistic objective with L2 and baseline-anchor regularization, fixed feature/pair order, compensated loss/gradient accumulation, and projected-gradient norm; validate against small hand-calculated fixtures.
- [ ] 4.2 Implement deterministic projection for six nonnegative benefit weights, two nonpositive penalty weights, per-weight bounds, frozen weights, and the capped total absolute-weight bound; test boundary and infeasible-gradient cases.
- [ ] 4.3 Implement baseline initialization, bounded full-batch epochs, validation-only checkpoint selection, stable tie-breaking, tolerance/patience convergence, finite-value checks, and bounded trace capture; fail closed on non-convergence or absence of a valid checkpoint.
- [ ] 4.4 Add optimizer-level tests for repeatability, convergence selection, numerical failures, frozen-feature invariance, every sign/box/L1 constraint, and proof that test data cannot influence gradients, hyperparameters, stopping, or checkpoint choice.

## 5. Candidate Assembly and Held-Out Evaluation

- [ ] 5.1 After checkpoint selection, deep-clone the normalized baseline, replace only the eight `feature_weights`, and revalidate the complete candidate; canonically prove every non-weight field is unchanged and render no policy ID, version, enabled state, activation request, or rollout instruction.
- [ ] 5.2 Evaluate baseline and candidate exactly once on identical untouched test request groups through the shared evaluator, preserving served-subset replay, observational labels, production ordering/diversity, complete-label eligibility, clustering, exclusions, warnings, and metric definitions.
- [ ] 5.3 Apply held-out coverage/interval and NDCG, utility, quick-skip, and explicit-negative non-regression gates; test every unavailable-metric, integrity, coverage, new-exclusion, and regression failure before making artifacts publishable.

## 6. Deterministic Reporting and Atomic Local Publication

- [ ] 6.1 After all gates pass, render a versioned canonical report containing input/configuration hashes, versions, split/sampling counts, hyperparameters, convergence, coverage/freezes, baseline/learned weights, constraints, candidate hash, evaluator comparison, warnings, and inactive candidate-only status; verify byte determinism.
- [ ] 6.2 Implement permission-restricted sibling partial files, sync and cross-hash verification, atomic candidate/report publication, safe replacement, cleanup, and preservation of existing outputs; inject render/write/sync/rename/overwrite failures to verify fail-closed behavior.
- [ ] 6.3 Add an integration boundary test proving successful and failed runs only read local inputs and write requested local artifacts, never modify inputs or policy state, and never open database, cache, broker, HTTP, embedding, or model-server connections.

## 7. End-to-End Safety and Determinism Tests

- [ ] 7.1 Create seeded synthetic user/time-split fixtures with known signed preferences and add recovery plus golden candidate/report tests that verify expected ordering, weight direction, bounds, and exact artifact bytes.
- [ ] 7.2 Add end-to-end repeated-run and reordered-input tests covering eligibility, sampling, pairs, checkpoints, weights, metrics, and bytes, plus leakage, sparse/constant coverage, mixed versions, corrupt artifacts, strict baseline decoding, and non-weight mutation rejection.
- [ ] 7.3 Add end-to-end failure tests for inadequate evidence, non-finite optimization, non-convergence, evaluator regressions, output failures, partial cleanup, and pre-existing-output preservation, asserting that no candidate is persisted or activated.

## 8. Documentation and Final Validation

- [ ] 8.1 After behavior stabilizes, document prerequisite order, compatible inputs, exposure/split rules, objective and sampling, defaults/bounds/resource caps, convergence, evaluator gates, deterministic outputs, permissions, failure recovery, and observational limitations.
- [ ] 8.2 Document and verify the narrow scope: only the existing eight linear weights may change; database writes, persistence/activation, online inference, A/B systems, other policy fields, embeddings, vector stores, neural models, and production runtime dependencies remain excluded.
- [ ] 8.3 Run focused learner/evaluator/policy tests, the relevant Go package suite and both Go builds, then run `openspec validate --all --strict`; reconcile any failure with the proposal, design, specification, and this checklist before implementation is considered complete.
