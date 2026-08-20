## Why

Frux needs low-volume, production-shaped and human-reviewed evidence about the registered semantic provider before any policy activates it. Evaluation must remain disabled by default, must not alter user-visible or recorded production behavior, and must not claim causal lift from observational retrieval data.

## What Changes

- Deterministically sample a bounded subset of recommendation requests by user, request, and scene using configurable parts-per-million sampling with a default of `0`.
- Run the existing semantic ANN provider asynchronously alongside or after active recall, but never merge its candidates or change degraded output, ranking, snapshots, recommendation request logs, evidence, attribution, or response latency.
- Reuse the provider's global capacity and deadline controls and add a process-local no-queue shadow admission bound so calls that ignore cancellation cannot create unbounded goroutines or database queries.
- Compare ANN IDs only in memory against bounded baseline sets and run an isolated simulation of the planned provider reservations, pre-rank pool cap, and explicit semantic ranking.
- Record bounded latency, error, capacity, profile/index coverage, profile/projection staleness, unique contribution, pool-truncation survival, simulated rank survival, author/topic diversity, and Fresh/Hot displacement using fixed labels; never persist candidate IDs or vectors.
- Add a small golden semantic-relevance set and human-review workflow that can operate without large traffic samples.
- Add an operator-readable report/runbook that states denominators and uncertainty, treats insufficient evidence as inconclusive, does not activate policies, and does not claim causal online lift.
- Add cancellation, shutdown, capacity, failure, metric-bound, and production-result invariance tests.
- Explicitly exclude pgvector migrations/projection changes, production provider scoring/policy activation/ranking changes, model training, an A/B framework, causal-lift claims, and Web changes.

## Capabilities

### New Capabilities

- `semantic-ann-shadow-evaluation`: Defines deterministic bounded sampling, isolated asynchronous ANN shadow execution, no-queue admission, in-memory retrieval/mixing/ranking simulation, operational/coverage/staleness/diversity metrics, small golden/human relevance review, acceptance reporting, shutdown, and verification.

### Modified Capabilities

- `contextual-recommendation`: Strengthens the recommendation isolation contract so sampled shadow work cannot affect production candidates, degradation, ordering, snapshots, logs, evidence, attribution, or response latency.

## Impact

- Depends explicitly on `enable-pgvector-recommendation-index` for the bounded ANN interface and index controls, `project-semantic-user-interest` for compatible profile reads, and the narrowed `add-pgvector-recommendation-recall` for the existing provider and its capacity/deadline behavior.
- Affects recommendation application orchestration, API composition/configuration, bounded metrics, golden/human evaluation tooling and fixtures within existing artifacts, focused tests, and the recommendation operator documentation/report template.
- Adds no public API, persistence schema, vector/profile projection, ANN scoring, active policy, ranking feature, training path, experiment framework, or Web behavior.
