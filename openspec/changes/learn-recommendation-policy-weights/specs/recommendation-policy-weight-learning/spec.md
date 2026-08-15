## ADDED Requirements

### Requirement: Offline-Only Learned-Weight Boundary
Frux SHALL provide a standalone offline operator tool that learns only the existing production linear feature weights and SHALL NOT introduce a new online inference model or mutate production state.

#### Scenario: Operator runs weight learning
- **WHEN** valid local dataset, manifest, and baseline policy files are supplied
- **THEN** the tool performs all validation, optimization, and evaluation locally without connecting to PostgreSQL, Redis, Kafka, HTTP services, embedding services, or a model server

#### Scenario: Learning completes
- **WHEN** the tool produces a candidate
- **THEN** the candidate remains a local inactive artifact and the tool does not create, enable, activate, roll out, or persist a recommendation policy

#### Scenario: Unsupported learning scope is requested
- **WHEN** an input or option attempts to learn recall budgets, provider deadlines, diversity, suppression, retention, rollout, snapshot, sampling, fallback, decay, exposure-window, or other non-weight settings
- **THEN** the tool rejects the request rather than optimizing or modifying those settings

### Requirement: Explicit Prerequisite and Version Compatibility
The learner SHALL depend on the artifacts and contracts planned by `persist-recommendation-training-impressions`, `export-recommendation-training-dataset`, and `evaluate-recommendation-policies-offline` and SHALL accept only explicitly supported versions.

#### Scenario: Compatible inputs are supplied
- **WHEN** the manifest, compressed dataset, impression record schema, feature schema, dataset schema, label schema, source-model schema, pseudonymization schema, and evaluator label version are all registered as compatible
- **THEN** the learner verifies their hashes, sizes, counts, canonical fields, and version sets before constructing examples

#### Scenario: A prerequisite contract is unavailable
- **WHEN** the durable-impression fields, versioned export fields, evaluator composite label, or shared production policy validation required by the learner are absent
- **THEN** the learner fails preflight with a bounded dependency error and publishes no candidate or success report

#### Scenario: An unsupported or mixed version is present
- **WHEN** the manifest or any row declares an unknown or incompatible record, feature, dataset, label, source-model, pseudonymization, replay, or evaluator version
- **THEN** the learner fails closed rather than coercing, skipping, or partially training on the input

### Requirement: Existing Feature Registry Only
The learner SHALL optimize exactly the eight existing bounded score components: `content_similarity`, `session_similarity`, `hotness`, `freshness`, `author_affinity`, `follow_relation`, `negative_penalty`, and `exposure_penalty`.

#### Scenario: Training row is accepted
- **WHEN** a compatible row is decoded
- **THEN** it contains each registered finite score component exactly once in the supported bounded range and no learned feature outside the registry

#### Scenario: Feature payload is incomplete or unknown
- **WHEN** a row omits, duplicates, or adds a learned feature, or supplies a non-finite or out-of-range value
- **THEN** the learner rejects the dataset as incompatible rather than imputing, dropping, recomputing, or deriving a feature

#### Scenario: Semantic or neural feature is proposed
- **WHEN** an input references embeddings, pgvector, semantic vectors, two-tower features, sequence-model outputs, or another model score
- **THEN** the learner rejects it because those features are outside the production linear component registry

### Requirement: Exposure-Safe Training Examples
The learner SHALL construct optimization examples only from rows with validated exposure and a supported evaluator composite relevance label and SHALL never treat an unexposed delivered item as a negative.

#### Scenario: Delivered row has validated exposure
- **WHEN** the compatible export marks a row exposed and the evaluator can derive its versioned composite relevance label
- **THEN** the row is eligible for request-local pair construction

#### Scenario: Delivered row lacks validated exposure
- **WHEN** a row is `delivered_unexposed` or `engaged_unexposed`
- **THEN** the learner excludes it from optimization pairs and negative counts even if it contains a skip, explicit feedback, or positive engagement

