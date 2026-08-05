## ADDED Requirements

### Requirement: Explicit Shadow Prerequisites and Disabled Default
Semantic ANN shadow evaluation SHALL depend on the bounded ANN interface from `enable-pgvector-recommendation-index`, compatible semantic profile reads from `project-semantic-user-interest`, and the existing provider contract from the narrowed `add-pgvector-recommendation-recall`. Shadow sampling SHALL default to `0` PPM and MUST NOT require, create, or activate a policy containing `semantic_ann`.

#### Scenario: Shadow configuration is absent
- **WHEN** the API starts without explicit semantic ANN shadow configuration
- **THEN** shadow sampling is `0` PPM, no shadow provider call is admitted, and active recommendation behavior retains its prior prerequisites

#### Scenario: A prerequisite is unavailable
- **WHEN** shadow sampling is greater than zero but the compatible semantic ANN provider or either prerequisite interface is unavailable
- **THEN** startup fails with a bounded configuration error before partial shadow evaluation is enabled

#### Scenario: Active policy omits semantic ANN
- **WHEN** shadow sampling is enabled and the selected recommendation policy has no `semantic_ann` entries
- **THEN** eligible requests may run shadow evaluation without changing or activating that policy

### Requirement: Deterministic Bounded Request Sampling
The evaluator SHALL sample by a versioned deterministic hash of positive user ID, canonical request ID, and normalized scene. Sampling PPM SHALL be configurable from `0` through `1,000,000`, use the same result for the same tuple across retries and instances, and MUST NOT persist or log the tuple or hash.

#### Scenario: Sampling is disabled
- **WHEN** configured sampling is `0` PPM
- **THEN** no request is selected and no shadow admission, goroutine, profile read, or ANN query occurs

#### Scenario: Sampling is complete
- **WHEN** configured sampling is `1,000,000` PPM and the request tuple is valid
- **THEN** the request is selected subject only to non-blocking capacity and lifecycle admission

#### Scenario: The request is retried
- **WHEN** two instances evaluate the same user, request ID, normalized scene, and sampler version
- **THEN** both instances make the same sampling decision

#### Scenario: Sampling configuration is invalid
- **WHEN** PPM is negative or greater than `1,000,000`, or budget, deadline, in-flight, or comparison bounds are invalid
- **THEN** API startup rejects the configuration without enabling shadow execution

### Requirement: Asynchronous Production-Isolated Execution
For an admitted sampled request, the evaluator SHALL invoke the existing semantic ANN provider asynchronously after or alongside active recall with a copied bounded request input. The recommendation response MUST NOT await shadow completion, and shadow candidates or failures MUST NOT enter merge, visibility filtering, suppression, ranking, diversity, snapshots, degraded output, request logs, served-candidate evidence, outcomes, attribution, or response fields.

#### Scenario: Shadow ANN succeeds
- **WHEN** the shadow provider returns valid candidates
- **THEN** only aggregate shadow observations are emitted and the production candidate pool, ordering, metadata, and durable artifacts are identical to execution without shadow

#### Scenario: Shadow ANN fails or times out
- **WHEN** the profile read or ANN query fails, is cancelled, exceeds its deadline, or returns invalid output
- **THEN** the production result and degraded-provider state remain unchanged and only a fixed shadow result is observed

#### Scenario: Shadow provider blocks
- **WHEN** the semantic ANN provider does not return before the recommendation response is ready
- **THEN** the response completes without waiting for the provider or its metrics

#### Scenario: Shadow work receives request context
- **WHEN** the request is admitted
- **THEN** the evaluator copies only bounded scalar/context values and does not retain pooled HTTP request state or mutable production candidate objects

### Requirement: Capacity-Safe No-Queue Admission
Shadow execution SHALL use the existing provider deadline validation and process-wide capacity controller while preserving the active provider permit count. It SHALL additionally use a distinct process-local non-blocking shadow admission bound from `1` through `16`, acquire all required shadow permits before creating a goroutine, hold them until the actual provider call returns, and MUST NOT queue, retry, or consume active permits.

#### Scenario: Shadow capacity is available
- **WHEN** a sampled request is selected and all shadow permits are immediately available
- **THEN** exactly one provider invocation starts with budget `1..100` and deadline `25..500` milliseconds

#### Scenario: Shadow capacity is exhausted
- **WHEN** a selected request cannot immediately acquire shadow admission
- **THEN** no goroutine, profile read, or ANN query starts and a fixed `capacity` result is recorded

#### Scenario: Provider ignores cancellation
- **WHEN** admitted provider calls continue after their contexts are cancelled
- **THEN** actual outstanding shadow calls and their goroutines remain bounded by configured `max_in_flight`

#### Scenario: Active capacity is saturated
- **WHEN** active recommendation providers need every active permit while shadow calls are outstanding
- **THEN** shadow execution does not reduce active admission or add active capacity degradation

### Requirement: Cancellation and Bounded Shutdown
The evaluator SHALL stop new admission before shutdown cancellation, cancel admitted cooperative work through a process lifecycle context, and support a caller-bounded drain. Shutdown MUST NOT wait indefinitely for context-ignoring providers or start replacement work.

