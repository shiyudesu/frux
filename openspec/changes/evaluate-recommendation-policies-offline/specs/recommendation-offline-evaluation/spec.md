## ADDED Requirements

### Requirement: Validated Low-Data Evaluation Inputs
Frux SHALL evaluate only supported versioned replay bundles, blinded human golden sets, and optional observational bundles whose hashes, manifests, rows, and privacy bounds pass strict validation.

#### Scenario: Valid replay and golden inputs are supplied
- **WHEN** the operator supplies bounded replay cases and a matching versioned human golden set
- **THEN** the evaluator verifies hashes, counts, schemas, generation identity, canonical fields, annotation rubric/provenance, privacy exclusions, and completeness before computing metrics

#### Scenario: Input integrity is corrupted
- **WHEN** an input is truncated, has a checksum/count mismatch, contains malformed or duplicate cases/candidates, or violates identity or annotation invariants
- **THEN** evaluation fails without publishing a report and identifies the bounded integrity category without echoing row payloads

#### Scenario: Input semantics are unsupported
- **WHEN** a manifest, case, candidate, annotation, or optional observation declares an unknown replay, rubric, label, record, feature, source-model, or pseudonymization version
- **THEN** evaluation fails explicitly rather than guessing, coercing, or partially evaluating mixed semantics

#### Scenario: Input exceeds the configured bound
- **WHEN** case, candidate, annotation, or observation counts exceed validated limits
- **THEN** the command fails before evaluation and does not sample or silently truncate the input

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
- **THEN** the evaluator rejects comparative evaluation by default; an explicit diagnostic-only mode may list those fields but emits no winner, promotion decision, or comparative policy metric

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
- **THEN** any mismatch on canonical production replay fixtures fails evaluation, while diagnostic-subset mismatch is counted and prevents claims of exact parity

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

#### Scenario: Complete frozen replay fixture is supplied
- **WHEN** a replay manifest proves that its candidate pool is complete and records the expected production order
- **THEN** the evaluator may label the case `full_pool_fixture_replay` and requires exact baseline-order parity before comparing candidates

#### Scenario: Candidate policy changes ordering
- **WHEN** a candidate moves an observed item into top K
- **THEN** the evaluator uses only that item's observed label and states that the result does not estimate the outcome that would have occurred under the new position

#### Scenario: Request metadata is incomplete
- **WHEN** a request cannot support deterministic score, tie-break, diversity, or identity checks
- **THEN** it is excluded with a bounded reason and its count appears in both reports

### Requirement: Blinded Human Semantic Golden Set
The evaluator SHALL use a versioned, privacy-reviewed, blinded human golden set as the primary low-data relevance evidence.

#### Scenario: Candidate is judged
- **WHEN** an annotator reviews a context/candidate pair without policy name or rank
- **THEN** the annotator assigns the versioned 0-3 semantic relevance rubric and the input retains only bounded privacy-approved context

#### Scenario: Case receives annotations
- **WHEN** a golden case is accepted
- **THEN** every candidate has at least two independent judgments, disagreements of two or more points are adjudicated, and the report includes judge counts, adjudicated labels, and agreement statistics when defined

#### Scenario: Semantic metrics are computed
- **WHEN** adjudicated labels exist for a replayed case
- **THEN** the evaluator reports semantic NDCG@K, thresholded precision/recall, and pairwise preference accuracy with explicit case and candidate denominators

### Requirement: Optional Versioned Observational Label
When eligible observations are supplied, the evaluator SHALL define and report `observational-utility/v1` as a bounded diagnostic label derived from watch ratio, effective watch, completion, like, favorite, follow, quick skip, and eligible explicit feedback.

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

### Requirement: Deterministic Replay, Semantic, Diversity, and Optional Observational Metrics
The evaluator SHALL compute deterministic replay, semantic, recall, diversity, and optional observational metrics over identical cases.

#### Scenario: Ranking metrics are computed
- **WHEN** every replayed top-K candidate for a golden case has an adjudicated semantic label
- **THEN** the evaluator includes semantic NDCG@K using graded gain `2^relevance - 1` and the case in the corresponding denominator

