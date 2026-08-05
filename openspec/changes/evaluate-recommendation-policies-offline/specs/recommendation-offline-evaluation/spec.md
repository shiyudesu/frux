## ADDED Requirements

### Requirement: Validated Versioned Dataset Input
Frux SHALL evaluate only a supported versioned recommendation export whose manifest, compressed bytes, and rows pass strict integrity and compatibility validation.

#### Scenario: Valid export is supplied
- **WHEN** the operator supplies a supported manifest and its matching gzip JSONL dataset within configured row and line bounds
- **THEN** the evaluator verifies manifest and dataset hashes, compressed size, filename, row count, schema/version sets, canonical fields, and gzip/JSONL completeness before computing metrics

#### Scenario: Dataset integrity is corrupted
- **WHEN** the dataset is truncated, contains trailing invalid bytes, has a checksum or size mismatch, disagrees with manifest counts, or contains malformed or duplicate rows
- **THEN** evaluation fails without publishing a report and identifies the bounded integrity category without echoing row payloads

#### Scenario: Dataset semantics are unsupported
- **WHEN** the manifest or a row declares an unknown dataset, label, record, feature, source-model, or pseudonymization version
- **THEN** evaluation fails explicitly rather than guessing, coercing, or partially evaluating mixed semantics

#### Scenario: Dataset exceeds the configured bound
- **WHEN** the manifest row count exceeds the validated evaluator limit
- **THEN** the command fails before decompression and does not sample or silently truncate the dataset

### Requirement: Production-Compatible Policy Validation
The evaluator SHALL load exactly one named baseline and one or more named candidate `PolicyConfiguration` JSON files and validate them with the same supported names, normalization, and bounds as production.

#### Scenario: Valid policy files are supplied
- **WHEN** every policy file strictly decodes to a production-valid `PolicyConfiguration`
- **THEN** the evaluator records its input-file SHA-256, canonical normalized-configuration SHA-256, and normalized configuration in the report

#### Scenario: Policy configuration is invalid
- **WHEN** a file contains trailing JSON, unknown fields or feature/provider names, duplicate report names, non-finite values, or a production-invalid bound or map relationship
- **THEN** evaluation fails before replay and reports the bounded policy/configuration error

#### Scenario: Policy contains non-replayable differences
- **WHEN** a candidate differs from the baseline in recall, feature-generation, exposure, suppression, fallback, rollout, sampling, retention, snapshot, or other settings not represented by logged components
- **THEN** the evaluator validates the configuration but lists those fields as non-replayable and does not claim to have evaluated their effects

### Requirement: Deterministic Linear Score Replay
The evaluator SHALL recompute scores from logged normalized components using a versioned fixed feature order and replay production stable ordering over each exported request candidate set.

#### Scenario: Candidate set is replayed
- **WHEN** a request group contains valid required score components, publication times, video IDs, author keys, and recall reasons
- **THEN** each policy score is the finite weighted sum in registry order and candidates sort by descending score, descending publication time, and descending video ID

#### Scenario: Required score component is absent or invalid
- **WHEN** a candidate omits, duplicates, or provides a non-finite or out-of-range registered component
- **THEN** the evaluator rejects the incompatible row rather than substituting a value

#### Scenario: Baseline replay differs from logged order
- **WHEN** the recomputed baseline ordering does not match exported absolute positions over comparable rows
- **THEN** the evaluator counts and warns about the disagreement before reporting policy metrics

### Requirement: Deterministic Diversity Replay
The evaluator SHALL reproduce production author and recall-content diversity over the available sorted candidate set.

#### Scenario: Diversity constraints are feasible
- **WHEN** the sorted candidates can satisfy author caps and configured author/content gaps
- **THEN** the evaluator selects the first stable eligible candidate at each position using pseudonymous author equality and the production recall-provider content bucket

#### Scenario: Gaps are temporarily infeasible
- **WHEN** no remaining candidate satisfies the configured gaps but an author-cap-valid candidate exists
- **THEN** the evaluator retries without gaps while preserving sorted candidate order

#### Scenario: Author caps are infeasible
- **WHEN** no remaining candidate satisfies the author cap
- **THEN** the evaluator appends the stable sorted remainder exactly as the production bounded fallback does