#### Scenario: Shutdown begins with cooperative work
- **WHEN** shutdown closes admission and cancels the lifecycle context
- **THEN** cooperative profile and ANN calls stop, observations are finalized at most once, and permits are released

#### Scenario: Shutdown begins with context-ignoring work
- **WHEN** a provider remains blocked after the shutdown deadline
- **THEN** shutdown returns an incomplete-drain result while outstanding calls remain bounded and no new calls are accepted

#### Scenario: Request is cancelled before admission
- **WHEN** request cancellation is observed before all admissions succeed
- **THEN** no shadow goroutine or provider call starts

### Requirement: Bounded In-Memory Comparator Evaluation
The evaluator SHALL compare at most the configured bounded unique ANN IDs with bounded unique active `content_similarity` and `session_continuation` IDs captured before production merge. It SHALL compute request-local candidate count, intersection count, Jaccard ratio, and comparator coverage for fixed comparators `content_similarity`, `session_continuation`, and their union, and MUST NOT persist or log candidate IDs, vectors, scores, or per-request overlap rows.

#### Scenario: Both sets contain candidates
- **WHEN** ANN and an available comparator have bounded unique IDs
- **THEN** intersection is `|ANN ∩ comparator|`, Jaccard is `|intersection| / |ANN ∪ comparator|`, and coverage is `|intersection| / |comparator|`

#### Scenario: Comparator is unavailable
- **WHEN** an active comparator was omitted, failed, or did not produce a capturable result
- **THEN** its fixed unavailable state is recorded without rerunning that provider or emitting undefined ratios

#### Scenario: Available comparator is empty
- **WHEN** a comparator completed successfully with no candidates
- **THEN** empty counts are observed and undefined coverage is omitted rather than reported as a relevance value

#### Scenario: Provider returns duplicate or excessive IDs
- **WHEN** ANN or comparator output contains duplicates, invalid IDs, or more than its configured bound
- **THEN** evaluation keeps only deterministic bounded unique positive IDs and allocates no unbounded set

### Requirement: Fixed-Label Shadow Observability
Shadow evaluation SHALL expose aggregate selected count, terminal result, profile availability, provider latency, candidate counts, comparator state, intersection, Jaccard, and coverage histograms. Label values SHALL be restricted to documented fixed enums, and each selected request SHALL produce at most one terminal result observation.

#### Scenario: Shadow work completes
- **WHEN** evaluation succeeds, is empty, has no profile, times out, reaches capacity, is cancelled, fails, or returns invalid output
- **THEN** metrics use only fixed result, profile, source, comparator, and comparator-state labels

#### Scenario: Sensitive or high-cardinality values exist
- **WHEN** evaluation handles user, request, session, video, vector, score, model, policy, raw scene, SQL/index, or error data
- **THEN** those values do not appear in metric labels or normal logs

#### Scenario: Completion races cancellation
- **WHEN** provider completion and deadline or shutdown cancellation occur concurrently
- **THEN** exactly one terminal result class and one latency observation are emitted

### Requirement: Operator Acceptance Report Without Automatic Activation
The recommendation runbook SHALL provide copyable aggregate queries and an operator-readable report for a window of at least 24 hours. Acceptance SHALL require at least 10,000 selected requests, at least 1,000 profile-available provider executions, at least 99% terminal metric coverage, at least 10% profile availability, p95 provider latency no greater than both 250 milliseconds and the configured deadline, provider-error plus invalid-result rate no greater than 1%, timeout rate no greater than 1%, and capacity rate no greater than 5%.

#### Scenario: Observation window meets data and operational gates
- **WHEN** operators complete the report with the required sample, coverage, latency, error, timeout, and capacity evidence
- **THEN** the report records the capability as operationally accepted and includes candidate, empty/unavailable, intersection, Jaccard, and coverage distributions for every available comparator

#### Scenario: Evidence is insufficient or a bound fails
- **WHEN** any minimum sample/coverage requirement is missing or an operational bound is exceeded
- **THEN** the report states not accepted and recommends configuration or dependency investigation without changing a recommendation policy

#### Scenario: Overlap appears favorable
- **WHEN** Jaccard or coverage observations are high
- **THEN** the report describes retrieval overlap only, makes no online relevance-lift claim, and performs no automatic policy activation

### Requirement: Focused Safety and Invariance Verification
Implementation SHALL include deterministic sampler, configuration, cancellation, shutdown, capacity, context-ignoring provider, failure, fixed-label metric, overlap math, and production-invariance tests. Verification MUST prove that enabling and selecting shadow evaluation changes no production result or durable recommendation side effect.

#### Scenario: Shadow modes are compared with the baseline
- **WHEN** tests execute success, empty, no-profile, error, timeout, capacity, cancellation, and blocked-provider cases with shadow disabled and selected
- **THEN** candidate IDs/order, reasons, scores, degraded flags, cursors, snapshots, request-log calls, evidence calls, attribution inputs, and response completion behavior are identical

#### Scenario: Capacity stress is executed
- **WHEN** more sampled requests arrive than `max_in_flight` while providers ignore cancellation
- **THEN** only the bounded admitted calls start, the remainder receive capacity observations, and active recall retains its baseline result
