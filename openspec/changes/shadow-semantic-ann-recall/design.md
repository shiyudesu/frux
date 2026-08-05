## Context

Frux already has bounded active recall execution with per-provider budgets/deadlines, non-blocking process-local admission, candidate merge, ranking, snapshots, sampled request logs, served-candidate evidence, and attribution. The narrowed `add-pgvector-recommendation-recall` change adds the existing application-level `semantic_ann` provider but deliberately leaves bootstrap policies unchanged and defers shadow evaluation.

This change depends explicitly on:

- `enable-pgvector-recommendation-index` for the validated, bounded, cancellable ANN query interface and index capacity controls;
- `project-semantic-user-interest` for compatible recent/long-term semantic profile reads;
- the narrowed `add-pgvector-recommendation-recall` for one provider invocation contract, profile selection, result classes, budget/deadline bounds, and active-path capacity behavior.

Shadow work is observational only. It must run on production-shaped request context without entering candidate merge or any durable recommendation artifact. Because a provider or database driver may ignore context cancellation, launching one goroutine per sampled request without admission would still be unsafe.

## Goals / Non-Goals

**Goals:**

- Deterministically sample recommendation requests by user, request, and normalized scene with default-zero PPM configuration.
- Invoke the existing semantic ANN provider asynchronously without extending response latency or changing any production result or durable record.
- Preserve active provider capacity while bounding shadow work with a distinct no-queue in-flight limit and the existing provider deadline rules.
- Compare bounded unique ANN IDs in memory with bounded content-similarity and session-continuation ID sets.
- Emit fixed-label aggregate metrics for execution, profile availability, latency, candidate counts, and overlap.
- Provide a repeatable operator report/runbook with explicit data-sufficiency and operational acceptance gates.
- Prove cancellation, shutdown, capacity, failure isolation, metric bounds, and production-result invariance.

**Non-Goals:**

- pgvector extension, migration, projection, index, reconciliation, query-plan, or embedding/profile projection changes.
- Semantic provider scoring, profile selection, exclusions, or active policy behavior changes.
- Candidate merge, ranking features/weights, snapshots, request logs, evidence, attribution, training, policy activation, or online experiments.
- Persisting per-request shadow facts, candidate IDs, vectors, or overlap rows.
- A/B assignment, online relevance measurement, public API, or Web changes.

## Decisions

### 1. Use a versioned deterministic PPM sampler

Add shadow configuration under recommendation composition:

- `sample_ppm`: integer `0..1_000_000`, default `0`;
- `budget`: integer `1..100`, default `20`;
- `deadline_ms`: integer `25..500`, default `250`;
- `max_in_flight`: integer `1..16`, default `2`;
- `comparison_limit`: integer `1..100`, default `100`.

`sample_ppm=0` disables construction/admission of shadow calls. For an eligible recommendation request, the sampler hashes a versioned, length-delimited tuple of positive `user_id`, canonical `request_id`, and normalized scene with SHA-256, maps the first 64 bits to `[0,1_000_000)`, and samples when the bucket is below `sample_ppm`. The same tuple is stable across retries and instances; changing the algorithm requires a new sampler version. The hash and tuple are not logged or persisted.

The evaluator uses its own validated budget/deadline rather than requiring an active policy to contain `semantic_ann`; this is evaluation configuration, not policy activation. It reuses the provider's accepted bounds and does not alter bootstrap or selected policy JSON.

Alternative considered: random sampling per process. Rejected because retries and replicas would produce inconsistent cohorts and make aggregate interpretation harder.

### 2. Capture comparison sets before merge, then launch shadow work without awaiting it

Active recall retains bounded unique ID snapshots for only the `content_similarity` and `session_continuation` provider results before merge. Each set is truncated deterministically to `comparison_limit`, copied into a small immutable shadow input, and discarded after evaluation. Failed, absent, or policy-omitted comparators produce an unavailable comparator state rather than triggering extra baseline recall.

After active recall has produced the production result needed by the existing path, the service performs sampling and non-blocking admission, then launches shadow evaluation. No caller waits for provider completion, metrics, or report data. The evaluator receives copied scalar context and bounded recent/current video IDs; it never receives mutable candidate objects, snapshot state, request-log builders, evidence writers, or response objects.