### Requirement: Explicit Replay Scope and Limitations
The evaluator SHALL distinguish observed served-subset replay from exact full-pool replay and SHALL never imply outcomes for candidates or ranks absent from the export.

#### Scenario: Dataset contains delivered impressions only
- **WHEN** the supported dataset schema is evaluated
- **THEN** the report sets full-pool replay availability to false, labels results `served_subset_replay`, and reports candidate counts, absolute-position gaps, delivered-page coverage, and baseline-order agreement

#### Scenario: Candidate policy changes ordering
- **WHEN** a candidate moves an observed item into top K
- **THEN** the evaluator uses only that item's observed label and states that the result does not estimate the outcome that would have occurred under the new position

#### Scenario: Request metadata is incomplete
- **WHEN** a request cannot support deterministic score, tie-break, diversity, or identity checks
- **THEN** it is excluded with a bounded reason and its count appears in both reports

### Requirement: Versioned Composite Observational Label
The evaluator SHALL define and report `observational-utility/v1` as a bounded relevance label derived from watch ratio, effective watch, completion, like, favorite, follow, quick skip, and eligible explicit feedback.

#### Scenario: Eligible observed row is labeled
- **WHEN** a row has an observed exposure or positive engagement
- **THEN** utility is `clamp(0.35*watch_ratio_term + 0.15*effective_watch_term + 0.15*completed + 0.10*liked + 0.15*favorited + 0.10*followed - 0.20*quick_skip - 0.35*eligible_not_interested - 0.25*eligible_reduce_author - 0.15*eligible_already_seen, 0, 1)`

#### Scenario: Watch terms are derived
- **WHEN** watch data is available
- **THEN** `watch_ratio_term` is the bounded ratio or zero, `effective_watch_term` is effective watch divided by 30 seconds and clamped to one, and quick skip requires exposure, skip, at most three seconds effective watch, and absent or at most 0.10 watch ratio

#### Scenario: Negative feedback is ineligible
- **WHEN** explicit negative facts exist without the export's negative-label eligibility
- **THEN** their negative utility terms are zero and the report retains the source-quality count

#### Scenario: Delivered card is unobserved
- **WHEN** a delivered row has neither exposure nor positive engagement
- **THEN** it remains unlabeled rather than becoming a zero-relevance negative

### Requirement: Deterministic Observational Metrics
The evaluator SHALL compute deterministic policy metrics over identical exported request groups and SHALL label every estimate observational.

#### Scenario: Ranking metrics are computed
- **WHEN** every replayed top-K row for a request has an eligible label
- **THEN** the evaluator includes complete-label NDCG@K using graded gain `2^relevance - 1`, average utility, and the request in the corresponding denominator

#### Scenario: Top-K labels are incomplete
- **WHEN** any replayed top-K row is unlabeled
- **THEN** the evaluator excludes that request from NDCG@K, retains it for applicable non-label metrics, and reports complete-label coverage

#### Scenario: Engagement metrics are computed
- **WHEN** eligible top-K rows exist
- **THEN** the evaluator reports effective watch, known watch ratio, completion, quick-skip, explicit-feedback, like, favorite, and follow rates with explicit denominators

#### Scenario: Coverage and concentration are computed
- **WHEN** selected top-K rows contain video and pseudonymous author keys
- **THEN** the evaluator reports distinct observed content/author coverage, content/author HHI concentration, and largest-author share against the observed candidate-set universe

#### Scenario: Recall-source mix is computed
- **WHEN** selected rows contain one or more bounded recall reasons
- **THEN** each item contributes equal fractional credit across its reasons and the evaluator reports provider shares and the multi-source item rate

### Requirement: Policy, Position, Degraded, and Schema Slices
The evaluator SHALL emit bounded sample and metric slices needed to identify observational composition differences.

#### Scenario: Slice dimensions are available
- **WHEN** metrics are aggregated
- **THEN** results include source policy version, degraded, healthy, or unknown state, dataset schema, source record schema, feature schema, source model, and logged absolute-position bands `0`, `1-4`, `5-9`, and `10+`