#### Scenario: Label eligibility is ambiguous
- **WHEN** exposure, negative-label eligibility, label horizon, or evaluator label derivation cannot be verified
- **THEN** the row is excluded with a bounded reason and cannot become the lower-relevance side of a pair

### Requirement: Leakage-Safe Deterministic Splits
The learner SHALL honor and independently verify the export's deterministic time-based or pseudonymous-user-based train, validation, and test split contract before optimization.

#### Scenario: User-based split is supplied
- **WHEN** the manifest selects pseudonymous-user splitting
- **THEN** every row, request, and pair for one `user_key` belongs to exactly one split and no user key crosses train, validation, or test

#### Scenario: Time-based split is supplied
- **WHEN** the manifest selects time splitting
- **THEN** split boundaries are ordered, the embargo is at least the label horizon, excluded embargo rows are reconciled, and no row's label cutoff crosses into a later split

#### Scenario: Pair is constructed
- **WHEN** two examples form a ranking pair
- **THEN** they share the same request group and split and neither member nor its request is copied across splits

#### Scenario: Split integrity is violated
- **WHEN** a user, request, row, label horizon, boundary, or pair violates the declared split strategy
- **THEN** the learner refuses output with a bounded leakage category

### Requirement: Deterministic Bounded Pair Construction
The learner SHALL form bounded request-local preference pairs from the evaluator's versioned composite relevance label using deterministic class and pair sampling.

#### Scenario: Request contains distinguishable exposed labels
- **WHEN** two eligible rows in one request differ by at least the configured minimum label gap
- **THEN** the higher-label row is the preferred side, the pair uses the fixed-order component difference, and its bounded weight is derived deterministically from the label gap

#### Scenario: Request has excessive examples or pairs
- **WHEN** an exposure-label stratum, request, split, or complete run exceeds a configured bound
- **THEN** a stable seed-derived hash priority retains a bounded balanced subset independent of input order

#### Scenario: Labels are tied or too close
- **WHEN** the absolute label difference is below the configured threshold
- **THEN** the rows do not form a preference pair and the exclusion is counted

#### Scenario: Same inputs are repeated
- **WHEN** dataset bytes, baseline bytes, versions, hyperparameters, and seed are identical
- **THEN** example eligibility, sampled row identities, pair identities, order, weights, and counts are identical

### Requirement: Constrained Regularized Linear Optimization
The learner SHALL minimize a deterministic regularized pairwise linear-ranking objective starting from the validated baseline weights and SHALL project every update into the existing production constraint set.

#### Scenario: Optimization runs
- **WHEN** training pairs pass coverage gates
- **THEN** the learner uses the versioned pairwise logistic objective, fixed feature order, configured L2 and baseline-anchor regularization, deterministic seed, bounded iterations, deterministic accumulation order, and validation-based model selection

#### Scenario: Positive component is updated
- **WHEN** content similarity, session similarity, hotness, freshness, author affinity, or follow relation is optimized
- **THEN** its projected weight remains finite and nonnegative

#### Scenario: Penalty component is updated
- **WHEN** negative penalty or exposure penalty is optimized
- **THEN** its projected weight remains finite and nonpositive

#### Scenario: Production bounds are applied
- **WHEN** any update or selected checkpoint is considered
- **THEN** every absolute weight remains within the current production per-weight bound and total absolute weight remains within the current production total-weight bound

### Requirement: Sparse and Constant Feature Handling
The learner SHALL measure feature coverage on training data and SHALL handle sparse or constant components explicitly without inventing signal.

#### Scenario: Feature has adequate variation
- **WHEN** a component meets configured finite-row, nonzero-row, and range thresholds in the training split
- **THEN** it participates in gradient updates and its coverage and variation are recorded

#### Scenario: Feature is sparse or constant
- **WHEN** a component has insufficient nonzero coverage or no meaningful variation
- **THEN** the learner freezes it at the normalized baseline weight, excludes it from gradient updates, and records the reason and counts

