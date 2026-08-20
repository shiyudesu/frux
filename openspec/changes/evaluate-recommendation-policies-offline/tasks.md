## 1. Low-Data Input Contracts

**Does not depend on:** `export-recommendation-training-dataset` or `learn-recommendation-policy-weights`.

- [ ] 1.1 Define bounded versioned replay-bundle, golden-set, and optional observation schemas using `(user_key, request_key, generation, video_id)`, generation-relative position, publication time, pseudonymous author/topic keys, reasons/components, degraded metadata, and occurred/recorded semantics.
- [ ] 1.2 Define the privacy-reviewed blinded 0-3 semantic rubric, annotation instructions, minimum two independent judgments, adjudication for disagreements of at least two points, and agreement reporting.
- [ ] 1.3 Add strict hashes/counts/schema/privacy validation and deterministic fixtures for complete frozen pools, delivered subsets, human annotations, unknown degraded state, missing metadata, and optional observations.

## 2. Shared Policy Validation and Evaluation Domain

- [ ] 2.1 Export the existing recommendation policy normalization/validation entry point for shared use by `NewPolicy` and the evaluator, with regression tests proving existing valid and invalid production configurations retain identical behavior.
- [ ] 2.2 Define evaluator domain registries for input/rubric/replay/report versions, fixed feature order, policy descriptors, K/case bounds, metric availability, warnings, and bounded identity-safe errors.
- [ ] 2.3 Strictly decode named baseline/candidate policies, classify replayable differences, and reject non-replayable differences by default; diagnostic-only override may list differences but must suppress comparative metrics and recommendations.

## 3. Input Integrity Reader

- [ ] 3.1 Implement strict replay/golden/optional-observation manifest preflight for versions, hashes, counts, case/candidate bounds, required metadata, and annotation provenance.
- [ ] 3.2 Strictly decode canonical timestamps and finite numbers, enforce generation/position and unique case/candidate invariants, and reject payloads or errors that expose raw identity.
- [ ] 3.3 Reconcile manifests with parsed inputs and add valid, empty, corrupted, unsupported, oversized, unsorted, conflicting, incomplete-annotation, and count-mismatch fixtures.

## 4. Production Scorer Replay and Parity

- [ ] 4.1 Implement `linear-replay/v1` using exactly one finite `[0,1]` component per registered feature in fixed order, then sort by descending score, descending `published_at`, and descending video ID; test ties, negative weights, missing/invalid components, and deterministic equal values.
- [ ] 4.2 Implement production diversity with pseudonymous author equality, lexicographically minimum recall-provider content buckets, author caps, author/content gaps, gap-relaxed retry, and stable infeasible-cap fallback; cover feasible, relaxed, fallback, one-author, and multi-reason cases.
- [ ] 4.3 Require 100% exact baseline order on canonical production fixtures; report parity, inversions, generation/rank gaps, and candidate counts on diagnostic bundles, and invalidate exact-replay claims on any unexplained mismatch.
- [ ] 4.4 Distinguish manifest-proven `full_pool_fixture_replay` from `served_subset_replay`; never infer absent candidates or counterfactual outcomes.

## 5. Human Golden and Diagnostic Metrics

- [ ] 5.1 Implement adjudicated 0-3 semantic labels, judge/agreement summaries, semantic NDCG@K, thresholded precision/recall, and pairwise preference accuracy with explicit case/candidate denominators.
- [ ] 5.2 Implement recall coverage of adjudicated relevant items overall and by source, plus source contribution and multi-source rates.
- [ ] 5.3 Implement video/author/topic coverage, HHI/concentration, largest-group share, and repeated author/topic run metrics over frozen candidate pools.
- [ ] 5.4 Keep `observational-utility/v1` only for optional eligible observations; emit quick-skip and explicit-negative metrics only when denominators are positive, otherwise `unavailable` rather than zero.
- [ ] 5.5 Add paired baseline/candidate semantic deltas, replay/degraded/schema/position slices, sample-quality summaries, exclusions, and prominent non-causal warnings.

## 6. Optional Sample-Appropriate Uncertainty

- [ ] 6.1 Always emit deterministic point estimates, numerators/denominators, and sample counts without requiring bootstrap or a minimum user population.
- [ ] 6.2 Add optional preregistered case-level bootstrap, exact/binomial intervals, or user-cluster bootstrap only when their assumptions and minimum samples hold; otherwise emit an explicit unavailable reason.

## 7. Canonical Reports and Operator CLI

- [ ] 7.1 Define deterministic JSON/Markdown reports containing input/policy hashes, replay scope/parity, rubric/annotation agreement, semantic/recall/diversity metrics, optional observations, metric availability, exclusions, and prominent non-causal limitations.
- [ ] 7.2 Implement permission-restricted sibling partial files, sync, atomic publication, safe overwrite, and cleanup so both reports publish together, contain no wall-clock-dependent values, preserve existing outputs on failure, and never mutate inputs.
- [ ] 7.3 Add `apps/api/cmd/recommendation-policy-evaluate` with replay-bundle, golden-set, optional-observations, baseline/candidate, output, K/case-bound, uncertainty, diagnostic-only, and overwrite flags.
- [ ] 7.4 Add command/filesystem tests and end-to-end small golden fixtures covering exact parity, non-replayable rejection, semantic judgments, recall, author/topic diversity, optional/absent behavior samples, atomic failures, hashes, and repeatability.

## 8. Documentation and Deferred Training Boundary

- [ ] 8.1 Document low-data inputs, annotation rubric/blinding/adjudication, exact baseline parity, replayable policy scope, semantic/recall/diversity metrics, optional behavior denominators, and sample-appropriate uncertainty.
- [ ] 8.2 Document exporter independence, served-subset/full-pool limits, position bias, no causal-lift claim, default non-replayable rejection, and that deferred weight learning is not a semantic-roadmap prerequisite.

## 9. Validation

- [ ] 9.1 Run targeted policy validation, input reader, replay/parity, golden annotation, semantic/recall/diversity, optional observation, uncertainty, report, CLI/filesystem, and golden tests.
- [ ] 9.2 Run the relevant Go suite and build `./cmd/feed`, `./cmd/worker`, and `./cmd/recommendation-policy-evaluate`; the future dataset-export binary is not an evaluator prerequisite.
- [ ] 9.3 Run `openspec validate --all --strict` and confirm proposal, design, both delta specs, low-data inputs, exporter independence, non-causal limits, and deferred training boundary remain coherent.