#### Scenario: Slice is small
- **WHEN** a slice has too few samples for statistical inference
- **THEN** the evaluator retains its counts and point estimates but omits an invalid confidence interval with an explicit reason

### Requirement: Sample Coverage and Join Quality
The evaluator SHALL reconcile and report the usable observational sample and all exclusions without silently repairing incompatible data.

#### Scenario: Evaluation completes
- **WHEN** all valid rows and request groups have been processed
- **THEN** reports include manifest/parsed counts, users, requests, items, request-size distribution, label/exposure/watch coverage, absolute-position gaps, baseline-order agreement, source-version distribution, and exclusions by bounded reason

#### Scenario: Identity or position invariants conflict
- **WHEN** duplicate request/video membership, contradictory author/video metadata, invalid absolute positions, or impossible canonical ordering is detected
- **THEN** evaluation fails for structural corruption rather than selecting one conflicting value

### Requirement: Deterministic Bootstrap Confidence Intervals
The evaluator SHALL provide deterministic user-clustered bootstrap confidence intervals only for statistically valid additive observational estimates.

#### Scenario: Metric has adequate clustered samples
- **WHEN** an additive mean, rate, NDCG, or paired candidate-minus-baseline delta has at least 30 eligible pseudonymous user clusters and finite non-degenerate values
- **THEN** the evaluator reports a deterministic percentile 95% interval using the configured bounded replicate count and a seed derived from input, policy, label, metric, and slice hashes

#### Scenario: Metric is unsuitable or undersampled
- **WHEN** a metric is a global unique count/concentration diagnostic or lacks adequate clusters or variance
- **THEN** no interval is emitted and the machine-readable report states the unavailable reason

#### Scenario: Evaluation is repeated
- **WHEN** identical inputs, policies, tool version, options, and output paths are used
- **THEN** bootstrap samples, estimates, intervals, and report bytes are identical

### Requirement: Observational and Non-Causal Interpretation
The evaluator SHALL clearly state that results are observational and SHALL not implement propensity estimators without a future validated propensity contract.

#### Scenario: Reports are rendered
- **WHEN** evaluation succeeds
- **THEN** JSON and Markdown state that logged outcomes are position-biased, served-subset reordering is not causal lift, and confidence intervals describe only the observed export

#### Scenario: Current dataset lacks randomized propensities
- **WHEN** candidate-minus-baseline differences are computed
- **THEN** the evaluator does not compute or mention IPS, SNIPS, doubly robust, unbiased counterfactual lift, or experiment significance

#### Scenario: Propensity fields appear in a future dataset
- **WHEN** a later schema introduces propensity-like fields
- **THEN** this evaluator rejects the unsupported schema until a separate specification defines validation and estimator semantics

### Requirement: Canonical Machine and Human Reports
The evaluator SHALL atomically produce deterministic canonical JSON and concise Markdown reports containing enough metadata to reproduce and interpret the run.

#### Scenario: Evaluation succeeds
- **WHEN** all inputs validate and metrics complete
- **THEN** JSON and Markdown contain dataset/manifest/policy hashes, tool/replay/label/report versions, policy differences, metric definitions, sample counts, exclusions, warnings, slices, estimates, confidence intervals, and replay/causal limitations

#### Scenario: Same evaluation is repeated
- **WHEN** identical inputs and options are evaluated again
- **THEN** reports contain no wall-clock-dependent value and are byte-for-byte identical

#### Scenario: Output publication fails
- **WHEN** rendering, writing, syncing, or final publication fails
- **THEN** partial files are removed, existing final reports remain untouched, and no input file is modified

### Requirement: Downstream Learned-Weight Boundary
The evaluator SHALL expose a versioned report contract that a later `learn-recommendation-policy-weights` change can consume without incorporating training into this capability.

#### Scenario: Learned-weight work is introduced later
- **WHEN** `learn-recommendation-policy-weights` evaluates generated candidate configurations
- **THEN** it may invoke or consume this evaluator's report but owns optimization, training, candidate generation, and any promotion decision

#### Scenario: Evaluator runs
- **WHEN** a policy comparison is requested
- **THEN** it never writes policy state, trains weights, activates a policy, calls model inference, or changes online serving