Shadow output is reduced immediately to bounded unique positive video IDs for in-memory comparison. Candidate scores and vectors are not copied into production candidates, metrics, logs, or persistence.

Alternative considered: rerun the baseline providers inside shadow evaluation. Rejected because it adds avoidable load and compares different point-in-time results.

### 3. Separate active and shadow admission while reusing provider deadline/capacity machinery

Refactor provider execution behind one process-local controller with active and shadow classes:

- active admission preserves the existing configured active permit count and behavior;
- shadow admission is a distinct non-blocking semaphore capped by `max_in_flight`;
- shadow calls use the same deadline validation, timeout wrapper, result classification, and underlying semantic provider;
- shadow permits cannot consume or reduce active permits.

Sampling and both shadow admissions occur before any goroutine is created. Capacity rejection records a fixed `capacity` result and returns immediately. An admitted shadow call holds its shadow permit until the actual provider invocation returns, even if the deadline observer has already emitted `timeout` or `cancelled`; therefore a context-ignoring provider can leave at most `max_in_flight` actual calls/goroutines outstanding. There is no wait queue and no retry.

This preserves the active-path capacity contract while imposing an additional process-wide bound on observational load. The default-zero sampler means no extra query load until operators opt in.

Alternative considered: share the active semaphore directly. Rejected because shadow calls could cause production providers to receive capacity degradation, violating isolation.

### 4. Use a process lifecycle context and bounded shutdown

The shadow evaluator owns a lifecycle context created during API composition, an admission-closed flag, and a wait group for admitted observers. Request cancellation before admission prevents launch; after launch, work is governed by the earlier of the configured provider deadline and lifecycle cancellation, not by a pooled HTTP request object.

Shutdown closes admission first, cancels the lifecycle context, and waits only until the caller's shutdown deadline. Cooperative provider calls exit and release permits. Context-ignoring calls remain bounded by `max_in_flight`; shutdown reports an incomplete drain without starting replacement work or blocking indefinitely. Process exit remains the final cleanup boundary.

Alternative considered: detach from all cancellation using `context.Background()`. Rejected because deployments could not stop shadow queries promptly.

### 5. Define bounded in-memory overlap calculations

The evaluator canonicalizes at most `budget` unique ANN IDs and at most `comparison_limit` unique IDs for each captured comparator. It computes for fixed comparator values `content_similarity`, `session_continuation`, and `union`:

- candidate counts;
- intersection count `|ANN ∩ comparator|`;
- Jaccard ratio `|intersection| / |ANN ∪ comparator|` when the union is non-empty;
- comparator coverage ratio `|intersection| / |comparator|` when the comparator is non-empty.

Unavailable comparators are counted but do not emit ratio observations. Empty available sets emit candidate/intersection counts; undefined ratios are omitted rather than encoded as misleading zeroes. All sets are request-local and become unreachable after metrics are observed.

Alternative considered: persist candidate pairs for later analysis. Rejected because aggregate acceptance does not require identifiers and persistence would expand privacy and retention scope.

### 6. Emit only fixed-label aggregate metrics

Add metrics such as:

- `frux_recommendation_semantic_ann_shadow_selected_total`;
- `frux_recommendation_semantic_ann_shadow_results_total{result,profile}`;
- `frux_recommendation_semantic_ann_shadow_duration_seconds{result}`;
- `frux_recommendation_semantic_ann_shadow_candidate_count{source}`;
- `frux_recommendation_semantic_ann_shadow_intersection_count{comparator}`;
- `frux_recommendation_semantic_ann_shadow_jaccard_ratio{comparator}`;
- `frux_recommendation_semantic_ann_shadow_comparator_coverage_ratio{comparator}`;
- `frux_recommendation_semantic_ann_shadow_comparator_total{comparator,state}`.

Allowed result labels are `success`, `empty`, `no_profile`, `timeout`, `capacity`, `cancelled`, `provider_error`, and `invalid_result`. Profile labels are `available`, `unavailable`, and `unknown`; source labels are `ann`, `content_similarity`, and `session_continuation`; comparator labels are `content_similarity`, `session_continuation`, and `union`; comparator state is `available`, `empty`, or `unavailable`.