#### Scenario: Overall data coverage is inadequate
- **WHEN** configured minimum exposed rows, users, requests, pairs, split populations, or trainable-feature counts are not met
- **THEN** the learner refuses candidate and success-report publication

### Requirement: Convergence and Held-Out Selection
The learner SHALL select weights using only training and validation data and SHALL reserve the test split for one final offline evaluator comparison.

#### Scenario: Training progresses
- **WHEN** deterministic optimization checkpoints are produced
- **THEN** the learner chooses the checkpoint with the best finite validation pairwise loss using stable tie-breaking and stops only through the configured convergence or bounded-iteration rules

#### Scenario: Optimizer does not converge
- **WHEN** loss is non-finite, projected gradients remain above the configured tolerance, validation loss does not satisfy the deterministic patience rule, or no valid checkpoint exists
- **THEN** the learner refuses candidate and success-report publication

#### Scenario: Test data is available
- **WHEN** checkpoint selection is complete
- **THEN** the test split is evaluated exactly once and does not affect pair sampling, gradients, hyperparameter selection, early stopping, or checkpoint choice

### Requirement: Baseline-Preserving Candidate Configuration
The learner SHALL start from a strictly decoded production-valid baseline `PolicyConfiguration` and SHALL produce a complete production-valid candidate configuration that differs only in feature weights.

#### Scenario: Baseline is loaded
- **WHEN** the operator supplies the baseline JSON
- **THEN** the learner applies the shared production normalization and bounds, requires the exact eight-feature registry, and records both the input-file hash and normalized configuration hash

#### Scenario: Candidate is assembled
- **WHEN** learned weights pass all optimizer gates
- **THEN** the learner deep-clones the normalized baseline, replaces only `feature_weights`, revalidates the complete configuration through production rules, and verifies canonical equality of every non-weight field

#### Scenario: Non-weight field changes
- **WHEN** recall, deadline, half-life, exposure, diversity, rollout, snapshot, sampling, retention, fallback, hard-suppression, or suppression values differ from the baseline
- **THEN** the learner rejects the candidate as an invariant violation

#### Scenario: Candidate file exists locally
- **WHEN** learning succeeds
- **THEN** the JSON is only an inactive `PolicyConfiguration` artifact with no policy ID, version, enabled state, activation request, database write, or rollout action

### Requirement: Offline Evaluator Regression Gates
The learner SHALL compare the selected candidate with the baseline on the untouched test split using the versioned replay, label, metric, and report semantics from `evaluate-recommendation-policies-offline`.

#### Scenario: Candidate is evaluated
- **WHEN** optimization and candidate validation succeed
- **THEN** the learner replays baseline and candidate over identical held-out request groups and records the evaluator version, replay scope, label version, metric definitions, sample counts, exclusions, warnings, estimates, and paired deltas

#### Scenario: Evaluator gate passes
- **WHEN** configured primary NDCG, composite utility, quick-skip, explicit-negative, and required sample/confidence gates all meet their non-regression thresholds
- **THEN** the candidate is eligible for atomic local publication

#### Scenario: Evaluator input or metric is unavailable
- **WHEN** replay integrity fails, required held-out coverage is insufficient, a required metric or interval is unavailable, or the evaluator reports an unsupported limitation
- **THEN** the learner refuses candidate and success-report publication

#### Scenario: Evaluator regression is detected
- **WHEN** any required candidate-minus-baseline delta crosses its configured regression threshold
- **THEN** the learner refuses output even if training and validation losses improved

### Requirement: Complete Deterministic Training Report
Successful learning SHALL emit a canonical machine-readable report containing enough metadata to reproduce, audit, and interpret the run.

