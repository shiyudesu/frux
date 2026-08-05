## 1. Prerequisites and Dataset Contract

**Depends on:** implemented `persist-recommendation-training-impressions` and `export-recommendation-training-dataset` changes with finalized versioned contracts.

- [ ] 1.1 Verify the prerequisite changes expose final canonical manifest/row types, delivered-impression identity, absolute positions, recall reasons, score components, label semantics, version constants, and deterministic gzip behavior; block implementation rather than duplicating or guessing any unfinished contract.
- [ ] 1.2 Extend the supported dataset row, manifest, and exporter joins with canonical candidate `published_at`, domain-separated HMAC `author_key`, trusted `degraded_state` (`healthy`, `degraded`, or `unknown`), and bounded sorted degraded providers, while excluding raw author identity and emitting `unknown` when no trusted durable degraded record exists.
- [ ] 1.3 Update canonical serialization, manifest enumeration, checkpoint/resume compatibility, privacy validation, and deterministic fixtures, with tests covering author-key stability/domain separation, timestamp normalization, degraded-state variants and provider bounds, missing metadata, privacy exclusions, resume mismatch, and byte-for-byte repeatability.

## 2. Shared Policy Validation and Evaluation Domain

**Depends on:** task 1.1 for finalized source version names and task 1.2 for the replay metadata contract.

- [ ] 2.1 Export the existing recommendation policy normalization/validation entry point for shared use by `NewPolicy` and the evaluator, with regression tests proving existing valid and invalid production configurations retain identical behavior.
- [ ] 2.2 Define `recommendationevaluation` domain types and explicit registries for supported dataset/source versions, fixed feature order, replay/label/report versions, policy descriptors and hashes, K/row/bootstrap bounds, slices, estimates, intervals, warnings, and bounded identity-safe errors.
- [ ] 2.3 Implement strict named baseline/candidate policy decoding, production normalization, duplicate name/hash rejection, input and canonical configuration hashes, and replayable versus non-replayable difference classification, with table-driven coverage of supported names, bounds, malformed JSON/maps, non-finite values, hashes, and difference reporting.

## 3. Dataset Integrity Reader

**Depends on:** tasks 1.2-1.3 for canonical export bytes and task 2.2 for supported-version registries and errors.

- [ ] 3.1 Implement strict manifest preflight for schema/tool/source versions, dataset basename, compressed size, hashes/counts, evaluator row bounds, and required replay metadata, failing before decompression for unsupported or oversized input.
- [ ] 3.2 Implement streaming compressed-byte SHA-256 and deterministic multi-member gzip JSONL validation with bounded lines, strict row decoding, canonical UTC and finite-number checks, declared ordering enforcement, gzip completeness, and errors that never echo row payloads.
- [ ] 3.3 Reconcile manifest state/label/split/version counts with parsed rows; reject duplicate or contradictory identity/position/metadata and integrity failures; group valid rows by pseudonymous user/request for replay and quality summaries; add fixtures covering valid, empty, multi-member, corrupted, unsupported, oversized, unsorted, conflicting, and count-mismatched inputs.

## 4. Score, Ordering, Diversity, and Replay Scope

**Depends on:** task 2.3 for normalized policies and task 3.3 for validated request groups.

- [ ] 4.1 Implement `linear-replay/v1` using exactly one finite `[0,1]` component per registered feature in fixed order, then sort by descending score, descending `published_at`, and descending video ID; test ties, negative weights, missing/invalid components, and deterministic equal values.
- [ ] 4.2 Implement production diversity with pseudonymous author equality, lexicographically minimum recall-provider content buckets, author caps, author/content gaps, gap-relaxed retry, and stable infeasible-cap fallback; cover feasible, relaxed, fallback, one-author, and multi-reason cases.
- [ ] 4.3 Compare baseline replay with logged absolute positions and report agreement, inversions, rank/page gaps, and candidate counts; classify results only as `served_subset_replay`, set full-pool replay unavailable, exclude incomplete metadata with bounded reasons, and verify agreement/disagreement and repeated-run behavior with golden fixtures.

## 5. Observational Labels and Metrics

**Depends on:** task 4.3 for replayed policy rankings and explicit served-subset classifications.