User, request, session, video, vector, model, score, policy version, raw scene, SQL/index details, and raw errors never appear in labels or normal logs. Metrics are finalized at most once per selected request.

Alternative considered: label metrics by scene or policy. Rejected because the shadow scope currently has one normalized recommendation scene and those dimensions are unnecessary cardinality.

### 7. Keep every production artifact outside the shadow evaluator

The evaluator returns no candidates or degradation to the recommendation service. Its result type contains only aggregate observation values consumed by the metric sink. It has no interfaces for snapshots, request logs, served evidence, outcomes, attribution, policy selection/mutation, or persistence.

Regression tests compare shadow-disabled and shadow-selected executions for exact production candidate IDs/order, reasons, source scores, rank scores, degraded flags/providers, cursor/snapshot payloads, request-log calls, evidence calls, and response completion ordering. Success, empty profile, provider error, timeout, capacity, cancellation, and context-ignoring cases must all be production-equivalent.

Alternative considered: attach a hidden semantic reason and strip it before HTTP serialization. Rejected because it could still affect merge, ranking, snapshots, logs, or evidence.

### 8. Make the acceptance report operational, not an activation mechanism

Update the recommendation operator documentation with a copyable report template and Prometheus queries. A valid acceptance observation window is at least 24 hours and requires:

- at least 10,000 selected requests;
- at least 1,000 profile-available provider executions;
- terminal metric coverage for at least 99% of selected requests;
- profile availability reported explicitly and at least 10% for the evaluated population;
- provider p95 latency at or below both 250 milliseconds and the configured shadow deadline;
- provider-error plus invalid-result rate at or below 1%;
- timeout rate at or below 1%;
- capacity rate at or below 5%;
- candidate, intersection, Jaccard, and comparator-coverage observations for every available comparator, including p50/p95 ratios and empty/unavailable rates.

The report records window, deployment/configuration, dependency revisions, counts, profile availability, latency, result rates, and overlap distributions. Missing sufficiency or a failed operational bound is reported as not accepted. Passing the report only establishes safe operability and observable retrieval behavior; overlap has no relevance threshold, does not prove online lift, and never creates or activates a recommendation policy.

Alternative considered: automatically enable `semantic_ann` when gates pass. Rejected because rollout remains an explicit policy decision under the active-provider change.

## Risks / Trade-offs

- [Shadow queries add database load] → Default sampling to zero, cap PPM/budget/deadline/in-flight work, preserve active permits, and require capacity/latency observation before increasing traffic.
- [A provider ignores cancellation forever] → Acquire before goroutine creation, retain permits until actual return, bound outstanding calls by `max_in_flight`, and never queue or retry.
- [Captured comparator sets are unavailable after active failure or policy omission] → Record fixed unavailable state and do not synthesize another baseline.
- [Overlap distributions may be mistaken for relevance] → Label the report as retrieval-overlap evidence only and require explicit wording that online lift needs a separate accepted experiment.
- [Asynchronous metrics may be lost during abrupt process death] → Use lifecycle cancellation and bounded shutdown; treat aggregate telemetry as best-effort and require 99% terminal coverage.
- [Deterministic sampling could change accidentally] → Version and unit-test canonical tuple encoding and golden buckets.

## Migration Plan

1. Complete and strictly validate the three prerequisite changes in dependency order.
2. Add sampler, configuration validation, lifecycle evaluator, classed admission, metric sink, and tests with `sample_ppm=0`.
3. Wire copied comparator sets and asynchronous launch without adding persistence or policy fields.
4. Deploy disabled and prove existing recommendation outputs and bootstrap policies are unchanged.
5. Enable a small PPM in a prepared environment, observe capacity/error/latency, and raise only within documented bounds.
6. Run the acceptance report for a qualifying window. Treat insufficient or failed observations as no-go information only.

Rollback sets `sample_ppm=0`, which stops new admissions without changing active policies or prerequisite data. Shutdown cancels cooperative work; bounded context-ignoring calls disappear with the process. No schema or data rollback is needed.

## Open Questions

None. Sampling identity, defaults and bounds, comparator sets, overlap formulas, admission/shutdown behavior, metric labels, acceptance gates, dependencies, and exclusions are fixed by this proposal.