#### Scenario: Success report is rendered
- **WHEN** every gate passes
- **THEN** the report contains compressed dataset and manifest hashes, baseline input and normalized hashes, dataset/record/feature/source-model/label/evaluator/tool versions, split policy, seed, all hyperparameters, pair-sampling rules, convergence trace summary, feature coverage and frozen reasons, learned and baseline weights, constraint checks, candidate hash, exclusions, and the held-out evaluator comparison

#### Scenario: Run is repeated
- **WHEN** input bytes, tool version, supported registries, hyperparameters, and output options are identical
- **THEN** learned weights, candidate JSON bytes, report JSON bytes, hashes, sampling, convergence result, and evaluator comparison are byte-for-byte identical

#### Scenario: Sensitive source fields exist
- **WHEN** dataset source material contains raw identities, tokens, URLs, arbitrary context, embeddings, key material, or raw event payloads
- **THEN** those fields do not appear in logs, errors, candidate JSON, or the training report

### Requirement: Atomic Fail-Closed Publication
The learner SHALL publish the candidate configuration and success report only after all validation, optimization, constraint, convergence, coverage, leakage, and evaluator gates pass.

#### Scenario: Learning succeeds
- **WHEN** both final artifacts render, sync, hash, and cross-reference successfully
- **THEN** permission-restricted sibling partial files are atomically renamed to the requested final paths and neither artifact can describe a different candidate

#### Scenario: Any gate or write fails
- **WHEN** validation, sampling, optimization, convergence, candidate assembly, evaluator comparison, rendering, writing, syncing, or publication fails
- **THEN** no new final candidate or success report is published, partial files are removed, existing final files remain untouched, and the command returns a bounded failure category

#### Scenario: Output path already exists
- **WHEN** final candidate or report paths exist without the explicit supported overwrite option
- **THEN** the command fails before training and does not append to or replace either file

### Requirement: Recovery, Determinism, and Safety Test Coverage
The implementation SHALL include synthetic, golden, and failure-path tests that prove recovery of known weights, deterministic behavior, constraint enforcement, leakage prevention, and fail-closed publication.

#### Scenario: Synthetic recoverable data is trained
- **WHEN** a seeded fixture is generated from known signed linear weights with adequate coverage
- **THEN** the learner recovers the expected ordering and bounded weight direction within documented tolerance and matches the golden candidate and report

#### Scenario: Determinism suite runs
- **WHEN** identical data is presented in different database/export iteration orders or the same run is repeated
- **THEN** sampled pairs, selected checkpoint, weights, evaluator metrics, and artifact bytes remain identical

#### Scenario: Constraint and leakage suites run
- **WHEN** gradients push across sign/bounds or fixtures contain user, request, temporal, embargo, or test-selection leakage
- **THEN** projection preserves every sign/bound and leakage fixtures fail before publication

#### Scenario: Failure recovery suite runs
- **WHEN** data coverage, convergence, evaluator regression, write, sync, or atomic-rename failures are injected
- **THEN** no candidate is activated or partially published and pre-existing outputs remain unchanged

### Requirement: Operator and Scope Documentation
Frux SHALL document the learner's prerequisite sequence, supported inputs, optimization semantics, safety gates, output interpretation, and offline-only operational boundary.

#### Scenario: Operator prepares a run
- **WHEN** consulting the documentation
- **THEN** the operator can identify required prerequisite artifacts, compatible versions, split and exposure rules, defaults and bounds, deterministic seed behavior, resource limits, output permissions, evaluator gates, and failure recovery

#### Scenario: Candidate is reviewed
- **WHEN** an operator inspects successful artifacts
- **THEN** documentation states that the candidate changes only existing linear weights, is observational rather than causal, remains inactive, and requires a separate future reviewed activation workflow

#### Scenario: Out-of-scope systems are considered
- **WHEN** implementation scope is reviewed
- **THEN** documentation explicitly excludes database writes, policy activation, A/B systems, online model inference, semantic embeddings, pgvector, two-tower models, sequence models, model servers, and production runtime-image dependencies
