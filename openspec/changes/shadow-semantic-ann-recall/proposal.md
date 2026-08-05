## Why

Frux needs production-shaped evidence about the bounded semantic ANN provider before any policy activates it, but evaluating it on live recommendation context must not alter user-visible or recorded production behavior. This change adds disabled-by-default, capacity-safe shadow execution and operator acceptance criteria without treating overlap as proof of relevance lift.

## What Changes

- Deterministically sample a bounded subset of recommendation requests by user, request, and scene using configurable parts-per-million sampling with a default of `0`.
- Run the existing semantic ANN provider asynchronously alongside or after active recall, but never merge its candidates or change degraded output, ranking, snapshots, request logs, evidence, attribution, or response latency.
- Reuse the provider's global capacity and deadline controls and add a process-local no-queue shadow admission bound so calls that ignore cancellation cannot create unbounded goroutines or database queries.
- Compare ANN IDs only in memory against bounded existing content-similarity and session-continuation candidate sets.
- Record bounded candidate count, latency, result class, profile availability, intersection, Jaccard, and coverage histograms using fixed labels; never persist candidate IDs, vectors, or high-cardinality dimensions.
- Add an operator-readable acceptance report/runbook with minimum sample and profile coverage, p95 latency, error/capacity, and overlap observations. The report does not activate policies or claim online relevance lift.
- Add cancellation, shutdown, capacity, failure, metric-bound, and production-result invariance tests.
- Explicitly exclude pgvector migrations/projection changes, provider scoring changes, policy activation, ranking or training changes, an A/B framework, and Web changes.

## Capabilities

### New Capabilities

- `semantic-ann-shadow-evaluation`: Defines deterministic bounded sampling, isolated asynchronous ANN shadow execution, no-queue admission, in-memory bounded overlap evaluation, fixed-label observability, acceptance reporting, shutdown, and verification.

### Modified Capabilities

- `contextual-recommendation`: Strengthens the recommendation isolation contract so sampled shadow work cannot affect production candidates, degradation, ordering, snapshots, logs, evidence, attribution, or response latency.

## Impact

- Depends explicitly on `enable-pgvector-recommendation-index` for the bounded ANN interface and index controls, `project-semantic-user-interest` for compatible profile reads, and the narrowed `add-pgvector-recommendation-recall` for the existing provider and its capacity/deadline behavior.
- Affects recommendation application orchestration, API composition/configuration, bounded metrics, focused tests, and the recommendation operator documentation/report template.
- Adds no public API, persistence schema, vector/profile projection, ANN scoring, active policy, ranking feature, training path, experiment framework, or Web behavior.