#### Scenario: Top-K labels are incomplete
- **WHEN** any replayed top-K candidate lacks an adjudicated semantic label
- **THEN** the evaluator excludes that case from semantic NDCG@K, retains it for replay/diversity metrics, and reports complete-label coverage

#### Scenario: Engagement metrics are computed
- **WHEN** eligible optional observed top-K rows exist
- **THEN** the evaluator reports effective watch, known watch ratio, completion, quick-skip, explicit-feedback, like, favorite, and follow rates with explicit denominators

#### Scenario: Engagement sample is absent
- **WHEN** no eligible observed sample exists for quick skip, explicit negative feedback, or another behavior rate
- **THEN** the metric is `unavailable` with denominator zero and is not serialized as numeric zero

#### Scenario: Coverage and concentration are computed
- **WHEN** selected top-K candidates contain video, pseudonymous author, and bounded topic keys
- **THEN** the evaluator reports distinct content/author/topic coverage, author/topic concentration, largest-group share, and repeated-group runs against the frozen candidate-set universe

#### Scenario: Recall-source mix is computed
- **WHEN** selected rows contain one or more bounded recall reasons
- **THEN** each item contributes equal fractional credit across its reasons and the evaluator reports provider shares, multi-source rate, and coverage of adjudicated relevant items overall and by source

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

### Requirement: Optional Sample-Appropriate Uncertainty
The evaluator SHALL run without a large user population or bootstrap and SHALL emit uncertainty only when a preregistered method is valid for the available sample.

#### Scenario: Point estimate is available
- **WHEN** a metric has at least one eligible case or row
- **THEN** the evaluator reports the deterministic point estimate, numerator/denominator, and sample count without requiring an interval

#### Scenario: Preregistered uncertainty is valid
- **WHEN** a case-level bootstrap, exact/binomial interval, or optional user-cluster bootstrap meets its declared independence and minimum-sample assumptions
- **THEN** the evaluator emits a deterministic interval and records the method, assumptions, seed where applicable, and sample count

#### Scenario: Metric is unsuitable or undersampled
- **WHEN** assumptions or minimum samples are not met
- **THEN** no interval is emitted, the point estimate remains available, and the report states the unavailable reason

#### Scenario: Evaluation is repeated
- **WHEN** identical inputs, policies, tool version, options, and output paths are used
- **THEN** any uncertainty samples, estimates, intervals, and report bytes are identical

### Requirement: Observational and Non-Causal Interpretation
The evaluator SHALL clearly state that results are observational and SHALL not implement propensity estimators without a future validated propensity contract.

#### Scenario: Reports are rendered
- **WHEN** evaluation succeeds
- **THEN** JSON and Markdown state that optional logged outcomes are position-biased, served-subset reordering is not causal lift, and confidence intervals describe only the sampled golden cases or observations

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
- **THEN** JSON and Markdown contain input-manifest/policy hashes, tool/replay/rubric/label/report versions, policy differences, annotation agreement, metric definitions, sample counts, exclusions, warnings, slices, estimates, confidence intervals, and replay/causal limitations

#### Scenario: Same evaluation is repeated
- **WHEN** identical inputs and options are evaluated again
- **THEN** reports contain no wall-clock-dependent value and are byte-for-byte identical

#### Scenario: Output publication fails
- **WHEN** rendering, writing, syncing, or final publication fails
- **THEN** partial files are removed, existing final reports remain untouched, and no input file is modified

### Requirement: Deferred Learned-Weight Independence
The evaluator SHALL remain fully usable while `learn-recommendation-policy-weights` is indefinitely and conditionally deferred.

#### Scenario: Semantic roadmap proceeds
- **WHEN** Frux evaluates semantic relevance, recall, or diversity
- **THEN** no weight-learning artifact, sample-size gate, optimizer, or training export is required

#### Scenario: Learned-weight work is activated later
- **WHEN** a separately approved learner evaluates generated candidate configurations
- **THEN** it may consume this evaluator's report but owns optimization, training, candidate generation, and any promotion decision

#### Scenario: Evaluator runs
- **WHEN** a policy comparison is requested
- **THEN** it never writes policy state, trains weights, activates a policy, calls model inference, or changes online serving