- [ ] 5.1 Implement `observational-utility/v1` exactly as specified, including bounded watch terms, three-second/0.10 quick-skip logic, negative-label eligibility, clamping, and unlabeled delivered-unexposed rows, with tests for every term, boundary, and conflict.
- [ ] 5.2 Implement complete-label NDCG@K with graded gain and observed-set IDCG plus top-K utility, effective-watch, known watch-ratio, completion, quick-skip, explicit-feedback, like, favorite, follow, and combined-negative rates, each with explicit eligible denominators and missing-label coverage.
- [ ] 5.3 Implement observed-universe video/author coverage, video/author HHI, largest-author share, fractional recall-source mix, and multi-source item rate, preserving deterministic aggregation order and withholding causal or full-pool interpretations.
- [ ] 5.4 Implement paired candidate-minus-baseline estimates on identical requests, bounded source-policy/degraded/schema/model/position slices, and sample/join-quality summaries for rows, users, requests, items, request sizes, label/watch/exposure coverage, rank gaps, baseline agreement, source versions, known degraded coverage, and exclusions; verify all metric families and slices with golden tests.

## 6. Deterministic Observational Uncertainty

**Depends on:** task 5.4 for paired request-level observations, metric eligibility, and slice keys.

- [ ] 6.1 Implement deterministic user-cluster bootstrap sampling with cluster preservation and seeds derived from manifest, normalized policies, replay/label, metric, slice, and replicate hashes, including paired candidate/baseline sampling.
- [ ] 6.2 Emit percentile 95% intervals only for eligible additive means, rates, NDCG, and paired deltas with at least 30 finite non-degenerate user clusters; emit explicit unavailable reasons for undersampled, degenerate, global coverage/concentration, count, and quality metrics; test fixed seeds, repeatability, gates, and finite intervals.

## 7. Canonical Reports and Operator CLI

**Depends on:** tasks 4.3, 5.4, and 6.2 for replay warnings, observational estimates, slices, and interval availability.

- [ ] 7.1 Define the versioned canonical JSON report model and deterministic Markdown renderer containing input/tool/schema/policy hashes, normalized policies and non-replayable differences, replay scope, label/metric definitions, samples, exclusions, warnings, slices, estimates, intervals, and prominent observational/non-causal limitations.
- [ ] 7.2 Implement permission-restricted sibling partial files, sync, atomic publication, safe overwrite, and cleanup so both reports publish together, contain no wall-clock-dependent values, preserve existing outputs on failure, and never mutate inputs.
- [ ] 7.3 Add `apps/api/cmd/recommendation-policy-evaluate` with strict manifest/dataset, single baseline, repeatable candidate, JSON/Markdown output, K, row/bootstrap bound, and overwrite flags; ensure help and errors state served-subset observational scope and reject IPS, causal-lift, full-pool, and non-weight/diversity replay claims.
- [ ] 7.4 Add command/filesystem failure tests and one end-to-end multi-user golden dataset covering pages, policies, degraded/schema/position slices, rank gaps, authors, recall sources, labels, corrupted inputs/configs, unsupported versions, atomic failures, exact report content/hashes, and byte-for-byte repeatability.

## 8. Documentation and Downstream Boundary

**Depends on:** task 7.3 for the final operator interface and task 7.1 for report/metric terminology.

- [ ] 8.1 Document prerequisites, build/run examples, flags and bounds, supported schemas/features, policy file format and hashes, output permissions, integrity/configuration failures, `observational-utility/v1`, metric denominators, slices, bootstrap gates, quality fields, and unlabeled-versus-negative semantics.
- [ ] 8.2 Update recommendation, engineering/architecture, and privacy/export documentation for self-contained replay metadata, served-subset/full-pool limits, position bias, observational-only interpretation, and the prohibition on propensity-free IPS or causal claims; state that `learn-recommendation-policy-weights` may consume the versioned report but owns training, optimization, promotion, activation, rollout, dashboards, embeddings, pgvector, and inference.

## 9. Validation

**Depends on:** tasks 1.1-8.2.

- [ ] 9.1 Run targeted policy-validation, dataset/export compatibility, evaluator reader/replay/label/metric/bootstrap/report, CLI/filesystem, and golden tests.
- [ ] 9.2 Run `cd apps/api && go test ./...` and `go build ./cmd/feed ./cmd/worker ./cmd/recommendation-dataset-export ./cmd/recommendation-policy-evaluate`.
- [ ] 9.3 Run `openspec validate --all --strict` and confirm the proposal, design, both delta specs, 27-task checklist, prerequisite sequencing, evaluator contracts, observational limitations, and downstream training boundary remain coherent.
